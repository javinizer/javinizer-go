package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestRuntimeBatchFactoryCachesAndReloadsWithoutPoisoningCurrentEpoch(t *testing.T) {
	root := t.TempDir()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "jobs.db"), LogLevel: "silent"})
	require.NoError(t, err)
	owned := &commandutil.CoreDeps{DB: db}
	var rt *core.APIRuntime
	t.Cleanup(func() {
		// All snapshot/factory use finishes before this sole owner's close.
		// Reloaded registries remain owned by CoreDeps until consumers drain.
		if rt != nil {
			if state := rt.GetRuntime(); state != nil {
				state.Shutdown()
			}
			if err := rt.CloseAfterHTTPDrain(time.Second); err != nil {
				t.Errorf("close runtime after consumers drained: %v", err)
			}
		} else if err := owned.Close(); err != nil {
			t.Errorf("close partially constructed dependencies: %v", err)
		}
	})
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repos := db.Repositories()
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join(root, "temporary")
	cfg.Performance.MaxWorkers = 1
	fs := afero.NewOsFs()
	store := worker.NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo,
		cfg.System.TempDir, template.NewEngine(), fs, worker.WithEditTransactor(db))
	rt = core.NewAPIRuntime(&core.APIDeps{CoreDeps: owned, Repos: repos, JobStore: store, Fs: fs})
	initialRegistry := scraperutil.NewScraperRegistry()
	rt.ReplaceReloadable(cfg, initialRegistry)
	before := rt.Snapshot()
	initial := rt.GetBatchJobFactory()
	require.NotNil(t, initial, "ordinary valid dependencies must produce a usable factory")
	require.Same(t, initial, rt.GetBatchJobFactory(), "same epoch must reuse the lazy factory")
	require.Same(t, initial, before.BatchJobFactory(), "current snapshots share the same cache")

	file := filepath.Join(root, "IPX-001.mp4")
	require.NoError(t, os.WriteFile(file, []byte("owned metadata-only fixture"), 0600))
	createAndCheck := func(factory worker.BatchJobFactoryInterface, destination string) {
		t.Helper()
		job := factory.CreateJob([]string{file}, worker.BatchJobOptions{Destination: destination})
		require.NotNil(t, job)
		require.NotEmpty(t, job.GetID())
		status := job.GetStatus()
		require.Equal(t, models.JobStatusPending, status.Status)
		require.Equal(t, []string{file}, status.Files)
		require.Equal(t, destination, status.Destination)
		registered, found := store.GetBatchJob(job.GetID())
		require.True(t, found, "factory must use the actual registered JobStore")
		require.Equal(t, job.GetID(), registered.GetID())
		persisted, err := repos.JobRepo.FindByID(context.Background(), job.GetID())
		require.NoError(t, err)
		require.NotNil(t, persisted, "factory creation must persist the real job row")
		require.Equal(t, job.GetID(), persisted.ID)
	}
	createAndCheck(initial, filepath.Join(root, "before"))

	reloaded := cfg.Clone()
	reloaded.Performance.MaxWorkers = 7
	nextRegistry := scraperutil.NewScraperRegistry()
	rt.ReplaceReloadable(reloaded, nextRegistry)
	current := rt.GetBatchJobFactory()
	require.NotNil(t, current)
	require.NotSame(t, initial, current, "reload must invalidate the previous factory")
	require.Same(t, current, rt.GetBatchJobFactory())
	require.Equal(t, 7, rt.Snapshot().APIConfig().BatchConfig().MaxWorkers)
	require.Equal(t, 1, before.APIConfig().BatchConfig().MaxWorkers)
	require.Same(t, initialRegistry, before.Registry())
	createAndCheck(current, filepath.Join(root, "current"))

	// A request retaining an older snapshot can finish while its dependencies
	// are still owned. Its fresh old-epoch factory must not replace the cache.
	oldEpoch := before.BatchJobFactory()
	require.NotNil(t, oldEpoch)
	require.NotSame(t, current, oldEpoch)
	createAndCheck(oldEpoch, filepath.Join(root, "older-request"))
	require.Same(t, current, rt.GetBatchJobFactory(), "old snapshot must not poison the current cache")
	createAndCheck(initial, filepath.Join(root, "retained-factory"))
}
