package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/database"
	"os"
	"path/filepath"
	"time"
)

// One service-owned serial worker calls both methods. No lease is held at gate.
type gatedRepository struct {
	*database.ActressRepository
	root       string
	pauseAfter int
	applied    int
}

func (r *gatedRepository) ClaimOfflineNameSync(ctx context.Context, id, owner string, now time.Time, ttl time.Duration) (*database.ActressSyncLease, error) {
	if r.pauseAfter >= 0 && r.applied >= r.pauseAfter {
		b, err := json.Marshal(map[string]any{"job_id": id, "applied": r.applied})
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(r.root, "worker.paused"), b, 0600); err != nil {
			return nil, err
		}
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			_, err := os.Stat(filepath.Join(r.root, "worker.release"))
			if err == nil {
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
			}
		}
	}
	return r.ActressRepository.ClaimOfflineNameSync(ctx, id, owner, time.Now(), ttl)
}
func (r *gatedRepository) ApplyOfflineNameSync(ctx context.Context, lease database.ActressSyncLease, snapshot *actresscache.Snapshot, now time.Time) (*database.ActressSyncTask, error) {
	receipt, err := r.ActressRepository.ApplyOfflineNameSync(ctx, lease, snapshot, now)
	if err == nil {
		r.applied++
	}
	return receipt, err
}
