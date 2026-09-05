package testkit

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
)

// Regression coverage for the Windows CI failure where tests hitting the
// async-launch endpoints (updateBatchJob/organizeJob → prepareAndLaunchApply)
// reached teardown with the background phase still holding test.db open, so
// t.TempDir()'s RemoveAll failed with "being used by another process".
// The drain joins tracked handler goroutines — not job status, which lags in
// both directions (pre-markStarted window; terminal status set before the
// phase's deferred persistence runs).

func TestDrainBackgroundTasks_NoTasksReturnsImmediately(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := CreateTestDeps(t, cfg, "")

	start := time.Now()
	drainBackgroundTasks(t, GetTestRuntime(deps), 5*time.Second)
	assert.Less(t, time.Since(start), 500*time.Millisecond,
		"drain must not wait when no background task is tracked")
}

func TestDrainBackgroundTasks_WaitsForTrackedTask(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := CreateTestDeps(t, cfg, "")
	rt := GetTestRuntime(deps)

	release := make(chan struct{})
	done := rt.TrackBackgroundTask()
	go func() {
		<-release
		done()
	}()

	drained := make(chan struct{})
	go func() {
		drainBackgroundTasks(t, rt, 10*time.Second)
		close(drained)
	}()

	select {
	case <-drained:
		t.Fatal("drain returned while the tracked task was still held")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not return after the tracked task released")
	}
}

func TestDrainBackgroundTasks_BudgetExpiryDoesNotHang(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := CreateTestDeps(t, cfg, "")
	rt := GetTestRuntime(deps)

	done := rt.TrackBackgroundTask()
	// Release after the measurement (defers run before cleanups), so
	// CreateTestDeps' own teardown drain is not held open by this test.
	defer done()

	const budget = 300 * time.Millisecond
	start := time.Now()
	drainBackgroundTasks(t, rt, budget)
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, budget-100*time.Millisecond,
		"drain should keep waiting until the budget expires for a stuck task")
	assert.Less(t, elapsed, 3*time.Second,
		"drain must give up shortly after the budget, not hang forever")
}

func TestDrainBackgroundTasks_NilRuntime(t *testing.T) {
	// Defensive: GetTestRuntime on nil deps can return nil.
	drainBackgroundTasks(t, nil, time.Second)
}
