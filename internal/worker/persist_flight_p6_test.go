package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJobPersistFlight_ErrorFansOutToPendingCallers(t *testing.T) {
	flight := newJobPersistFlight()
	entered := make(chan struct{})
	release := make(chan struct{})
	persistErr := errors.New("persist failed")
	persist := func() error {
		close(entered)
		<-release
		return persistErr
	}

	ownerDone := make(chan error, 1)
	go func() { ownerDone <- flight.do(context.Background(), persist) }()
	<-entered
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- flight.do(context.Background(), persist) }()
	require.Eventually(t, func() bool {
		flight.mu.Lock()
		defer flight.mu.Unlock()
		return len(flight.waiters) == 2
	}, time.Second, time.Millisecond)
	close(release)

	require.ErrorIs(t, <-ownerDone, persistErr)
	require.ErrorIs(t, <-waiterDone, persistErr)
}

func TestJobPersistFlight_ExclusiveFenceWaitsAndBlocksNewRequests(t *testing.T) {
	flight := newJobPersistFlight()
	entered := make(chan struct{})
	releasePersist := make(chan struct{})
	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- flight.do(context.Background(), func() error {
			close(entered)
			<-releasePersist
			return nil
		})
	}()
	<-entered

	exclusiveDone := make(chan struct {
		release func()
		err     error
	}, 1)
	go func() {
		release, err := flight.acquireExclusive(context.Background())
		exclusiveDone <- struct {
			release func()
			err     error
		}{release: release, err: err}
	}()
	require.Eventually(t, func() bool {
		flight.mu.Lock()
		defer flight.mu.Unlock()
		return flight.exclusive
	}, time.Second, time.Millisecond)
	newPersistDone := make(chan error, 1)
	go func() { newPersistDone <- flight.do(context.Background(), func() error { return nil }) }()
	select {
	case err := <-newPersistDone:
		t.Fatalf("new persistence must wait behind the exclusive fence, got %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releasePersist)
	require.NoError(t, <-ownerDone)
	got := <-exclusiveDone
	require.NoError(t, got.err)
	flight.sealExclusive(ErrJobGone)
	require.ErrorIs(t, <-newPersistDone, ErrJobGone)
}

func TestJobPersistFlight_SealedExclusiveReturnsGone(t *testing.T) {
	flight := newJobPersistFlight()
	_, err := flight.acquireExclusive(context.Background())
	require.NoError(t, err)
	flight.sealExclusive(ErrJobGone)

	_, err = flight.acquireExclusive(context.Background())
	require.ErrorIs(t, err, ErrJobGone)
}

func TestJobPersistFlight_CanceledWaiterDoesNotOwnFlight(t *testing.T) {
	flight := newJobPersistFlight()
	entered := make(chan struct{})
	release := make(chan struct{})
	persist := func() error {
		close(entered)
		<-release
		return nil
	}
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- flight.do(context.Background(), persist) }()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, flight.do(ctx, persist), context.Canceled)
	close(release)
	require.NoError(t, <-ownerDone)
}

// flightCancelContext lets tests deterministically cancel after a flight has
// entered a Done-channel select rather than before its initial Err check.
type flightCancelContext struct {
	done  chan struct{}
	ready chan struct{}
	once  sync.Once
}

func newFlightCancelContext() *flightCancelContext {
	return &flightCancelContext{done: make(chan struct{}), ready: make(chan struct{})}
}

func (c *flightCancelContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *flightCancelContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.ready) })
	return c.done
}
func (c *flightCancelContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}
func (c *flightCancelContext) Value(any) any { return nil }

