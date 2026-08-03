package core

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// EnsureActressSyncManager ...
func (r *APIRuntime) EnsureActressSyncManager() *worker.ActressSyncManager {
	if r == nil || r.deps == nil || r.deps.CoreDeps == nil || r.deps.CoreDeps.DB == nil || r.deps.CoreDeps.DB.DB == nil {
		return nil
	}
	r.actressSyncMu.Lock()
	defer r.actressSyncMu.Unlock()
	if r.actressSyncManager != nil {
		return r.actressSyncManager
	}
	db := r.deps.CoreDeps.DB
	r.actressSyncManager = worker.NewActressSyncManager(worker.ActressSyncManagerDeps{
		DB: db, ActressRepo: database.NewActressRepository(db), MovieRepo: database.NewMovieRepository(db),
		Snapshot: func() (*config.Config, *scraperutil.ScraperRegistry) {
			snapshot := r.Snapshot()
			return snapshot.Config(), snapshot.Registry()
		},
	})
	r.actressSyncManager.Start()
	return r.actressSyncManager
}

func (r *APIRuntime) stopActressSyncManager() {
	if r == nil {
		return
	}
	r.actressSyncMu.Lock()
	manager := r.actressSyncManager
	r.actressSyncManager = nil
	r.actressSyncMu.Unlock()
	if manager != nil {
		manager.Stop()
	}
}
