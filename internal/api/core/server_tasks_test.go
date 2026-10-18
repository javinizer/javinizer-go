package core

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerTaskShutdownJoinsBeforeDependencyClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewTokenStore().(*tokenStore)
		rt := NewAPIRuntime(&APIDeps{TokenStore: store})
		release := rt.TrackServerTask()
		require.NotNil(t, release)
		stopped := make(chan struct{})
		go func() { rt.Shutdown(); close(stopped) }()
		synctest.Wait()
		require.Error(t, rt.ServerCtx().Err())
		require.Nil(t, rt.TrackServerTask(), "late admission must be rejected")
		select {
		case <-stopped:
			t.Fatal("shutdown returned before task cleanup")
		default:
		}
		select {
		case <-store.done:
			t.Fatal("dependency closed before task cleanup")
		default:
		}
		require.False(t, rt.WaitBackgroundTasks(time.Millisecond))
		release()
		synctest.Wait()
		select {
		case <-stopped:
		default:
			t.Fatal("shutdown failed to join")
		}
		select {
		case <-store.done:
		default:
			t.Fatal("dependency was not closed")
		}
		require.True(t, rt.WaitBackgroundTasks(time.Second))
		rt.Shutdown()
	})
}

func TestServerTaskConcurrentAdmissionAndShutdown(t *testing.T) {
	for range 100 {
		rt := NewAPIRuntime(&APIDeps{})
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				<-start
				if done := rt.TrackServerTask(); done != nil {
					<-rt.ServerCtx().Done()
					done()
				}
			})
		}
		wg.Go(func() { <-start; rt.Shutdown() })
		close(start)
		wg.Wait()
		require.Nil(t, rt.TrackServerTask())
	}
}
