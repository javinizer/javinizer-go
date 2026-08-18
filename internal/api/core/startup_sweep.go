package core

import (
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/spf13/afero"
)

// configureStartupSweep records the one-shot crash-recovery task during
// bootstrap without running any ledger or filesystem I/O. The API server starts
// it after BootstrapAPI has returned, so a slow ledger root cannot delay the
// bootstrap path.
func (r *APIRuntime) configureStartupSweep(fs afero.Fs, repo database.BatchFileOperationRepositoryInterface) {
	r.startupSweep = func() {
		startStartupSweep(r, fs, repo)
	}
}

// StartStartupSweep starts the server-owned, one-shot replacement sweep. It is
// intentionally called by the server construction path rather than bootstrap:
// startup repair is background work, and its context is cancelled by Shutdown.
func (r *APIRuntime) StartStartupSweep() {
	if r == nil {
		return
	}
	r.startupSweepOnce.Do(func() {
		if r.startupSweep != nil {
			r.startupSweep()
		}
	})
}

func startStartupSweep(rt *APIRuntime, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface) {
	if rt == nil || fs == nil || repo == nil {
		return
	}

	rt.EnsureRuntime()
	ctx := rt.ServerCtx()
	done := rt.TrackBackgroundTask()
	go func() {
		defer done()
		history.SweepOnStartupWithContext(ctx, fs, repo)
	}()
}
