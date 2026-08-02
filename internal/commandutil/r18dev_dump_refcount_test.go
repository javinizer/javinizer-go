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
	dump := &refCountedDumpLookup{inner: store, closer: store}

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

	require.NoError(t, dump.Close())
	require.False(t, store.isClosed(), "Close must wait for the in-flight query")

	close(release)
	require.NoError(t, <-done)
	for i := 0; i < 200 && !store.isClosed(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, store.isClosed(), "the drain should close the store")

	_, err := dump.LookupByDVDID(context.Background(), "IPX-535")
	require.ErrorIs(t, err, models.ErrDumpMiss)
}

func TestRefCountedDumpLookupClosesWhenIdle(t *testing.T) {
	store := &fakeDumpStore{}
	dump := &refCountedDumpLookup{inner: store, closer: store}
	require.NoError(t, dump.Close())
	require.True(t, store.isClosed())
	require.Equal(t, 1, store.closeCalls)
}
