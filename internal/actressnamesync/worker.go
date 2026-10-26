package actressnamesync

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/database"
)

const workerInterval = time.Second
const workerLease = 2 * time.Minute

// Run owns initialization and one serial recovery worker until server shutdown.
// Concurrent calls do not create additional workers. The runtime must join Run
// before closing its repository; request handlers must also join that lifecycle.
func (s *Service) Run() {
	s.once.Do(func() {
		s.initialize()
		if !s.opts.Enabled {
			return
		}
		ticker := time.NewTicker(workerInterval)
		defer ticker.Stop()
		cursor := ""
		var historical *actresscache.Snapshot
		for {
			if s.ctx.Err() != nil {
				return
			}
			cursor, historical = s.workPass(s.ctx, cursor, historical)
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
			case <-ticker.C:
			}
		}
	})
}

func (s *Service) workPass(ctx context.Context, cursor string, historical *actresscache.Snapshot) (string, *actresscache.Snapshot) {
	jobs, err := s.repo.DueOfflineNameSyncJobs(ctx, s.now(), cursor, database.DefaultActressSyncDueJobs)
	s.mu.Lock()
	if err != nil {
		// Preserve a safe operational signal without logging SQL or paths on
		// every poll. A later successful discovery clears the current problem.
		s.info.WorkerProblemCode = "queue_unavailable"
	} else {
		s.info.WorkerProblemCode = ""
	}
	s.mu.Unlock()
	if err != nil {
		return cursor, historical
	}
	if len(jobs) == 0 {
		return "", historical
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return cursor, historical
		}
		cursor = job.ID
		snapshot, err := s.snapshotForJob(ctx, job.SnapshotDigest, historical)
		if err != nil {
			if ctx.Err() != nil {
				return cursor, historical
			}
			code := database.ActressSyncProblemSnapshotInvalid
			if errors.Is(err, os.ErrNotExist) {
				code = database.ActressSyncProblemSnapshotMissing
			}
			_ = s.repo.RecordOfflineNameSyncJobProblem(ctx, job.ID, code, s.now())
			continue
		}
		s.mu.RLock()
		var current *actresscache.Snapshot
		if s.generation != nil {
			current = s.generation.snapshot
		}
		s.mu.RUnlock()
		if snapshot != current {
			historical = snapshot
		}
		lease, err := s.repo.ClaimOfflineNameSync(ctx, job.ID, s.workerID, s.now(), workerLease)
		if err != nil {
			continue
		}
		_, err = s.repo.ApplyOfflineNameSync(ctx, *lease, snapshot, s.now())
		if err != nil && ctx.Err() == nil {
			_ = s.repo.RecordOfflineNameSyncLeaseProblem(ctx, *lease, database.ActressSyncProblemApplyFailed, s.now())
		}
	}
	return cursor, historical
}

func (s *Service) snapshotForJob(ctx context.Context, digest string, historical *actresscache.Snapshot) (*actresscache.Snapshot, error) {
	s.mu.RLock()
	var current *actresscache.Snapshot
	if s.generation != nil {
		current = s.generation.snapshot
	}
	s.mu.RUnlock()
	if current != nil && current.Digest() == digest {
		return current, nil
	}
	if historical != nil && historical.Digest() == digest {
		return historical, nil
	}
	store, err := artifactstore.Open(s.opts.ArtifactDirectory, offline.DefaultLimits().Store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	// digest is read exclusively from an admitted durable job, never an HTTP query.
	return store.LoadReferencedSnapshot(ctx, digest)
}
