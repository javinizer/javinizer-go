package jobs

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
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

	return &JobWithStats{
		Job:           *job,
		OpCount:       opCount,
		RevertedCount: revertedCount,
	}, nil
}

// ListJobsWithStats returns all jobs with their operation and revert counts.
func (d JobDeps) ListJobsWithStats(ctx context.Context) ([]JobWithStats, error) {
	jobs, err := d.JobRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	// Batch-fetch operation and revert counts in 2 queries instead of 2N.
	jobIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
	}

	var opCounts, revertedCounts map[string]int64
	if len(jobIDs) > 0 {
		opCounts, err = d.BatchFileOpRepo.CountByBatchJobIDs(ctx, jobIDs)
		if err != nil {
			return nil, err
		}
		revertedCounts, err = d.BatchFileOpRepo.CountRevertedByBatchJobIDs(ctx, jobIDs)
		if err != nil {
			return nil, err
		}
	} else {
		opCounts = make(map[string]int64)
		revertedCounts = make(map[string]int64)
	}

	results := make([]JobWithStats, len(jobs))
	for i, job := range jobs {
		results[i] = JobWithStats{
			Job:           job,
			OpCount:       opCounts[job.ID],
			RevertedCount: revertedCounts[job.ID],
		}
	}

	return results, nil
}
