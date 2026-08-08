package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Audit P1: a dispatchLoop restart after panic must not double-Done the
// WaitGroup — Stop's bounded wait must still return promptly.
func TestDispatchRestartWaitsClean(t *testing.T) {
	_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 77})
	manager.recoveryInterval = 10 * time.Second // keep recovery out of this test
	manager.ctx, manager.cancel = context.WithCancel(context.Background())
	manager.started = true
	manager.wg.Add(1)
	go manager.runDispatch(manager.ctx)

	time.Sleep(50 * time.Millisecond) // let the loop reach its select
	done := make(chan struct{})
	go func() { manager.wg.Wait(); close(done) }()
	manager.cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("wg.Wait blocked: WaitGroup counter desynced")
	}
	manager.started = false
}

// Audit P2: very long outages must not collapse the backoff to 0 (a raw bit
// shift overflows past 63 doublings precisely when an outage is longest).
func TestBackoffDelayLargeStreakCapped(t *testing.T) {
	m := &ActressSyncManager{}
	for _, streak := range []int{0, 1, 2, 6, 63, 100, 1000} {
		got := m.backoffDelay(streak)
		require.GreaterOrEqual(t, got, time.Second, "streak %d collapsed below base", streak)
		require.LessOrEqual(t, got, 60*time.Second, "streak %d exceeded cap", streak)
	}
	require.Equal(t, time.Second, m.backoffDelay(1))
	require.Equal(t, 2*time.Second, m.backoffDelay(2))
	require.Equal(t, 32*time.Second, m.backoffDelay(6))
	require.Equal(t, 60*time.Second, m.backoffDelay(60)) // clamped at the cap
}

// Audit P2: Shutdown latches permanently — held references cannot restart the
// engine or create jobs after runtime shutdown. Plain Stop stays restartable
// (hot reload path).
func TestShutdownLatchRefusesStartAndCreate(t *testing.T) {
	_, _, _, manager := newFinalManagerFixture(t, &models.Actress{DMMID: 78})

	manager.Shutdown()
	manager.Start()
	manager.mu.Lock()
	require.False(t, manager.started, "Start after Shutdown must not run")
	manager.mu.Unlock()
	_, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "missing"})
	require.True(t, errors.Is(err, ErrActressSyncManagerUnavailable), "CreateJob after Shutdown must 503-map: %v", err)

	// Plain Stop stays restartable (hot reload).
	_, _, _, manager2 := newFinalManagerFixture(t, &models.Actress{DMMID: 79})
	manager2.Stop()
	manager2.Start()
	manager2.Stop()
	require.NotPanics(t, func() { manager2.Shutdown() })
}
