package jobs

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
)

// JobDeps holds the dependencies that job API handlers need.
// Replaces the former JobQueryService — handlers take this directly,
// matching the Deps pattern used in the actress and genre packages.
//
// Handlers call the underlying repos and services directly instead of
// routing through one-line pass-through methods, eliminating the
// indirection layer that JobQueryService provided.
type JobDeps struct {
	JobRepo         database.JobRepositoryInterface
	BatchFileOpRepo database.BatchFileOperationRepositoryInterface
	JobStore        worker.JobStoreInterface
	Reverter        history.BatchReverter // Per D-10: interface, not concrete *history.Reverter
	EventEmitter    eventlog.EventEmitter
	AllowRevert     bool
}

// NewJobDeps creates a JobDeps from individual dependencies.
func NewJobDeps(
	jobRepo database.JobRepositoryInterface,
	batchFileOpRepo database.BatchFileOperationRepositoryInterface,
	jobStore worker.JobStoreInterface,
	reverter history.BatchReverter,
	eventEmitter eventlog.EventEmitter,
	allowRevert bool,
) JobDeps {
	return JobDeps{
		JobRepo:         jobRepo,
		BatchFileOpRepo: batchFileOpRepo,
		JobStore:        jobStore,
		Reverter:        reverter,
		EventEmitter:    eventEmitter,
		AllowRevert:     allowRevert,
	}
}

// JobWithStats holds a job together with its computed operation and revert counts.
// Used by getJob and listJobs handlers to avoid N+1 separate count queries
// when building the response.
type JobWithStats struct {
	Job           models.Job
	OpCount       int64
	RevertedCount int64
	// NoopCount counts terminal completed-noop rows (codex P2, PR #241 F2):
	// non-revertible by definition, so consumers subtract it alongside
	// RevertedCount when computing how many operations can still revert.
	NoopCount int64
}

// GetJobWithStats returns a single job with its operation and revert counts.
func (d JobDeps) GetJobWithStats(ctx context.Context, jobID string) (*JobWithStats, error) {
	job, err := d.JobRepo.FindByID(ctx, jobID)
	if err != nil {
		return nil, err
	}

	opCount, err := d.BatchFileOpRepo.CountByBatchJobID(ctx, job.ID)
	if err != nil {
		return nil, err
	}

	revertedCount, err := d.BatchFileOpRepo.CountByBatchJobIDAndRevertStatus(ctx, job.ID, models.RevertStatusReverted)
	if err != nil {
		return nil, err
	}

	noopCounts, err := d.BatchFileOpRepo.CountNoOpByBatchJobIDs(ctx, []string{job.ID})
	if err != nil {
		return nil, err
	}

	return &JobWithStats{
		Job:           *job,
		OpCount:       opCount,
		RevertedCount: revertedCount,
		NoopCount:     noopCounts[job.ID],
	}, nil
}

// ListJobsWithStats returns all jobs with their operation and revert counts.
func (d JobDeps) ListJobsWithStats(ctx context.Context) ([]JobWithStats, error) {
	return d.ListJobsWithStatsByStatus(ctx, "", 0)
}

// jobStatusLister is the narrow optional seam for status-filtered job
// listings, used by ListJobsWithStatsByStatus. Legacy test doubles see the
// in-memory fallback below; the concrete JobRepository implements this and
// pushes the filter down to SQL.
type jobStatusLister interface {
	ListByStatus(ctx context.Context, status string, limit int) ([]models.Job, error)
}

// ListJobsWithStatsByStatus is ListJobsWithStats filtered by job status in SQL
// when a non-empty filter is provided. Callers asking for status=running (e.g.
// the web layout's reload-restore probe that fires on every authenticated page
// load) can then skip hydrating the full job history (codex P2, PR #253).
// An empty status preserves the prior unfiltered behavior.
// A positive limit additionally bounds the query: the restore probe sends
// limit=1, so a page load with many running jobs must not fetch, decode, and
// aggregate every row (codex P2, PR #255).
func (d JobDeps) ListJobsWithStatsByStatus(ctx context.Context, status string, limit int) ([]JobWithStats, error) {
	var jobs []models.Job
	var err error
	if status != "" {
		if repo, ok := d.JobRepo.(jobStatusLister); ok {
			jobs, err = repo.ListByStatus(ctx, status, limit)
		} else {
			all, listErr := d.JobRepo.List(ctx)
			if listErr != nil {
				return nil, listErr
			}
			jobs = make([]models.Job, 0, len(all))
			for _, job := range all {
				if string(job.Status) == status {
					jobs = append(jobs, job)
				}
			}
		}
	} else {
		jobs, err = d.JobRepo.List(ctx)
	}
	if err != nil {
		return nil, err
	}
	// Legacy test doubles (and any repo not exposing the narrow seam) filter in
	// memory, so bound them the same way the SQL LIMIT bounds the repository —
	// the aggregate-count queries below must only run for the returned slice
	// (codex P2, PR #255).
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return d.attachJobCounts(ctx, jobs)
}

