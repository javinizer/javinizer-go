package core

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func newPosterEpochRuntime(t *testing.T) (*APIRuntime, *config.Config, *config.Config) {
	t.Helper()
	a := config.DefaultConfig(nil, nil)
	a.System.TempDir = filepath.Join(t.TempDir(), "epoch-A")
	b := a.Clone()
	b.System.TempDir = filepath.Join(t.TempDir(), "epoch-B")
	fs := afero.NewMemMapFs()
	rt := NewAPIRuntime(&APIDeps{CoreDeps: &commandutil.CoreDeps{}, Fs: fs})
	rt.ReplaceReloadable(a, scraperutil.NewScraperRegistry())
	rt.EnsureRuntime()
	t.Cleanup(func() { rt.GetRuntime().Shutdown(); require.NoError(t, rt.deps.CoreDeps.Close()) })
	writeCoreTestJPEG(t, fs, filepath.Join(a.System.TempDir, "posters", "job", "poster-full.jpg"), 200, 300)
	writeCoreTestJPEG(t, fs, filepath.Join(b.System.TempDir, "posters", "job", "poster-full.jpg"), 400, 500)
	return rt, a, b
}

func assertPosterEpoch(t *testing.T, pm poster.PosterManagerInterface, root string, width int) {
	t.Helper()
	require.NotNil(t, pm)
	result, err := pm.CropWithBounds(t.Context(), "job", "poster", 0, 0, 100, 100, 0)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "posters", "job", "poster-full.jpg"), result.FullPath)
	require.Equal(t, filepath.Join(root, "posters", "job", "poster.jpg"), result.CroppedPath)
	require.Equal(t, width, result.SourceWidth)
}

func awaitPosterConstructionBlocked(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		for _, stack := range strings.Split(string(buf[:n]), "\n\n") {
			if strings.Contains(stack, "TestPosterManagerConstructionPinsReloadEpoch") && strings.Contains(stack, "(*RuntimeState).GetPosterManager") && strings.Contains(stack, "sync.(*Mutex).Lock") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("poster getter did not block on construction lock")
		}
		runtime.Gosched()
	}
}

func TestPosterManagerConstructionPinsReloadEpoch(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		name := "direct"
		if snapshot {
			name = "snapshot"
		}
		t.Run(name, func(t *testing.T) {
			rt, a, b := newPosterEpochRuntime(t)
			old := rt.Snapshot()
			state := rt.GetRuntime()
			state.posterMgrCreateMu.Lock()
			var once sync.Once
			release := func() { once.Do(state.posterMgrCreateMu.Unlock) }
			var workers sync.WaitGroup
			defer joinLifecycleWorkers(t, release, &workers)
			result := make(chan poster.PosterManagerInterface, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				if snapshot {
					result <- old.PosterManager()
				} else {
					result <- rt.GetPosterManager()
				}
			}()
			awaitPosterConstructionBlocked(t)
			acquired := rt.reloadMu.TryLock()
			if acquired {
				rt.reloadMu.Unlock()
			}
			require.False(t, acquired, "a getter waiting for cache construction must pin its config epoch")
			reloaded := make(chan struct{})
			workers.Add(1)
			go func() {
				defer workers.Done()
				rt.ReplaceReloadable(b, scraperutil.NewScraperRegistry())
				close(reloaded)
			}()
			awaitReloadWriter(t, rt)
			release()
			first := awaitLifecycle(t, result)
			awaitLifecycle(t, reloaded)
			assertPosterEpoch(t, first, a.System.TempDir, 200)
			current := rt.Snapshot().PosterManager()
			require.NotSame(t, first, current)
			assertPosterEpoch(t, current, b.System.TempDir, 400)
			stale := old.PosterManager()
			require.NotSame(t, current, stale)
			assertPosterEpoch(t, stale, a.System.TempDir, 200)
			require.Same(t, current, rt.GetPosterManager())
			require.Same(t, current, rt.Snapshot().PosterManager())
		})
	}
}

func TestStalePosterSnapshotBypassesCurrentCache(t *testing.T) {
	for _, populate := range []bool{false, true} {
		name := "empty_current_cache"
		if populate {
			name = "populated_current_cache"
		}
		t.Run(name, func(t *testing.T) {
			rt, a, b := newPosterEpochRuntime(t)
			old := rt.Snapshot()
			rt.ReplaceReloadable(b, scraperutil.NewScraperRegistry())
			var current poster.PosterManagerInterface
			if populate {
				current = rt.GetPosterManager()
			}
			state := rt.GetRuntime()
			state.posterMgrCreateMu.Lock()
			var once sync.Once
			release := func() { once.Do(state.posterMgrCreateMu.Unlock) }
			var workers sync.WaitGroup
			defer joinLifecycleWorkers(t, release, &workers)
			result := make(chan poster.PosterManagerInterface, 1)
			workers.Add(1)
			go func() { defer workers.Done(); result <- old.PosterManager() }()
			stale := awaitLifecycle(t, result)
			assertPosterEpoch(t, stale, a.System.TempDir, 200)
			release()
			if current == nil {
				current = rt.GetPosterManager()
			}
			require.NotSame(t, current, stale)
			assertPosterEpoch(t, current, b.System.TempDir, 400)
			require.Same(t, current, rt.Snapshot().PosterManager())
		})
	}
}
