package actressnamesync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

// Both first lookups finish before either confirmation can acquire admission.
// This forces the durable second lookup rather than relying on scheduling luck.
type confirmationBarrierRepository struct {
	Repository
	calls         atomic.Int32
	entered       chan struct{}
	release       chan struct{}
	lookupErrorAt int32
	failure       error
}

func (r *confirmationBarrierRepository) OfflineNameSyncJobByRequestKey(ctx context.Context, key string) (*database.ActressSyncJob, error) {
	n := r.calls.Add(1)
	if n == r.lookupErrorAt {
		return nil, r.failure
	}
	if n <= 2 && r.entered != nil {
		job, err := r.Repository.OfflineNameSyncJobByRequestKey(ctx, key)
		r.entered <- struct{}{}
		// Return the already-observed miss, even if cancellation arrived while
		// this test barrier held it. The service must recheck its own admission.
		<-r.release
		return job, err
	}
	return r.Repository.OfflineNameSyncJobByRequestKey(ctx, key)
}
func TestConfirmationSecondLookupReturnsSameDurableReceipt(t *testing.T) {
	f := newServiceFixture(t)
	repo := &confirmationBarrierRepository{Repository: f.repo, entered: make(chan struct{}, 2), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(repo.release) }) }
	t.Cleanup(release)
	s, cancel := f.service(t, repo)
	s.initialize()
	review := f.review(t, s)
	type result struct {
		job *JobView
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
			results <- result{j, e}
		}()
	}
	t.Cleanup(func() { cancel(); release(); wg.Wait() })
	awaitServiceSignal(t, repo.entered)
	awaitServiceSignal(t, repo.entered)
	release()
	a, b := awaitServiceSignal(t, results), awaitServiceSignal(t, results)
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	require.Equal(t, a.job, b.job)
	require.EqualValues(t, 4, repo.calls.Load())
	requireServiceCounts(t, f.db, 1, 1)
	require.Empty(t, s.tickets)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
}

func TestConfirmationLookupFailuresPreserveReview(t *testing.T) {
	for _, at := range []int32{1, 2} {
		t.Run(string(rune('0'+at)), func(t *testing.T) {
			f := newServiceFixture(t)
			failure := errors.New("lookup unavailable")
			repo := &confirmationBarrierRepository{Repository: f.repo, lookupErrorAt: at, failure: failure}
			s, _ := f.service(t, repo)
			s.initialize()
			review := f.review(t, s)
			j, err := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
			require.Nil(t, j)
			require.ErrorIs(t, err, failure)
			require.Contains(t, s.tickets, review.ReviewID)
			requireServiceCounts(t, f.db, 0, 0)
		})
	}
}
func TestConfirmationCancellationWhileAdmissionOccupied(t *testing.T) {
	f := newServiceFixture(t)
	repo := &confirmationBarrierRepository{Repository: f.repo, entered: make(chan struct{}, 2), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(repo.release) }) }
	t.Cleanup(release)
	s, _ := f.service(t, repo)
	s.initialize()
	review := f.review(t, s)
	s.confirm <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := s.Confirm(ctx, fixturePrincipal, review.ReviewID, true)
		result <- err
	}()
	t.Cleanup(func() { cancel(); release(); wg.Wait() })
	awaitServiceSignal(t, repo.entered)
	cancel()
	release()
	require.ErrorIs(t, awaitServiceSignal(t, result), context.Canceled)
	<-s.confirm
	require.Contains(t, s.tickets, review.ReviewID)
	requireServiceCounts(t, f.db, 0, 0)
}

func TestConfirmationSaturatedWakeStillCommits(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	s.wake <- struct{}{}
	_, job := f.confirm(t, s)
	require.Len(t, s.wake, 1)
	requireServiceCounts(t, f.db, 1, 1)
	durable, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", durable.Tasks[0].Status)
}

type reviewBoundaryRepository struct {
	Repository
	transform func(*database.ActressSyncReview)
	failure   error
}

func (r *reviewBoundaryRepository) ReviewOfflineNameSync(ctx context.Context, c []database.ActressSyncChoice, s *actresscache.Snapshot, o actresssync.Options) (*database.ActressSyncReview, error) {
	if r.failure != nil {
		return nil, r.failure
	}
	review, err := r.Repository.ReviewOfflineNameSync(ctx, c, s, o)
	if err == nil && r.transform != nil {
		r.transform(review)
	}
	return review, err
}
func TestReviewFailureBoundariesNeverRetainAuthority(t *testing.T) {
	for _, kind := range []string{"repository", "missing_source", "before_budget", "after_budget", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := newServiceFixture(t)
			repo := &reviewBoundaryRepository{Repository: f.repo}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := ErrInvalid
			switch kind {
			case "repository":
				repo.failure = errors.New("review unavailable")
				want = repo.failure
			case "missing_source":
				repo.transform = func(r *database.ActressSyncReview) { r.Proposals[0].SourceKey = "missing" }
			case "before_budget":
				repo.transform = func(r *database.ActressSyncReview) { r.Proposals[0].BeforeName = strings.Repeat("x", 4097) }
			case "after_budget":
				repo.transform = func(r *database.ActressSyncReview) { r.Proposals[0].AfterName = strings.Repeat("x", 4097) }
			case "cancelled":
				repo.transform = func(*database.ActressSyncReview) { cancel() }
				want = context.Canceled
			}
			s, _ := f.service(t, repo)
			s.initialize()
			review, err := s.Review(ctx, fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
			require.Nil(t, review)
			require.ErrorIs(t, err, want)
			require.Empty(t, s.tickets)
			require.Empty(t, s.reviewSlots)
			requireServiceCounts(t, f.db, 0, 0)
		})
	}
}

