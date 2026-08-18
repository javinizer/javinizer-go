package core

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestStartupSweepLaunchDoesNotBlockBootstrap(t *testing.T) {
	const (
		root   = "/blocked-ledger-root"
		dest   = root + "/poster.jpg"
		backup = dest + ".dlbak.0123456789abcdef"
	)

	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll(root, 0o755))
	require.NoError(t, afero.WriteFile(base, backup, []byte("pre-crash"), 0o644))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, base.Chtimes(backup, old, old))

	fs := &blockedStartupSweepFs{
		Fs:      base,
		dir:     root,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	repo := &startupSweepDBFixture{
		rows: []models.BatchFileOperation{{
			ID:             1,
			GeneratedFiles: `{"roots":["/blocked-ledger-root"]}`,
			RevertStatus:   models.RevertStatusApplied,
		}},
		ctxSeen: make(chan context.Context, 1),
	}

	rt := NewAPIRuntime(&APIDeps{})
	rt.EnsureRuntime()
	serverCtx := rt.ServerCtx()
	rt.configureStartupSweep(fs, repo)
	require.True(t, rt.WaitBackgroundTasks(50*time.Millisecond),
		"bootstrap configuration must not run the sweep inline")
	t.Cleanup(func() {
		fs.releaseReadDir()
		rt.Shutdown()
		_ = rt.WaitBackgroundTasks(500 * time.Millisecond)
	})

	// This is the server-owned launch that happens after bootstrap has returned.
	// The launch call itself must complete while the sweep is blocked in ReadDir.
	launchReturned := make(chan struct{})
	go func() {
		rt.StartStartupSweep()
		close(launchReturned)
	}()
	select {
	case <-launchReturned:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("startup sweep launch blocked bootstrap")
	}
	select {
	case <-fs.started:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("startup sweep did not reach the blocked filesystem call")
	}

	select {
	case got := <-repo.ctxSeen:
		require.Same(t, serverCtx, got, "sweep must use the server lifetime context")
	case <-time.After(250 * time.Millisecond):
		t.Fatal("startup sweep did not begin its database scan")
	}
	require.False(t, rt.WaitBackgroundTasks(50*time.Millisecond),
		"the blocked sweep must remain outstanding after bootstrap returns")

	// A second server-construction call is still one-shot; it must not start a
	// second sweep while the first one is blocked.
	rt.StartStartupSweep()
	fs.releaseReadDir()
	require.True(t, rt.WaitBackgroundTasks(500*time.Millisecond),
		"background startup sweep did not complete after the filesystem unblocked")

	got, err := afero.ReadFile(base, dest)
	require.NoError(t, err)
	require.Equal(t, []byte("pre-crash"), got, "the background task must still run sweep repair logic")
	exists, err := afero.Exists(base, backup)
	require.NoError(t, err)
	require.True(t, exists, "an unjournaled marker remains conservatively inspectable")

	rt.Shutdown()
	select {
	case <-serverCtx.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatal("startup sweep context was not cancelled by runtime shutdown")
	}
}

func TestStartStartupSweep_NilAndUnconfiguredAreNoop(t *testing.T) {
	var nilRuntime *APIRuntime
	nilRuntime.StartStartupSweep()
	startStartupSweep(nil, nil, nil)

	rt := NewAPIRuntime(&APIDeps{})
	rt.EnsureRuntime()
	rt.StartStartupSweep()
	require.True(t, rt.WaitBackgroundTasks(100*time.Millisecond))
	startStartupSweep(rt, nil, nil)
}

type blockedStartupSweepFs struct {
	afero.Fs
	dir         string
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
}

func (f *blockedStartupSweepFs) Open(name string) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.dir) {
		f.startedOnce.Do(func() { close(f.started) })
		<-f.release
	}
	return f.Fs.Open(name)
}

func (f *blockedStartupSweepFs) releaseReadDir() {
	f.releaseOnce.Do(func() { close(f.release) })
}

type startupSweepDBFixture struct {
	database.BatchFileOperationRepositoryInterface
	rows    []models.BatchFileOperation
	ctxSeen chan context.Context
}

func (r *startupSweepDBFixture) FindOperationsWithReplacements(context.Context) ([]models.BatchFileOperation, error) {
	return nil, nil
}

func (r *startupSweepDBFixture) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	r.ctxSeen <- ctx
	return r.rows, nil
}

func (r *startupSweepDBFixture) FindOperationsByDestination(context.Context, string) ([]models.BatchFileOperation, error) {
	return nil, nil
}
