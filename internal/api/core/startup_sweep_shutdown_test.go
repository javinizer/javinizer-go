package core

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestStartupSweepShutdownJoinsPostCancellationCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		canceled := make(chan struct{})
		cleanupRelease := make(chan struct{})
		finished := make(chan struct{})
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(cleanupRelease) }) }
		repo := &startupSweepShutdownRepo{findReplacements: func(ctx context.Context) ([]models.BatchFileOperation, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			// Cancellation is observed before the worker has released its resources.
			<-cleanupRelease
			close(finished)
			return nil, ctx.Err()
		}}
		store := NewTokenStore().(*tokenStore)
		rt := NewAPIRuntime(&APIDeps{TokenStore: store})
		rt.configureStartupSweep(afero.NewMemMapFs(), repo)
		defer func() {
			release()
			rt.Shutdown()
			synctest.Wait()
		}()
		rt.StartStartupSweep()
		synctest.Wait()
		requireClosedStartupSweepSignal(t, started, "startup sweep did not enter the repository")

		stopped := make(chan struct{})
		go func() { rt.Shutdown(); close(stopped) }()
		synctest.Wait()
		requireClosedStartupSweepSignal(t, canceled, "shutdown did not cancel the sweep")
		select {
		case <-stopped:
			t.Error("shutdown returned before startup sweep cleanup completed")
		default:
		}
		select {
		case <-store.done:
			t.Error("runtime dependency closed before startup sweep cleanup completed")
		default:
		}

		release()
		synctest.Wait()
		requireClosedStartupSweepSignal(t, finished, "startup sweep cleanup did not finish")
		requireClosedStartupSweepSignal(t, stopped, "shutdown did not join the completed sweep")
		requireClosedStartupSweepSignal(t, store.done, "runtime dependency did not close after the sweep")
	})
}

func TestStartupSweepShutdownJoinsBlockedFilesystem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const root = "/blocked-startup-root"
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll(root, 0o755))
		fs := &blockedStartupSweepFs{Fs: base, dir: root, started: make(chan struct{}), release: make(chan struct{})}
		repo := &startupSweepDBFixture{
			rows:    []models.BatchFileOperation{{ID: 1, GeneratedFiles: `{"roots":["/blocked-startup-root"]}`, RevertStatus: models.RevertStatusApplied}},
			ctxSeen: make(chan context.Context, 1),
		}
		rt := NewAPIRuntime(&APIDeps{})
		rt.configureStartupSweep(fs, repo)
		defer func() {
			fs.releaseReadDir()
			rt.Shutdown()
			synctest.Wait()
		}()
		rt.StartStartupSweep()
		synctest.Wait()
		requireClosedStartupSweepSignal(t, fs.started, "startup sweep did not enter filesystem I/O")

		stopped := make(chan struct{})
		go func() { rt.Shutdown(); close(stopped) }()
		synctest.Wait()
		require.ErrorIs(t, rt.ServerCtx().Err(), context.Canceled)
		// afero.Open has no context parameter: cancellation cannot interrupt it.
		// Preserve dependency ownership until the outstanding I/O actually returns.
		select {
		case <-stopped:
			t.Error("shutdown returned while startup sweep filesystem I/O was still blocked")
		default:
		}
		fs.releaseReadDir()
		synctest.Wait()
		requireClosedStartupSweepSignal(t, stopped, "shutdown did not finish after filesystem I/O returned")
	})
}

func TestStartupSweepRejectsAdmissionAfterShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		repo := &startupSweepShutdownRepo{findReplacements: func(context.Context) ([]models.BatchFileOperation, error) {
			calls++
			return nil, nil
		}}
		rt := NewAPIRuntime(&APIDeps{})
		rt.configureStartupSweep(afero.NewMemMapFs(), repo)
		rt.Shutdown()
		require.Nil(t, rt.Runtime)
		rt.StartStartupSweep()
		synctest.Wait()
		require.Zero(t, calls, "a stopped runtime must not begin a repository scan")
		require.Nil(t, rt.Runtime, "rejected sweep admission must not initialize background task state")
	})
}

type startupSweepShutdownRepo struct {
	database.BatchFileOperationRepositoryInterface
	findReplacements func(context.Context) ([]models.BatchFileOperation, error)
}

func (r *startupSweepShutdownRepo) FindOperationsWithReplacements(ctx context.Context) ([]models.BatchFileOperation, error) {
	return r.findReplacements(ctx)
}

func (r *startupSweepShutdownRepo) FindOperationsWithLedger(context.Context) ([]models.BatchFileOperation, error) {
	return nil, nil
}

func requireClosedStartupSweepSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	default:
		t.Fatal(message)
	}
}