func TestJobPersistFlight_ValidationAndExclusiveBranches(t *testing.T) {
	flight := newJobPersistFlight()
	require.NoError(t, flight.do(nil, func() error { return nil }))
	require.ErrorContains(t, flight.do(context.Background(), nil), "requires a callback")

	flight.mu.Lock()
	flight.exclusive = true
	flight.exclusiveDone = nil
	flight.exclusiveErr = nil
	flight.mu.Unlock()
	require.ErrorIs(t, flight.do(context.Background(), func() error { return nil }), ErrJobBusy)
	flight.mu.Lock()
	flight.exclusive = false
	flight.mu.Unlock()

	blocked := newJobPersistFlight()
	blocked.mu.Lock()
	blocked.exclusive = true
	blocked.exclusiveDone = make(chan struct{})
	blocked.mu.Unlock()
	blockedCtx := newFlightCancelContext()
	blockedDone := make(chan error, 1)
	go func() { blockedDone <- blocked.do(blockedCtx, func() error { return nil }) }()
	<-blockedCtx.ready
	close(blockedCtx.done)
	require.ErrorIs(t, <-blockedDone, context.Canceled)
	blocked.mu.Lock()
	blocked.exclusive = false
	blocked.exclusiveDone = nil
	blocked.mu.Unlock()

	release, err := flight.acquireExclusive(nil)
	require.NoError(t, err)
	release()
	flight.releaseExclusive() // no-op after the fence is already released

	ctx := context.WithValue(context.Background(), struct{}{}, "cancel")
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = flight.acquireExclusive(cancelCtx)
	require.ErrorIs(t, err, context.Canceled)

	idleFlight := newJobPersistFlight()
	idleFlight.mu.Lock()
	idleFlight.active = true
	idleFlight.idle = make(chan struct{})
	idleFlight.mu.Unlock()
	idleCtx := newFlightCancelContext()
	acquireDone := make(chan error, 1)
	go func() {
		_, acquireErr := idleFlight.acquireExclusive(idleCtx)
		acquireDone <- acquireErr
	}()
	<-idleCtx.ready
	close(idleCtx.done)
	require.ErrorIs(t, <-acquireDone, context.Canceled)
}

func TestJobPersistFlight_CanceledPendingWaiter(t *testing.T) {
	flight := newJobPersistFlight()
	entered := make(chan struct{})
	var enteredOnce sync.Once
	release := make(chan struct{})
	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- flight.do(context.Background(), func() error {
			enteredOnce.Do(func() { close(entered) })
			<-release
			return nil
		})
	}()
	<-entered

	waitCtx := newFlightCancelContext()
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- flight.do(waitCtx, func() error { return nil }) }()
	<-waitCtx.ready
	close(waitCtx.done)
	require.ErrorIs(t, <-waiterDone, context.Canceled)
	close(release)
	require.NoError(t, <-ownerDone)
}

func TestJobPersistFlight_OverlappingRequestsUseFreshFollowUp(t *testing.T) {
	flight := newJobPersistFlight()
	var stateMu sync.Mutex
	state := "before"
	var snapshots []string
	var persistCalls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})

	persist := func() error {
		call := persistCalls.Add(1)
		stateMu.Lock()
		snapshots = append(snapshots, state)
		stateMu.Unlock()
		if call == 1 {
			close(entered)
			<-release
		}
		return nil
	}

	ownerDone := make(chan error, 1)
	go func() { ownerDone <- flight.do(context.Background(), persist) }()
	<-entered

	stateMu.Lock()
	state = "after"
	stateMu.Unlock()
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- flight.do(context.Background(), persist) }()
	require.Eventually(t, func() bool {
		flight.mu.Lock()
		defer flight.mu.Unlock()
		return len(flight.waiters) == 2
	}, time.Second, time.Millisecond)

	close(release)
	require.NoError(t, <-ownerDone)
	require.NoError(t, <-waiterDone)
	require.Equal(t, int32(2), persistCalls.Load(), "the pending request needs one fresh follow-up")
	stateMu.Lock()
	defer stateMu.Unlock()
	require.Equal(t, []string{"before", "after"}, snapshots, "a stale pre-stall snapshot must never be the trailing write")
}
