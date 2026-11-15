package core

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/stretchr/testify/require"
)

func TestOfflineEvidencePinPanicReleasesOnlyOwnedLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := NewAPIRuntime(nil)
		request, cancelRequest := context.WithCancel(t.Context())
		defer cancelRequest()
		sibling, releaseSibling := rt.LinkOfflineEvidenceOperationContext(request, true)
		defer releaseSibling()
		var linked context.Context
		resourceClosed := false
		panicValue := &struct{ reason string }{"pinning failed"}

		require.PanicsWithValue(t, panicValue, func() {
			_, _, _ = rt.beginOfflineScrapeEvidenceForFiles(request, true, 7, func(ctx context.Context, count int) (context.Context, func()) {
				linked = ctx
				require.Equal(t, 7, count)
				require.NoError(t, ctx.Err())
				require.False(t, rt.WaitBackgroundTasks(time.Millisecond), "admission must precede pinning")
				// A pinning dependency unwinds its own resource before the outer
				// runtime cancels and releases the admitted operation.
				defer func() {
					require.NoError(t, ctx.Err())
					require.False(t, rt.WaitBackgroundTasks(time.Millisecond))
					resourceClosed = true
				}()
				panic(panicValue)
			})
		})
		// Join cancellation callbacks and timed-out wait probes before starting
		// another task, so a new WaitGroup generation cannot race an old Wait.
		synctest.Wait()
		require.True(t, resourceClosed)
		require.ErrorIs(t, linked.Err(), context.Canceled, "panic cleanup must cancel the abandoned operation")
		require.NoError(t, request.Err(), "the caller still owns its request")
		require.NoError(t, sibling.Err(), "another operation must retain its lifetime")
		require.NoError(t, rt.ServerCtx().Err(), "a pinning panic must not shut down the server")
		require.True(t, rt.WaitBackgroundTasks(time.Second), "panic cleanup must release task admission")

		// The public path remains usable after the failed call and retains its
		// admission until the new caller explicitly releases it.
		fresh, release, err := rt.BeginOfflineScrapeEvidence(request, true)
		require.NoError(t, err)
		require.NoError(t, fresh.Err())
		require.False(t, rt.WaitBackgroundTasks(time.Millisecond))
		release()
		release()
		require.ErrorIs(t, fresh.Err(), context.Canceled)
		require.True(t, rt.WaitBackgroundTasks(time.Second))
		rt.Shutdown()
	})
}

func TestOfflineEvidencePinReleaseCancelsBeforeResourceAndAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := NewAPIRuntime(nil)
		request := t.Context()
		type pinnedKey struct{}
		var pinned context.Context
		releases := 0
		ctx, release, err := rt.beginOfflineScrapeEvidenceForFiles(request, true, 3, func(linked context.Context, count int) (context.Context, func()) {
			require.Equal(t, 3, count)
			require.NotSame(t, request, linked)
			require.NoError(t, linked.Err())
			pinned = context.WithValue(linked, pinnedKey{}, "generation")
			return pinned, func() {
				releases++
				require.ErrorIs(t, linked.Err(), context.Canceled, "cancel before releasing pinned resources")
				require.False(t, rt.WaitBackgroundTasks(time.Millisecond), "retain task admission until resource release returns")
			}
		})
		require.NoError(t, err)
		require.Same(t, pinned, ctx)
		require.Equal(t, "generation", ctx.Value(pinnedKey{}))
		require.NoError(t, ctx.Err(), "successful pinning transfers the live operation to its caller")
		require.Zero(t, releases)
		require.False(t, rt.WaitBackgroundTasks(time.Millisecond))
		release()
		release()
		require.Equal(t, 1, releases)
		require.True(t, rt.WaitBackgroundTasks(time.Second))
		require.NoError(t, request.Err())
		require.NoError(t, rt.ServerCtx().Err())
		rt.Shutdown()
	})
}

func TestOfflineEvidencePinPanicUnblocksShutdownAfterResourceCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewTokenStore().(*tokenStore)
		rt := NewAPIRuntime(&APIDeps{TokenStore: store})
		entered := make(chan context.Context, 1)
		failPin := make(chan struct{})
		recovered := make(chan any, 1)
		resourceClosed := make(chan struct{})
		panicValue := &struct{ reason string }{"load interrupted"}
		go func() {
			defer func() { recovered <- recover() }()
			_, _, _ = rt.beginOfflineScrapeEvidenceForFiles(t.Context(), true, 1, func(linked context.Context, _ int) (context.Context, func()) {
				defer close(resourceClosed)
				entered <- linked
				<-failPin
				panic(panicValue)
			})
		}()
		linked := <-entered
		stopped := make(chan struct{})
		go func() { rt.Shutdown(); close(stopped) }()
		synctest.Wait()
		require.ErrorIs(t, linked.Err(), context.Canceled)
		require.Nil(t, rt.TrackServerTask(), "shutdown must close further admission")
		select {
		case <-stopped:
			t.Fatal("shutdown escaped an in-flight pin")
		case <-store.done:
			t.Fatal("dependencies closed before pinning cleanup")
		default:
		}
		close(failPin)
		require.Same(t, panicValue, <-recovered, "cleanup must preserve the original panic")
		synctest.Wait()
		select {
		case <-resourceClosed:
		default:
			t.Fatal("pinning resource was not closed")
		}
		select {
		case <-stopped:
		default:
			t.Fatal("shutdown did not join the failed operation")
		}
		select {
		case <-store.done:
		default:
			t.Fatal("dependencies were not closed after cleanup")
		}
		require.True(t, rt.WaitBackgroundTasks(time.Second))
		require.NoError(t, t.Context().Err())
		rt.Shutdown()
	})
}

func TestOfflineEvidencePinBoundaryRejectsBeforeDependencyAccess(t *testing.T) {
	pin := func(context.Context, int) (context.Context, func()) {
		t.Fatal("pinning must not run without opted-in admission")
		return nil, nil
	}
	var absent *APIRuntime
	parent := t.Context()
	off, release, err := absent.beginOfflineScrapeEvidenceForFiles(parent, false, 1, pin)
	require.NoError(t, err)
	require.Same(t, parent, off)
	require.Nil(t, scrapeevidence.Resolve(off, nil, false))
	release()

	for _, rt := range []*APIRuntime{absent, NewAPIRuntime(nil)} {
		if rt != nil {
			rt.Shutdown()
		}
		ctx, release, err := rt.beginOfflineScrapeEvidenceForFiles(parent, true, 1, pin)
		require.ErrorIs(t, err, ErrOfflineEvidenceShuttingDown)
		require.Same(t, parent, ctx)
		require.Nil(t, release)
	}
	require.NoError(t, parent.Err())
}
