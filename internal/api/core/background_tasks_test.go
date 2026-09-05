package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAPIRuntime_BackgroundTaskTracking(t *testing.T) {
	t.Run("returns true immediately with no tracked tasks", func(t *testing.T) {
		rt := NewAPIRuntime(&APIDeps{})
		rt.EnsureRuntime()
		start := time.Now()
		assert.True(t, rt.WaitBackgroundTasks(5*time.Second))
		assert.Less(t, time.Since(start), 500*time.Millisecond)
	})

	t.Run("waits until the task releases", func(t *testing.T) {
		rt := NewAPIRuntime(&APIDeps{})
		rt.EnsureRuntime()
		done := rt.TrackBackgroundTask()

		release := make(chan struct{})
		go func() {
			<-release
			done()
		}()

		settled := make(chan bool, 1)
		go func() {
			settled <- rt.WaitBackgroundTasks(5 * time.Second)
		}()

		select {
		case ok := <-settled:
			t.Fatalf("wait returned %v while the tracked task was still held", ok)
		case <-time.After(100 * time.Millisecond):
		}

		close(release)
		select {
		case ok := <-settled:
			assert.True(t, ok)
		case <-time.After(5 * time.Second):
			t.Fatal("wait did not return after the tracked task released")
		}
	})

	t.Run("timeout reports false while task is still tracked", func(t *testing.T) {
		rt := NewAPIRuntime(&APIDeps{})
		rt.EnsureRuntime()
		done := rt.TrackBackgroundTask()

		assert.False(t, rt.WaitBackgroundTasks(100*time.Millisecond),
			"must report false when a task outlives the timeout")

		// After release, the counter drains to zero and the wait succeeds.
		done()
		assert.True(t, rt.WaitBackgroundTasks(5*time.Second))
	})
}
