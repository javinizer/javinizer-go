package actressnamesync

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestServiceConcurrentConfirmationIsDurablyIdempotent(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	review := f.review(t, s)
	type result struct {
		job *JobView
		err error
	}
	const contenders = 8
	start := make(chan struct{})
	results := make(chan result, contenders)
	var wg sync.WaitGroup
	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			job, err := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
			results <- result{job: job, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var id string
	for result := range results {
		require.NoError(t, result.err)
		if id == "" {
			id = result.job.ID
		}
		require.Equal(t, id, result.job.ID)
		require.Equal(t, "pending", result.job.Status)
	}
	requireServiceCounts(t, f.db, 1, 1)
	require.Empty(t, s.tickets)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
}

func TestServiceReviewNoncesAreBoundedExpiringAndPrincipalBound(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	var first *Review
	nonces := make(map[string]bool)
	for range maxReviews {
		review := f.review(t, s)
		if first == nil {
			first = review
		}
		require.True(t, validHex(review.ReviewID))
		require.False(t, nonces[review.ReviewID])
		nonces[review.ReviewID] = true
	}
	_, err := s.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
	require.ErrorIs(t, err, ErrReviewCapacity)
	require.Len(t, s.tickets, maxReviews)
	_, err = s.Confirm(t.Context(), "api-token:another-verified-owner", first.ReviewID, true)
	require.ErrorIs(t, err, ErrPrincipal)
	require.Len(t, s.tickets, maxReviews, "another principal cannot consume the owner's review")
	s.now = func() time.Time { return first.ExpiresAt }
	_, err = s.Confirm(t.Context(), fixturePrincipal, first.ReviewID, true)
	require.ErrorIs(t, err, ErrReviewExpired, "review TTL is exclusive at exact expiry")
	replacement := f.review(t, s)
	require.Equal(t, first.ExpiresAt.Add(reviewTTL), replacement.ExpiresAt)
	require.Len(t, s.tickets, 1, "expired tickets are pruned before new admission")
	requireServiceCounts(t, f.db, 0, 0)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
}

type blockedReviewRepository struct {
	Repository
	entered chan struct{}
	release chan struct{}
}

func (r *blockedReviewRepository) ReviewOfflineNameSync(ctx context.Context, choices []database.ActressSyncChoice, snapshot *actresscache.Snapshot, options actresssync.Options) (*database.ActressSyncReview, error) {
	r.entered <- struct{}{}
	select {
	case <-r.release:
		return r.Repository.ReviewOfflineNameSync(ctx, choices, snapshot, options)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestServiceConcurrentReviewWorkIsBounded(t *testing.T) {
	f := newServiceFixture(t)
	repo := &blockedReviewRepository{Repository: f.repo, entered: make(chan struct{}, 4), release: make(chan struct{})}
	s, cancel := f.service(t, repo)
	s.initialize()
	const concurrent = 4
	results := make(chan error, concurrent)
	var wg sync.WaitGroup
	for range concurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
			results <- err
		}()
	}
	// If an assertion fails, cancel linked requests and join before DB cleanup.
	t.Cleanup(func() { cancel(); wg.Wait() })
	for range concurrent {
		awaitServiceSignal(t, repo.entered)
	}
	_, err := s.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
	require.ErrorIs(t, err, ErrReviewCapacity)
	close(repo.release)
	wg.Wait()
	for range concurrent {
		require.NoError(t, awaitServiceSignal(t, results))
	}
	require.Len(t, s.tickets, concurrent)
	requireServiceCounts(t, f.db, 0, 0)
}

func TestServiceQueueCapacitySurvivesRestartAndFailedConfirmRemainsRetryable(t *testing.T) {
	f := newServiceFixture(t)
	options := Options{Enabled: true, ArtifactDirectory: f.directory, MaxAge: time.Hour, QueueLimits: database.ActressSyncQueueLimits{MaxQueuedJobs: 1, MaxQueuedTasks: 1}}
	s, _ := f.service(t, f.repo, options)
	s.initialize()
	_, first := f.confirm(t, s)
	f.reopen(t)
	restarted, _ := f.service(t, f.repo, options)
	restarted.initialize()
	other := models.Actress{DMMID: 8, FirstName: "Other", LastName: "Actor", JapaneseName: "別旧名"}
	require.NoError(t, f.repo.ImportUpsert(t.Context(), &other))
	otherKey, err := actresscache.SourceKey("r18dev", "r18-only-901")
	require.NoError(t, err)
	review, err := restarted.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: other.ID, SourceKey: otherKey}})
	require.NoError(t, err)
	_, err = restarted.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.ErrorIs(t, err, database.ErrActressSyncQueueFull)
	require.Contains(t, restarted.tickets, review.ReviewID, "failed admission must not destroy valid confirmation authority")
	requireServiceCounts(t, f.db, 1, 1)
	_, err = restarted.Cancel(t.Context(), first.ID)
	require.NoError(t, err)
	second, err := restarted.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, "pending", second.Status)
	requireServiceCounts(t, f.db, 2, 2)
	restarted.workPass(t.Context(), "", nil)
	finished, err := restarted.Job(t.Context(), second.ID)
	require.NoError(t, err)
	require.Equal(t, 1, finished.Counts["applied"])
	original, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "旧名", original.JapaneseName, "cancelled work must not consume later capacity by applying")
}

func TestServiceSourcePagesAndAdmissionValidation(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	first, err := s.Sources(t.Context(), "", 0, 1)
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	require.NotNil(t, first.NextOffset)
	second, err := s.Sources(t.Context(), "", *first.NextOffset, 1)
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	require.Nil(t, second.NextOffset)
	require.NotEqual(t, first.Items[0].SourceKey, second.Items[0].SourceKey)
	first.Items[0].JapaneseName = "client mutation"
	again, err := s.Sources(t.Context(), "r18-only-900", 0, 1)
	require.NoError(t, err)
	require.Equal(t, "新名", again.Items[0].JapaneseName)
	for _, query := range []string{strings.Repeat("x", 129), string([]byte{0xff})} {
		_, err := s.Sources(t.Context(), query, 0, 1)
		require.ErrorIs(t, err, ErrInvalid)
	}
	for _, bounds := range [][2]int{{-1, 1}, {3, 1}, {0, 0}, {0, 101}} {
		_, err := s.Sources(t.Context(), "", bounds[0], bounds[1])
		require.ErrorIs(t, err, ErrInvalid)
	}
	for _, principal := range []string{"", "not\na principal", strings.Repeat("x", 257)} {
		_, err := s.Review(t.Context(), principal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
		require.ErrorIs(t, err, ErrInvalid)
	}
	for _, choices := range [][]database.ActressSyncChoice{
		nil,
		{{ActressID: 0, SourceKey: f.key}},
		{{ActressID: f.actor.ID, SourceKey: ""}},
		{{ActressID: f.actor.ID, SourceKey: f.key}, {ActressID: f.actor.ID, SourceKey: f.key}},
		make([]database.ActressSyncChoice, actresssync.MaxSelection+1),
	} {
		_, err := s.Review(t.Context(), fixturePrincipal, choices)
		require.ErrorIs(t, err, ErrInvalid)
	}
	for _, nonce := range []string{"", "../../current.json", strings.Repeat("A", 64), strings.Repeat("a", 65)} {
		_, err := s.Confirm(t.Context(), fixturePrincipal, nonce, true)
		require.ErrorIs(t, err, ErrInvalid)
	}
	requireServiceCounts(t, f.db, 0, 0)
}
