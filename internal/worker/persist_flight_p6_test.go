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
