package actressnamesync

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/stretchr/testify/require"
)

// nameSyncReadContext preserves a real context's values, deadlines and
// cancellation semantics. The test can pause an observation, cancel its real
// parent, then release the observation to return the parent's actual state.
// With no pause configured it only counts observations.
type nameSyncReadContext struct {
	context.Context
	errCalls    atomic.Int32
	doneCalls   atomic.Int32
	pauseErrAt  int32
	pauseDoneAt int32
	reached     chan struct{}
	release     chan struct{}
}

func (ctx *nameSyncReadContext) Err() error {
	if ctx.errCalls.Add(1) == ctx.pauseErrAt {
		close(ctx.reached)
		<-ctx.release
	}
	return ctx.Context.Err()
}

func (ctx *nameSyncReadContext) Done() <-chan struct{} {
	if ctx.doneCalls.Add(1) == ctx.pauseDoneAt {
		close(ctx.reached)
		<-ctx.release
	}
	return ctx.Context.Done()
}

func cancelNameSyncReadAt(t *testing.T, errAt, doneAt int32, call func(context.Context)) {
	t.Helper()
	parent, cancel := context.WithCancel(t.Context())
	ctx := &nameSyncReadContext{Context: parent, pauseErrAt: errAt, pauseDoneAt: doneAt, reached: make(chan struct{}), release: make(chan struct{})}
	finished := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(ctx.release) }) }
	go func() {
		defer close(finished)
		call(ctx)
	}()
	defer func() {
		cancel()
		release()
		awaitServiceSignal(t, finished)
	}()
	select {
	case <-ctx.reached:
	case <-finished:
		t.Fatal("read returned before reaching its cancellation boundary")
	case <-time.After(15 * time.Second):
		t.Fatal("read did not reach its cancellation boundary")
	}
	cancel()
	release()
	awaitServiceSignal(t, finished)
	require.ErrorIs(t, parent.Err(), context.Canceled)
}

func TestInfoCancellationDuringRequestLinkingSuppressesSourceState(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	pinned := s.generation
	review := f.review(t, s)
	ticketBefore := s.tickets[review.ReviewID]
	databaseBefore := reloadDatabaseState(t, f.db)
	want, err := s.Info(t.Context())
	require.NoError(t, err)
	require.True(t, want.Available)
	require.Equal(t, f.snapshot.Digest(), want.ArtifactDigest)

	var got SourceInfo
	var infoErr error
	// request checks Err before linking. Pause the first Done observation in
	// context.WithCancel so cancellation lands after that check, but before
	// Info's own final check. No background worker or timing race is needed.
	cancelNameSyncReadAt(t, 0, 1, func(ctx context.Context) { got, infoErr = s.Info(ctx) })
	require.ErrorIs(t, infoErr, context.Canceled)
	require.Equal(t, SourceInfo{}, got, "a cancelled read must not expose a digest or partial source state")
	require.Equal(t, want, s.info)
	require.Same(t, pinned, s.generation)
	require.Equal(t, ticketBefore, s.tickets[review.ReviewID])
	require.Equal(t, databaseBefore, reloadDatabaseState(t, f.db))

	retried, err := s.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, want, retried)
}

func TestGenerationCancellationDuringIndexingDiscardsPartialCandidate(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	pinned := s.generation
	review := f.review(t, s)
	ticketBefore := s.tickets[review.ReviewID]
	databaseBefore := reloadDatabaseState(t, f.db)
	before := f.snapshot.Bytes()

	// Measure the real native read/validation phase separately. This avoids
	// hard-coding its platform-specific number of context observations. The
	// unchanged fixture then lets us rendezvous at either index entry after
	// Load has successfully validated the complete artifact.
	store, err := artifactstore.Open(f.directory, offline.DefaultLimits().Store)
	require.NoError(t, err)
	readCtx := &nameSyncReadContext{Context: t.Context()}
	view, err := store.Load(readCtx)
	closeErr := store.Close()
	require.NoError(t, err)
	require.NoError(t, closeErr)
	require.False(t, view.Recovered)
	require.Equal(t, before, view.Snapshot.Bytes())
	readCalls := readCtx.errCalls.Load()
	require.Positive(t, readCalls)

	baselineCtx := &nameSyncReadContext{Context: t.Context()}
	want, err := s.loadGeneration(baselineCtx)
	require.NoError(t, err)
	recordCount := len(view.Snapshot.Records())
	require.GreaterOrEqual(t, recordCount, 2, "exercise both an empty and a partially built index")
	require.Equal(t, readCalls+int32(recordCount), baselineCtx.errCalls.Load(), "only the per-record index checks should follow the validated read")
	require.Len(t, want.sources, recordCount)
	require.NotNil(t, want.provider)

	for _, record := range []int{1, recordCount} {
		t.Run(fmt.Sprintf("before_record_%d", record), func(t *testing.T) {
			var got *generation
			var loadErr error
			cancelNameSyncReadAt(t, readCalls+int32(record), 0, func(ctx context.Context) { got, loadErr = s.loadGeneration(ctx) })
			require.ErrorIs(t, loadErr, context.Canceled)
			require.Nil(t, got, "a partial index must never become a publishable generation")
			require.Same(t, pinned, s.generation)
			require.Equal(t, ticketBefore, s.tickets[review.ReviewID])
			require.Equal(t, databaseBefore, reloadDatabaseState(t, f.db))

			// The cancelled candidate does not damage the artifact or the pinned
			// generation. A new read builds the same complete, usable candidate.
			retried, err := s.loadGeneration(t.Context())
			require.NoError(t, err)
			require.Equal(t, before, retried.snapshot.Bytes())
			require.Equal(t, want.sources, retried.sources)
			require.Equal(t, want.info, retried.info)
			require.Equal(t, want.options, retried.options)
			require.NotNil(t, retried.provider)
			page, err := s.SourcesAt(t.Context(), "", 0, 100, review.ArtifactDigest)
			require.NoError(t, err)
			require.Len(t, page.Items, recordCount)
			require.Equal(t, review.ArtifactDigest, page.ArtifactDigest)
		})
	}
}
