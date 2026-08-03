package commandutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type fakeDumpStore struct {
	release    chan struct{}
	movie      *models.DumpMovie
	mu         sync.Mutex
	closed     bool
	closeCalls int
}

func (f *fakeDumpStore) LookupByDVDID(context.Context, string) (string, error) {
	return "", models.ErrDumpMiss
}

func (f *fakeDumpStore) LookupByContentID(context.Context, string) (string, error) {
	return "", models.ErrDumpMiss
}

func (f *fakeDumpStore) LookupMovie(_ context.Context, _ string) (*models.DumpMovie, error) {
	if f.release != nil {
		<-f.release
	}
	return f.movie, nil
}

func (f *fakeDumpStore) Stats(context.Context) (models.DumpStats, error) {
	return models.DumpStats{}, nil
}

func (f *fakeDumpStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	f.closeCalls++
	return nil
}

func (f *fakeDumpStore) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// Close must not release the SQLite handle while a query is still in flight;
// the handle closes when the last lookup drains. Late queries degrade to a
// miss so callers fall back to HTTP.
func TestRefCountedDumpLookupDrainsInFlightQueries(t *testing.T) {
	release := make(chan struct{})
	store := &fakeDumpStore{release: release, movie: &models.DumpMovie{}}
	dump := &refCountedDumpLookup{inner: store, closer: store, drainedCh: make(chan struct{})}

	done := make(chan error, 1)
	go func() {
		_, err := dump.LookupMovie(context.Background(), "IPX-535")
		done <- err
	}()
	// Wait until the query has actually acquired the slot.
	for i := 0; i < 200; i++ {
		dump.mu.Lock()
		inFlight := dump.active
		dump.mu.Unlock()
		if inFlight == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- dump.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before drain: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	require.False(t, store.isClosed(), "Close must wait for the in-flight query")

	close(release)
	require.NoError(t, <-done)
	require.NoError(t, <-closeDone)
	require.True(t, store.isClosed(), "the drain should close the store")

	_, err := dump.LookupByDVDID(context.Background(), "IPX-535")
	require.ErrorIs(t, err, models.ErrDumpMiss)
}

func TestRefCountedDumpLookupIdempotentCloseAndMissesAfterClose(t *testing.T) {
	store := &fakeDumpStore{}
	dump := &refCountedDumpLookup{inner: store, closer: store, drainedCh: make(chan struct{})}

	// Cover the remaining wrappers before close.
	_, err := dump.LookupByContentID(context.Background(), "118ipx00535")
	require.ErrorIs(t, err, models.ErrDumpMiss)
	_, sErr := dump.Stats(context.Background())
	require.NoError(t, sErr)

	require.NoError(t, dump.Close())
	require.True(t, store.isClosed())
	// Second Close is a no-op.
	require.NoError(t, dump.Close())
	require.Equal(t, 1, store.closeCalls)
}

// Close times out past drain when a lookup wedges (exercises the timeout arm).
func TestRefCountedDumpLookupCloseTimesOutOnStuckLookup(t *testing.T) {
	store := &fakeDumpStore{release: make(chan struct{})} // never released
	dump := &refCountedDumpLookup{inner: store, closer: store, drainedCh: make(chan struct{})}

	// Park a lookup permanently by latching the acquire manually.
	dump.mu.Lock()
	dump.active++
	dump.mu.Unlock()
	prevWait := dumpDrainWait
	dumpDrainWait = 30 * time.Millisecond
	t.Cleanup(func() { dumpDrainWait = prevWait })
	start := time.Now()
	require.NoError(t, dump.Close())
	require.GreaterOrEqual(t, time.Since(start), 25*time.Millisecond)
	require.True(t, store.isClosed())

	// late lookups miss cleanly
	_, err := dump.LookupMovie(context.Background(), "IPX-535")
	require.ErrorIs(t, err, models.ErrDumpMiss)
}

// After Close every wrapper must degrade to a clean dump miss so callers
// fall back to HTTP even mid-reload.
func TestRefCountedDumpLookupPostCloseWrappersMiss(t *testing.T) {
	store := &fakeDumpStore{}
	dump := &refCountedDumpLookup{inner: store, closer: store, drainedCh: make(chan struct{})}
	require.NoError(t, dump.Close())

	if _, err := dump.LookupByDVDID(context.Background(), "IPX-535"); err == nil {
		t.Fatal("expected miss after close")
	} else {
		require.ErrorIs(t, err, models.ErrDumpMiss)
	}
	if _, err := dump.LookupByContentID(context.Background(), "118ipx00535"); err == nil {
		t.Fatal("expected miss after close")
	} else {
		require.ErrorIs(t, err, models.ErrDumpMiss)
	}
	if _, err := dump.Stats(context.Background()); err == nil {
		t.Fatal("expected miss after close")
	} else {
		require.ErrorIs(t, err, models.ErrDumpMiss)
	}
}

func TestRefCountedDumpLookupClosesWhenIdle(t *testing.T) {
	store := &fakeDumpStore{}
	dump := &refCountedDumpLookup{inner: store, closer: store, drainedCh: make(chan struct{})}
	require.NoError(t, dump.Close())
	require.True(t, store.isClosed())
	require.Equal(t, 1, store.closeCalls)
}
