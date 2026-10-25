package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

type offlineRuntimeRepository struct {
	database.ActressRepositoryInterface
	actressnamesync.Repository
	due func(context.Context) ([]database.ActressSyncJob, error)
}

func (r *offlineRuntimeRepository) DueOfflineNameSyncJobs(ctx context.Context, _ time.Time, _ string, _ int) ([]database.ActressSyncJob, error) {
	return r.due(ctx)
}

func newOfflineRuntime(t *testing.T, cfg *config.Config, repo database.ActressRepositoryInterface) *APIRuntime {
	t.Helper()
	rt := NewAPIRuntime(&APIDeps{Repos: database.Repositories{ContentRepos: database.ContentRepos{ActressRepo: repo}}})
	rt.SetConfig(cfg)
	t.Cleanup(rt.Shutdown)
	return rt
}

func TestOfflineNameSyncRuntimePinsSettingsWithoutArtifactIO(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.ActressDatabase.OfflineNameSync.Enabled = true
	cfg.Metadata.ActressDatabase.OfflineNameSync.MaxAgeHours = 1.5
	dir := filepath.Join(t.TempDir(), "not-provisioned")
	cfg.Metadata.ActressDatabase.OfflineNameSync.ArtifactDirectory = dir
	rt := newOfflineRuntime(t, cfg, &offlineRuntimeRepository{})
	svc := rt.OfflineNameSync()
	require.NotNil(t, svc)
	info, err := svc.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, "starting", info.Code)
	require.True(t, info.Enabled)
	require.Equal(t, int64(5400), info.MaxAgeSeconds)
	require.True(t, info.RestartRequiredForConfigChanges)
	select {
	case <-svc.Ready():
		t.Fatal("service getter must not initialize the artifact")
	default:
	}
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)

	reloaded := cfg.Clone()
	reloaded.Metadata.ActressDatabase.OfflineNameSync.Enabled = false
	reloaded.Metadata.ActressDatabase.OfflineNameSync.MaxAgeHours = 240
	reloaded.Metadata.ActressDatabase.OfflineNameSync.ArtifactDirectory = filepath.Join(t.TempDir(), "different")
	rt.SetConfig(reloaded)
	require.Same(t, svc, rt.OfflineNameSync())
	info, err = svc.Info(t.Context())
	require.NoError(t, err)
	require.True(t, info.Enabled, "hot reload must not change the pinned service")
	require.Equal(t, int64(5400), info.MaxAgeSeconds)
}

func TestOfflineNameSyncRuntimeDisabledDoesNotScanRepository(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Metadata.ActressDatabase.Enabled = false
		rt := newOfflineRuntime(t, cfg, &offlineRuntimeRepository{due: func(context.Context) ([]database.ActressSyncJob, error) {
			t.Error("disabled service must not scan queued jobs")
			return nil, nil
		}})
		svc := rt.OfflineNameSync()
		require.NotNil(t, svc, "disabled service remains available for durable status and cancel")
		rt.StartOfflineNameSync()
		rt.StartOfflineNameSync()
		synctest.Wait()
		requireClosedStartupSweepSignal(t, svc.Ready(), "disabled initialization must complete")
		info, err := svc.Info(t.Context())
		require.NoError(t, err)
		require.Equal(t, "disabled", info.Code)
		require.False(t, info.Enabled)
		require.True(t, rt.WaitBackgroundTasks(time.Second))
	})
}

func TestOfflineNameSyncRuntimeLegacyAbsentSettingsRemainDisabled(t *testing.T) {
	cfg := &config.Config{}
	rt := newOfflineRuntime(t, cfg, &offlineRuntimeRepository{})
	svc := rt.OfflineNameSync()
	require.NotNil(t, svc)
	info, err := svc.Info(t.Context())
	require.NoError(t, err)
	require.False(t, info.Enabled)
	require.Equal(t, int64(168*60*60), info.MaxAgeSeconds)
}

func TestOfflineNameSyncRuntimeShutdownJoinsWorkerCleanup(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "not-provisioned")
	synctest.Test(t, func(t *testing.T) {
		started := make(chan context.Context, 1)
		canceled := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
		calls := 0
		repo := &offlineRuntimeRepository{due: func(ctx context.Context) ([]database.ActressSyncJob, error) {
			calls++
			started <- ctx
			<-ctx.Done()
			close(canceled)
			<-release
			return nil, ctx.Err()
		}}
		cfg := config.DefaultConfig(nil, nil)
		cfg.Metadata.ActressDatabase.OfflineNameSync.Enabled = true
		cfg.Metadata.ActressDatabase.OfflineNameSync.ArtifactDirectory = missingDir
		rt := newOfflineRuntime(t, cfg, repo)
		store := NewTokenStore().(*tokenStore)
		rt.deps.TokenStore = store
		defer func() { releaseWorker(); rt.Shutdown() }()
		rt.StartOfflineNameSync()
		rt.StartOfflineNameSync()
		synctest.Wait()
		select {
		case ctx := <-started:
			require.Same(t, rt.ServerCtx(), ctx)
		default:
			t.Fatal("server-owned worker did not start asynchronously")
		}
		require.Equal(t, 1, calls, "repeated starts must not duplicate workers")
		stopped := make(chan struct{})
		go func() { rt.Shutdown(); close(stopped) }()
		synctest.Wait()
		requireClosedStartupSweepSignal(t, canceled, "shutdown must cancel the worker")
		select {
		case <-stopped:
			t.Fatal("shutdown returned before worker cleanup")
		default:
		}
		select {
		case <-store.done:
			t.Fatal("dependency closed before worker cleanup")
		default:
		}
		releaseWorker()
		synctest.Wait()
		requireClosedStartupSweepSignal(t, stopped, "shutdown must join worker cleanup")
		requireClosedStartupSweepSignal(t, store.done, "dependency should close after worker cleanup")
	})
}

func TestOfflineNameSyncRuntimeUnavailableAndStopped(t *testing.T) {
	var absent *APIRuntime
	require.Nil(t, absent.OfflineNameSync())
	absent.StartOfflineNameSync()
	for _, deps := range []*APIDeps{nil, {}, {Repos: database.Repositories{ContentRepos: database.ContentRepos{ActressRepo: &offlineRuntimeRepository{}}}}} {
		rt := NewAPIRuntime(deps)
		require.Nil(t, rt.OfflineNameSync())
		rt.StartOfflineNameSync()
		rt.Shutdown()
	}
	cfg := config.DefaultConfig(nil, nil)
	rt := newOfflineRuntime(t, cfg, nil)
	require.Nil(t, rt.OfflineNameSync(), "unrelated repository mocks need not gain sync methods")
	invalid := cfg.Clone()
	invalid.Metadata.ActressDatabase.OfflineNameSync.MaxQueuedJobs = 33
	rt = newOfflineRuntime(t, invalid, &offlineRuntimeRepository{})
	require.Nil(t, rt.OfflineNameSync(), "invalid settings must fail closed")
	rt.StartOfflineNameSync()
	rt = newOfflineRuntime(t, cfg, &offlineRuntimeRepository{})
	rt.Shutdown()
	rt.StartOfflineNameSync()
	require.Nil(t, rt.Runtime, "post-shutdown start must reject admission before constructing runtime state")
	require.Nil(t, rt.OfflineNameSync(), "post-shutdown getter must not construct a fresh service")
}
