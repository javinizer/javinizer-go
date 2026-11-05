package core_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPosterManagerConcurrentMissesShareConstructionAfterInvalidation(t *testing.T) {
	state := core.NewRuntimeState()
	first := concurrentPosterManager(t, state)
	state.InvalidatePosterManager()
	second := concurrentPosterManager(t, state)
	require.NotSame(t, first, second, "invalidation must discard the previous manager")
}

func concurrentPosterManager(t *testing.T, state *core.RuntimeState) poster.PosterManagerInterface {
	t.Helper()
	const contenders = 32
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var workers sync.WaitGroup
	defer func() {
		unblock()
		cancel()
		workers.Wait()
	}()
	var calls atomic.Int32
	var enteredOnce sync.Once
	factory := func() poster.PosterManagerInterface {
		calls.Add(1)
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
		}
		return poster.NewPosterManager(afero.NewMemMapFs(), "posters", http.DefaultClient, time.Second)
	}
	results := make(chan poster.PosterManagerInterface, contenders+1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		results <- state.GetPosterManager(factory)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("initial poster construction did not start")
	}

	// Keep the first construction in flight while starting other callers.
	// The handshake increases contention without sleeps or private cache access;
	// the contract holds regardless of which callers reach the cache first.
	ready := make(chan struct{})
	for range contenders {
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case ready <- struct{}{}:
			case <-ctx.Done():
				return
			}
			results <- state.GetPosterManager(factory)
		}()
	}
	for range contenders {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("concurrent poster callers did not start")
		}
	}
	unblock()

	var manager poster.PosterManagerInterface
	for range contenders + 1 {
		select {
		case got := <-results:
			require.NotNil(t, got)
			if manager == nil {
				manager = got
			}
			require.Same(t, manager, got, "concurrent misses must share the constructed manager")
		case <-ctx.Done():
			t.Fatal("concurrent poster callers did not finish")
		}
	}
	require.EqualValues(t, 1, calls.Load(), "concurrent misses must construct only once")
	require.Same(t, manager, state.GetPosterManager(factory), "subsequent reads must use the cache")
	require.EqualValues(t, 1, calls.Load(), "a cache hit must not invoke the factory")
	return manager
}
