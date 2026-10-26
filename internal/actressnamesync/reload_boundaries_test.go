package actressnamesync

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/javinizer/javinizer-go/internal/actressmetadata"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

func TestReloadDisabledHasNoArtifactOrConsentSideEffects(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo, Options{})
	s.initialize()
	before := reloadDatabaseState(t, f.db)
	_, err := s.Reload(t.Context())
	require.ErrorIs(t, err, ErrDisabled)
	require.Nil(t, s.generation)
	require.Empty(t, s.tickets)
	require.Empty(t, s.reload)
	require.Equal(t, before, reloadDatabaseState(t, f.db))
}

func TestReloadCancellationWhileWaitingForStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newServiceFixture(t)
		s, _ := f.service(t, f.repo)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		result := make(chan error, 1)
		go func() { _, err := s.Reload(ctx); result <- err }()
		// The only reload goroutine is now durably blocked on initial readiness.
		synctest.Wait()
		require.Len(t, s.reload, 1)
		cancel()
		require.ErrorIs(t, awaitServiceSignal(t, result), context.Canceled)
		require.Empty(t, s.reload)
		require.Nil(t, s.generation)
		require.Equal(t, "reload_cancelled", s.info.ReloadProblemCode)
		requireServiceCounts(t, f.db, 0, 0)
	})
}

func TestReloadCancellationWhileWaitingForConfirmation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newServiceFixture(t)
		s, _ := f.service(t, f.repo)
		s.initialize()
		old := s.generation
		review := f.review(t, s)
		f.publish(t, "更新名")
		s.confirm <- struct{}{}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		result := make(chan error, 1)
		go func() { _, err := s.Reload(ctx); result <- err }()
		// Artifact I/O has completed; final confirmation admission is occupied.
		synctest.Wait()
		cancel()
		require.ErrorIs(t, awaitServiceSignal(t, result), context.Canceled)
		<-s.confirm
		require.Same(t, old, s.generation)
		require.Contains(t, s.tickets, review.ReviewID)
		require.Empty(t, s.reload)
		require.Equal(t, "reload_cancelled", s.info.ReloadProblemCode)
		requireServiceCounts(t, f.db, 0, 0)
	})
}

func TestReloadCancellationAfterConfirmationAdmissionBeforePublication(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	old := s.generation
	review := f.review(t, s)
	f.publish(t, "更新名")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		go func() { _, err := s.Reload(ctx); result <- err }()
		// Receiving the token proves candidate validation and confirmation admission
		// both completed. The held publication lock prevents the generation swap.
		awaitServiceSignal(t, s.confirm)
		s.confirm <- struct{}{}
		cancel()
	}()
	require.ErrorIs(t, awaitServiceSignal(t, result), context.Canceled)
	require.Same(t, old, s.generation)
	require.Contains(t, s.tickets, review.ReviewID)
	require.Empty(t, s.reload)
	require.Empty(t, s.confirm)
	require.Equal(t, "reload_cancelled", s.info.ReloadProblemCode)
	requireServiceCounts(t, f.db, 0, 0)
}

func TestReloadReviewRejectsMissingSourceAndInconsistentBundle(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	_, err := s.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: "missing"}})
	require.ErrorIs(t, err, ErrInvalid)
	// Deliberately malformed internal bundle: the provider must not authorize
	// names from a different snapshot even if the exact selected source exists.
	next := f.publish(t, "更新名")
	provider, err := actressmetadata.NewSnapshotProvider(next, "r18dev")
	require.NoError(t, err)
	malformed := *s.generation
	malformed.provider = provider
	s.generation = &malformed
	_, err = s.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Empty(t, s.tickets)
	requireServiceCounts(t, f.db, 0, 0)
}

func TestReloadReviewCancellationAfterAdmissionReleasesAuthority(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		go func() {
			_, err := s.Review(ctx, fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
			result <- err
		}()
		// Pin the exact boundary after request admission, before generation read.
		awaitServiceSignal(t, s.reviewSlots)
		s.reviewSlots <- struct{}{}
		cancel()
	}()
	require.ErrorIs(t, awaitServiceSignal(t, result), context.Canceled)
	require.Empty(t, s.reviewSlots)
	require.Empty(t, s.tickets)
	requireServiceCounts(t, f.db, 0, 0)
}

func TestReloadConfirmationRejectsStaleHeldTicket(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	review := f.review(t, s)
	f.publish(t, "更新名")
	next, err := s.loadGeneration(t.Context())
	require.NoError(t, err)
	// Exercise the final digest fence independently of ticket invalidation:
	// even a mistakenly retained old ticket cannot authorize changed evidence.
	s.generation = next
	_, err = s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.ErrorIs(t, err, ErrStaleGeneration)
	require.Empty(t, s.confirm)
	requireServiceCounts(t, f.db, 0, 0)
}
