package actressnamesync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourcesCancellationAfterAdmissionPreservesGenerationAndReadCapacity(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	generation := s.generation
	review := f.review(t, s)
	databaseBefore := reloadDatabaseState(t, f.db)
	want, err := s.SourcesAt(t.Context(), "", 0, 1, f.snapshot.Digest())
	require.NoError(t, err)
	require.NotNil(t, want.NextOffset, "fixture must have another source to paginate")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		page *SourcePage
		err  error
	}
	finished := make(chan result, 1)
	func() {
		// Publication holds this lock while replacing the pinned generation.
		// Keep it held until this request has acquired a real read slot, so
		// cancellation occurs after admission but before scanning any source.
		s.mu.Lock()
		defer s.mu.Unlock()
		go func() {
			page, err := s.SourcesAt(ctx, "", 0, 1, f.snapshot.Digest())
			finished <- result{page, err}
		}()
		awaitServiceSignal(t, s.readSlots)
		s.readSlots <- struct{}{}
		cancel()
	}()
	got := awaitServiceSignal(t, finished)
	require.ErrorIs(t, got.err, context.Canceled)
	require.Nil(t, got.page, "a cancelled search must not return a partial page")
	require.Empty(t, s.readSlots, "cancelled readers must release admission capacity")
	require.Same(t, generation, s.generation)
	require.Contains(t, s.tickets, review.ReviewID)
	require.Equal(t, databaseBefore, reloadDatabaseState(t, f.db))

	page, err := s.SourcesAt(t.Context(), "", 0, 1, f.snapshot.Digest())
	require.NoError(t, err)
	require.Equal(t, want, page, "the next reader must see the intact pinned generation")
	require.Empty(t, s.readSlots)
}