type claimFailureRepository struct {
	Repository
	failure error
	calls   int
}

func (r *claimFailureRepository) ClaimOfflineNameSync(context.Context, string, string, time.Time, time.Duration) (*database.ActressSyncLease, error) {
	r.calls++
	return nil, r.failure
}
func TestWorkerClaimFailureDoesNotApplyOrRecordFalseProblem(t *testing.T) {
	f := newServiceFixture(t)
	repo := &claimFailureRepository{Repository: f.repo, failure: errors.New("lease raced")}
	s, _ := f.service(t, repo)
	s.initialize()
	_, job := f.confirm(t, s)
	cursor, historical := s.workPass(t.Context(), "", nil)
	require.Equal(t, job.ID, cursor)
	require.Nil(t, historical)
	require.Equal(t, 1, repo.calls)
	durable, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", durable.Tasks[0].Status)
	require.Empty(t, durable.Tasks[0].ProblemCode)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
}
func TestWorkerHistoricalCacheAndUnavailableStore(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	historical := f.publish(t, "別世代")
	got, err := s.snapshotForJob(t.Context(), historical.Digest(), historical)
	require.NoError(t, err)
	require.Same(t, historical, got)
	s.opts.ArtifactDirectory = "relative/path"
	got, err = s.snapshotForJob(t.Context(), strings.Repeat("a", 64), nil)
	require.EqualError(t, err, "invalid root or limits")
	require.Nil(t, got)
}

func TestConfirmationUnavailableSnapshotKeepsAuthorityForRetry(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	review := f.review(t, s)
	generation := s.generation
	s.generation = nil
	job, err := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.ErrorIs(t, err, ErrUnavailable)
	require.Nil(t, job)
	require.Contains(t, s.tickets, review.ReviewID)
	requireServiceCounts(t, f.db, 0, 0)
	s.generation = generation
	job, err = s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.NoError(t, err)
	require.NotNil(t, job)
	requireServiceCounts(t, f.db, 1, 1)
}

type cancellingDiscoveryRepository struct {
	Repository
	cancel context.CancelFunc
}

func (r *cancellingDiscoveryRepository) DueOfflineNameSyncJobs(ctx context.Context, now time.Time, cursor string, limit int) ([]database.ActressSyncJob, error) {
	jobs, err := r.Repository.DueOfflineNameSyncJobs(ctx, now, cursor, limit)
	r.cancel()
	return jobs, err
}
func TestWorkerCancellationAfterDiscoveryLeavesQueueUntouched(t *testing.T) {
	f := newServiceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	repo := &cancellingDiscoveryRepository{Repository: f.repo, cancel: cancel}
	s, _ := f.service(t, repo)
	s.initialize()
	_, job := f.confirm(t, s)
	cursor, historical := s.workPass(ctx, "", nil)
	require.Empty(t, cursor)
	require.Nil(t, historical)
	durable, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", durable.Tasks[0].Status)
	require.Zero(t, durable.Tasks[0].LeaseGeneration)
	require.Empty(t, durable.Tasks[0].ProblemCode)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
}
func TestWorkerAlreadyCancelledLifetimeStillSignalsReadyAndExits(t *testing.T) {
	f := newServiceFixture(t)
	s, cancel := f.service(t, f.repo)
	cancel()
	done := make(chan struct{})
	go func() { s.Run(); close(done) }()
	awaitServiceSignal(t, s.Ready())
	awaitServiceSignal(t, done)
	require.Nil(t, s.generation)
	requireServiceCounts(t, f.db, 0, 0)
}

// A receipt lookup may fail after discovery or after cancellation commits. The
// service must return the failure, not a fabricated or partial success view.
type receiptFailureRepository struct {
	Repository
	failure error
}

func (r *receiptFailureRepository) OfflineNameSyncJob(context.Context, string) (*database.ActressSyncJob, error) {
	return nil, r.failure
}
func TestReceiptReadFailuresDoNotFabricateHistoryOrUndoCancellation(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	_, job := f.confirm(t, s)
	failure := errors.New("receipt unavailable")
	s.repo = &receiptFailureRepository{Repository: f.repo, failure: failure}
	history, err := s.ListJobs(t.Context(), "", 10)
	require.ErrorIs(t, err, failure)
	require.Nil(t, history)
	before, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.False(t, before.Cancelled)
	require.Equal(t, "pending", before.Tasks[0].Status)
	cancelled, err := s.Cancel(t.Context(), job.ID)
	require.ErrorIs(t, err, failure)
	require.Nil(t, cancelled)
	after, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.True(t, after.Cancelled)
	require.Equal(t, "cancelled", after.Tasks[0].Status)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
	requireServiceCounts(t, f.db, 1, 1)
	// Retrying a committed cancellation after the read recovers is idempotent.
	s.repo = f.repo
	receipt, err := s.Cancel(t.Context(), job.ID)
	require.NoError(t, err)
	require.True(t, receipt.Cancelled)
	require.Equal(t, 1, receipt.Counts["cancelled"])
}
