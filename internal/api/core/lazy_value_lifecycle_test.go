package core

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func awaitLifecycle[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle operation did not finish")
	}
	var zero T
	return zero
}

func awaitReloadWriter(t *testing.T, rt *APIRuntime) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for rt.reloadMu.TryRLock() {
		rt.reloadMu.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("reload writer did not queue")
		}
		runtime.Gosched()
	}
}

func TestLazyValueInvalidatedBuildRetries(t *testing.T) {
	for _, publishCurrent := range []bool{false, true} {
		name := "rebuild"
		if publishCurrent {
			name = "reuse_current"
		}
		t.Run(name, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var calls atomic.Int32
			lv := newLazyValue(func() any {
				n := calls.Add(1)
				if n == 1 {
					close(started)
					<-release
				}
				return n
			}, func(v any) bool { return v == nil })
			done := make(chan any, 1)
			go func() { done <- lv.Get() }()
			awaitLifecycle(t, started)
			lv.Invalidate()
			if publishCurrent {
				require.Equal(t, int32(2), lv.Get())
			}
			once.Do(func() { close(release) })
			require.Equal(t, int32(2), awaitLifecycle(t, done))
			require.Equal(t, int32(2), lv.Get())
			require.Equal(t, int32(2), calls.Load())
		})
	}
}

func TestLazyValueConcurrentInvalidationAndBuilders(t *testing.T) {
	lv := newLazyValue(func() any { runtime.Gosched(); return new(int) }, func(v any) bool { return v == nil })
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				if lv.Get() == nil {
					t.Error("published value unexpectedly nil")
				}
				runtime.Gosched()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 1000 {
			lv.Invalidate()
			runtime.Gosched()
		}
	}()
	wg.Wait()
}

func joinLifecycleWorkers(t *testing.T, release func(), wg *sync.WaitGroup) {
	t.Helper()
	release()
	joined := make(chan struct{})
	go func() { wg.Wait(); close(joined) }()
	awaitLifecycle(t, joined)
}

func newLifecycleRuntime(t *testing.T) *APIRuntime {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "lifecycle.db"), LogLevel: "silent"})
	require.NoError(t, err)
	deps := &APIDeps{CoreDeps: &commandutil.CoreDeps{DB: db}, Repos: db.Repositories(), JobStore: worker.NewInMemoryJobStore()}
	rt := NewAPIRuntime(deps)
	t.Cleanup(func() { require.NoError(t, deps.CoreDeps.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.MaxFilesPerScan = 100
	cfg.Performance.MaxWorkers = 1
	rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())
	return rt
}

func TestPublicWorkflowBuildSerializesReload(t *testing.T) {
	rt := newLifecycleRuntime(t)
	before := rt.Snapshot()
	original := rt.workflowFactory.build
	built := make(chan any, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var workers sync.WaitGroup
	defer joinLifecycleWorkers(t, unblock, &workers)
	rt.workflowFactory.build = func() any {
		f := original()
		select {
		case built <- f:
		default:
		}
		<-release
		return f
	}
	done := make(chan workflow.WorkflowInterface, 1)
	workers.Add(1)
	go func() { defer workers.Done(); done <- rt.GetWorkflow() }()
	require.Equal(t, 100, awaitLifecycle(t, built).(*workflow.WorkflowFactory).MaxFilesPerScan())
	cfgB := before.Config().Clone()
	cfgB.API.Security.MaxFilesPerScan = 200
	reloaded := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		rt.ReplaceReloadable(cfgB, scraperutil.NewScraperRegistry())
		close(reloaded)
	}()
	awaitReloadWriter(t, rt)
	unblock()
	require.NotNil(t, awaitLifecycle(t, done))
	awaitLifecycle(t, reloaded)
	snap := rt.Snapshot()
	current, err := snap.WorkflowFactory()
	require.NoError(t, err)
	require.Equal(t, 200, snap.APIConfig().MaxFilesPerScan)
	require.Equal(t, 200, current.MaxFilesPerScan())
	old, err := before.WorkflowFactory()
	require.NoError(t, err)
	require.Equal(t, 100, old.MaxFilesPerScan())
	again, err := rt.getWorkflowFactory()
	require.NoError(t, err)
	require.Same(t, current, again)
	require.NotNil(t, rt.GetWorkflow())
	require.Same(t, current.PosterGen(), rt.GetPosterGen())
}

type lifecycleJobStore struct {
	worker.JobStoreInterface
	mu      sync.Mutex
	workers []int
}

func (s *lifecycleJobStore) SetReconstructionDeps(m matcher.MatcherInterface, p poster.PosterGenerator, c worker.BatchJobConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.JobStoreInterface.SetReconstructionDeps(m, p, c)
	s.workers = append(s.workers, c.MaxWorkers)
}

func TestBatchFactoryBuildWithWaitingReload(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		name := "direct"
		if snapshot {
			name = "snapshot"
		}
		t.Run(name, func(t *testing.T) {
			rt := newLifecycleRuntime(t)
			store := &lifecycleJobStore{JobStoreInterface: rt.deps.JobStore}
			rt.deps.JobStore = store
			before := rt.Snapshot()
			original := rt.batchJobFactory.build
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var workers sync.WaitGroup
			defer joinLifecycleWorkers(t, unblock, &workers)
			var calls atomic.Int32
			rt.batchJobFactory.build = func() any {
				if calls.Add(1) == 1 {
					close(started)
					<-release
				}
				return original()
			}
			done := make(chan worker.BatchJobFactoryInterface, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				if snapshot {
					done <- before.BatchJobFactory()
				} else {
					done <- rt.GetBatchJobFactory()
				}
			}()
			awaitLifecycle(t, started)
			cfgB := before.Config().Clone()
			cfgB.Performance.MaxWorkers = 7
			reloaded := make(chan struct{})
			workers.Add(1)
			go func() {
				defer workers.Done()
				rt.ReplaceReloadable(cfgB, scraperutil.NewScraperRegistry())
				close(reloaded)
			}()
			awaitReloadWriter(t, rt)
			unblock()
			require.NotNil(t, awaitLifecycle(t, done))
			awaitLifecycle(t, reloaded)
			current := rt.GetBatchJobFactory()
			require.NotNil(t, current)
			require.Same(t, current, rt.Snapshot().BatchJobFactory())
			require.NotNil(t, before.BatchJobFactory())
			require.Same(t, current, rt.GetBatchJobFactory())
			store.mu.Lock()
			defer store.mu.Unlock()
			require.Equal(t, []int{1, 7}, store.workers)
		})
	}
}