// jobStatusPhaseLister is the narrow optional seam for status+phase-filtered
// job listings, used by ListJobsWithStatsByStatusAndPhase. The concrete
// JobRepository implements this and pushes both predicates down to SQL;
// legacy test doubles hit the in-memory envelope-decode fallback below.
type jobStatusPhaseLister interface {
	ListByStatusAndPhase(ctx context.Context, status, phase string, limit int) ([]models.Job, error)
}

// ListJobsWithStatsByStatusAndPhase is ListJobsWithStatsByStatus with the
// durable phase marker included in the SQL predicate. The restore probe asks
// for status=running, phase=scrape, limit=1: pushing the phase into the
// repository means the row cap can no longer hide an older scrape behind
// newer apply-phase rows, while bounded jobs/aggregate fetches resume
// (codex P2, PR #255). Rows missing the marker stay eligible (scrape-
// eligible legacy rule). An empty phase degrades to the status-only variant.
func (d JobDeps) ListJobsWithStatsByStatusAndPhase(ctx context.Context, status, phase string, limit int) ([]JobWithStats, error) {
	if phase == "" {
		return d.ListJobsWithStatsByStatus(ctx, status, limit)
	}
	if repo, ok := d.JobRepo.(jobStatusPhaseLister); ok {
		jobs, err := repo.ListByStatusAndPhase(ctx, status, phase, limit)
		if err != nil {
			return nil, err
		}
		// No defensive re-cap here: the seam contract bounds the SQL query
		// itself, matching ListByStatus.
		return d.attachJobCounts(ctx, jobs)
	}
	// Legacy doubles without the seam decode the envelope in memory; the
	// status-filtered set is fetched unbounded first so a row cap applied
	// before filtering cannot hide scrape rows behind apply-phase rows.
	stats, err := d.ListJobsWithStatsByStatus(ctx, status, 0)
	if err != nil {
		return nil, err
	}
	matched := make([]JobWithStats, 0, len(stats))
	for _, stat := range stats {
		job := stat.Job
		snapshot, _ := jobpersist.Decode(&job)
		// Mirror the SQL predicate: a missing marker is only scrape-eligible
		// legacy evidence; other phases must match exactly (codex P2, PR #255).
		if snapshot.CurrentPhase == phase || (phase == "scrape" && snapshot.CurrentPhase == "") {
			matched = append(matched, stat)
		}
	}
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

// attachJobCounts batch-fetches operation/revert/noop counts for the given
// jobs in 3 queries total instead of 2 per job, pairing each job with its
// counts.
func (d JobDeps) attachJobCounts(ctx context.Context, jobs []models.Job) ([]JobWithStats, error) {
	// Batch-fetch operation and revert counts in 2 queries instead of 2N.
	jobIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
	}

	var opCounts, revertedCounts, noopCounts map[string]int64
	if len(jobIDs) > 0 {
		var err error
		opCounts, err = d.BatchFileOpRepo.CountByBatchJobIDs(ctx, jobIDs)
		if err != nil {
			return nil, err
		}
		revertedCounts, err = d.BatchFileOpRepo.CountRevertedByBatchJobIDs(ctx, jobIDs)
		if err != nil {
			return nil, err
		}
		noopCounts, err = d.BatchFileOpRepo.CountNoOpByBatchJobIDs(ctx, jobIDs)
		if err != nil {
			return nil, err
		}
	} else {
		opCounts = make(map[string]int64)
		revertedCounts = make(map[string]int64)
		noopCounts = make(map[string]int64)
	}

	results := make([]JobWithStats, len(jobs))
	for i, job := range jobs {
		results[i] = JobWithStats{
			Job:           job,
			OpCount:       opCounts[job.ID],
			RevertedCount: revertedCounts[job.ID],
			NoopCount:     noopCounts[job.ID],
		}
	}

	return results, nil
}
