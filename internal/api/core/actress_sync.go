package core

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// EnsureActressSyncManager lazily creates and starts the singleton sync
// manager on first demand (API-1: it is NEVER constructed eagerly —
// NewAPIRuntime is rebuilt per request path, so eager construction leaked
// managers). After Shutdown the latch is set and no manager is ever created
// again on this runtime (CON-08).
func (r *APIRuntime) EnsureActressSyncManager() *worker.ActressSyncManager {
	if r == nil || r.deps == nil || r.deps.CoreDeps == nil || r.deps.CoreDeps.DB == nil || r.deps.CoreDeps.DB.DB == nil {
		return nil
	}
	r.actressSyncMu.Lock()
	defer r.actressSyncMu.Unlock()
	if r.actressSyncStopped {
		return nil
	}
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

// stopActressSyncManager latches the singleton shut and stops any live
// manager. Called from Shutdown.
func (r *APIRuntime) stopActressSyncManager() {
	if r == nil {
		return
	}
	r.actressSyncMu.Lock()
	manager := r.actressSyncManager
	r.actressSyncManager = nil
	r.actressSyncStopped = true
	r.actressSyncMu.Unlock()
	if manager != nil {
		manager.Shutdown() // permanent latch, not the hot-reload-restartable Stop
	}
}
