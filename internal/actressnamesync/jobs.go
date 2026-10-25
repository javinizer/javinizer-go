package actressnamesync

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
)

func jobView(job *database.ActressSyncJob) *JobView {
	out := &JobView{ID: job.ID, SnapshotDigest: job.SnapshotDigest, CreatedAt: job.CreatedAt, Cancelled: job.Cancelled, Counts: make(map[string]int), Status: "completed"}
	pending, leased, blocked := false, false, false
	for _, t := range job.Tasks {
		out.Counts[t.Status]++
		if t.Status == "pending" {
			pending = true
			if t.ProblemCode != "" {
				blocked = true
			}
		}
		if t.Status == "leased" || t.Status == "applying" {
			leased = true
		}
	}
	switch {
	case job.Cancelled:
		out.Status = "cancelled"
	case leased:
		out.Status = "running"
	case pending && blocked:
		out.Status = "blocked"
	case pending:
		out.Status = "pending"
	}
	return out
}

// Job reads durable status even when offline evidence or new admission is disabled.
func (s *Service) Job(ctx context.Context, id string) (*JobView, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if !validJob(id) {
		return nil, ErrInvalid
	}
	job, err := s.repo.OfflineNameSyncJob(ctx, id)
	if err != nil {
		return nil, err
	}
	return jobView(job), nil
}

// Tasks returns a bounded receipt page without lease or confirmation authority.
func (s *Service) Tasks(ctx context.Context, id string, offset, limit int) (*TaskPage, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if !validJob(id) || offset < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	job, err := s.repo.OfflineNameSyncJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if offset > len(job.Tasks) {
		return nil, ErrInvalid
	}
	end := min(offset+limit, len(job.Tasks))
	out := &TaskPage{Items: make([]TaskView, 0, end-offset)}
	for _, t := range job.Tasks[offset:end] {
		out.Items = append(out.Items, TaskView{ID: t.ID, ActressID: t.ActressID, SourceKey: t.SourceKey, BeforeName: t.BeforeName, AfterName: t.AfterName, Status: t.Status, Reason: t.Reason, ProblemCode: t.ProblemCode, RetryAfterMS: t.RetryAfterMS, ProblemAttempts: t.ProblemAttempts, FinishedAt: t.FinishedAt})
	}
	if end < len(job.Tasks) {
		out.NextOffset = &end
	}
	return out, nil
}

// ListJobs reads bounded durable history, newest first.
func (s *Service) ListJobs(ctx context.Context, cursor string, limit int) (*JobPage, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if cursor != "" && !validJob(cursor) || limit < 1 || limit > database.MaxActressSyncHistoryJobs {
		return nil, ErrInvalid
	}
	jobs, err := s.repo.ListOfflineNameSyncJobs(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := &JobPage{Items: make([]JobView, 0, len(jobs))}
	for _, j := range jobs {
		full, err := s.repo.OfflineNameSyncJob(ctx, j.ID)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *jobView(full))
	}
	if len(jobs) == limit {
		out.NextCursor = jobs[len(jobs)-1].ID
	}
	return out, nil
}

// Cancel fences uncommitted work; already applied receipts remain applied.
func (s *Service) Cancel(ctx context.Context, id string) (*JobView, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if !validJob(id) {
		return nil, ErrInvalid
	}
	if err := s.repo.CancelOfflineNameSync(ctx, id); err != nil {
		return nil, err
	}
	job, err := s.repo.OfflineNameSyncJob(ctx, id)
	if err != nil {
		return nil, err
	}
	return jobView(job), nil
}
