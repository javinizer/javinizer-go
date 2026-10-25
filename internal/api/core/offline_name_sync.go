package core

import (
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// The service is startup-owned rather than reloadable. In particular, changing
// enabled, the artifact directory, freshness, or queue policy cannot silently
// replace the artifact or policy behind an existing confirmation or worker.
type offlineNameSyncState struct {
	offlineNameSyncInitOnce  sync.Once
	offlineNameSyncStartOnce sync.Once
	offlineNameSyncService   *actressnamesync.Service
}

// OfflineNameSync returns the startup-pinned service. Construction is lazy for
// manually wired API runtimes, but bootstrap pins it before returning. Neither
// construction nor this getter performs artifact/database I/O or starts work.
// Repositories without the optional capability leave the service unavailable;
// unrelated actor API repository mocks need not implement the worker surface.
func (r *APIRuntime) OfflineNameSync() *actressnamesync.Service {
	if r == nil {
		return nil
	}
	r.offlineNameSyncInitOnce.Do(func() {
		ctx := r.ServerCtx()
		if ctx.Err() != nil || r.deps == nil || r.deps.CoreDeps == nil || !r.deps.CoreDeps.HasConfig() {
			return
		}
		repo, ok := r.deps.Repos.ActressRepo.(actressnamesync.Repository)
		if !ok {
			return
		}
		// Only one atomically published config is needed here. Do not require
		// a scraper registry, which minimal actor-only runtimes need not supply.
		actorCfg := r.deps.CoreDeps.GetConfig().Metadata.ActressDatabase
		if err := config.ValidateOfflineNameSync(actorCfg); err != nil {
			// Configuration errors are fail-closed and never disclose local paths.
			logging.Warn("Offline name sync unavailable: invalid startup settings")
			return
		}
		cfg := actorCfg.OfflineNameSync.Effective()
		svc, err := actressnamesync.New(ctx, repo, actressnamesync.Options{
			Enabled:           cfg.Enabled,
			ArtifactDirectory: cfg.ArtifactDirectory,
			MaxAge:            time.Duration(cfg.MaxAgeHours * float64(time.Hour)),
			QueueLimits: database.ActressSyncQueueLimits{
				MaxQueuedJobs:  cfg.MaxQueuedJobs,
				MaxQueuedTasks: cfg.MaxQueuedTasks,
			},
		})
		if err != nil {
			logging.Warn("Offline name sync unavailable: service initialization failed")
			return
		}
		r.offlineNameSyncService = svc
	})
	return r.offlineNameSyncService
}

// StartOfflineNameSync begins the single server-owned worker after bootstrap.
// Slow artifact I/O cannot block server construction. Task admission closes at
// shutdown, and Shutdown joins Run before dependencies or the database close.
func (r *APIRuntime) StartOfflineNameSync() {
	if r == nil {
		return
	}
	r.offlineNameSyncStartOnce.Do(func() {
		done := r.TrackServerTask()
		if done == nil {
			return
		}
		svc := r.OfflineNameSync()
		if svc == nil {
			done()
			return
		}
		go func() {
			defer done()
			svc.Run()
		}()
	})
}
