package actressnamesync

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

func TestReloadChangesGenerationRequiresNewReview(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	before, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	databaseBefore := reloadDatabaseState(t, f.db)
	old := f.review(t, s)
	page, err := s.SourcesAt(t.Context(), "", 0, 1, old.ArtifactDigest)
	require.NoError(t, err)
	next := f.publish(t, "更新名")
	info, err := s.Reload(t.Context())
	require.NoError(t, err)
	require.Equal(t, next.Digest(), info.ArtifactDigest)
	require.Equal(t, s.opts, s.generation.options)
	require.True(t, info.RestartRequiredForConfigChanges)
	_, err = s.Confirm(t.Context(), fixturePrincipal, old.ReviewID, true)
	require.ErrorIs(t, err, ErrReviewExpired)
	_, err = s.SourcesAt(t.Context(), "", *page.NextOffset, 1, old.ArtifactDigest)
	require.ErrorIs(t, err, ErrStaleGeneration)
	_, err = s.ReviewAt(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}}, old.ArtifactDigest)
	require.ErrorIs(t, err, ErrStaleGeneration)
	fresh := f.review(t, s)
	require.Equal(t, next.Digest(), fresh.ArtifactDigest)
	require.Equal(t, "更新名", fresh.Proposals[0].AfterName)
	require.Equal(t, f.now, fresh.Proposals[0].Source.ObservedAt)
	after, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "reload/review must not mutate actor fields")
	require.Equal(t, databaseBefore, reloadDatabaseState(t, f.db), "reload/review must preserve all canonical, credit, alias, ownership, render and job state")
	requireServiceCounts(t, f.db, 0, 0)
	job, err := s.Confirm(t.Context(), fixturePrincipal, fresh.ReviewID, true)
	require.NoError(t, err)
	require.Equal(t, next.Digest(), job.SnapshotDigest)
}

func TestReloadSameDigestPreservesReviewAndGeneration(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	old := s.generation
	review := f.review(t, s)
	for range 12 {
		info, err := s.Reload(t.Context())
		require.NoError(t, err)
		require.Equal(t, f.snapshot.Digest(), info.ArtifactDigest)
		require.Same(t, old, s.generation)
		require.Equal(t, s.opts, s.generation.options)
		require.Equal(t, int64(s.generation.options.MaxAge.Seconds()), info.MaxAgeSeconds)
		require.Len(t, s.tickets, 1)
		require.Empty(t, s.reload)
	}
	_, err := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.NoError(t, err)
}

func TestReloadLostResponseReturnsOriginalReceiptAndWorkerUsesOldArtifact(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	review, job := f.confirm(t, s)
	next := f.publish(t, "更新名")
	_, err := s.Reload(t.Context())
	require.NoError(t, err)
	replay, err := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.NoError(t, err)
	require.Equal(t, job, replay)
	require.NotEqual(t, next.Digest(), replay.SnapshotDigest)
	s.workPass(t.Context(), "", nil)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "新名", actor.JapaneseName)
	requireServiceCounts(t, f.db, 1, 1)
}

func TestReloadOldJobNeverSubstitutesNewArtifact(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "corrupt"}[corrupt], func(t *testing.T) {
			f := newServiceFixture(t)
			s, _ := f.service(t, f.repo)
			s.initialize()
			_, job := f.confirm(t, s)
			f.publish(t, "更新名")
			_, err := s.Reload(t.Context())
			require.NoError(t, err)
			path := filepath.Join(f.directory, f.snapshot.Digest()+".ndjson")
			if corrupt {
				require.NoError(t, os.WriteFile(path, []byte("corrupt"), 0600))
			} else {
				require.NoError(t, os.Remove(path))
			}
			s.workPass(t.Context(), "", nil)
			actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
			require.NoError(t, err)
			require.Equal(t, "旧名", actor.JapaneseName)
			receipt, err := s.Job(t.Context(), job.ID)
			require.NoError(t, err)
			require.Zero(t, receipt.Counts["applied"])
		})
	}
}

func TestReloadFailurePreservesUsableGenerationAndTickets(t *testing.T) {
	for _, mode := range []string{"corrupt", "recovery", "oversized", "cancelled", "busy"} {
		t.Run(mode, func(t *testing.T) {
			f := newServiceFixture(t)
			s, _ := f.service(t, f.repo)
			s.initialize()
			old := s.generation
			review := f.review(t, s)
			ctx := t.Context()
			switch mode {
			case "corrupt":
				require.NoError(t, os.WriteFile(filepath.Join(f.directory, "current.json"), []byte("broken"), 0600))
			case "recovery":
				f.publish(t, "更新名")
				require.NoError(t, os.WriteFile(filepath.Join(f.directory, "current.json"), []byte("broken"), 0600))
			case "oversized":
				file, err := os.OpenFile(filepath.Join(f.directory, "current.json"), os.O_WRONLY|os.O_TRUNC, 0600)
				require.NoError(t, err)
				require.NoError(t, file.Truncate(16<<20))
				require.NoError(t, file.Close())
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "busy":
				s.reload <- struct{}{}
				defer func() { <-s.reload }()
			}
			_, err := s.Reload(ctx)
			require.Error(t, err)
			require.Same(t, old, s.generation)
			require.Contains(t, s.tickets, review.ReviewID)
			info, err := s.Info(t.Context())
			require.NoError(t, err)
			require.True(t, info.Available)
			require.Equal(t, f.snapshot.Digest(), info.ArtifactDigest)
			if mode == "corrupt" || mode == "recovery" || mode == "oversized" {
				require.Equal(t, "reload_failed", info.ReloadProblemCode)
			}
			page, err := s.Sources(t.Context(), "新名", 0, 100)
			require.NoError(t, err)
			require.Equal(t, f.snapshot.Digest(), page.ArtifactDigest)
			requireServiceCounts(t, f.db, 0, 0)
		})
	}
}

type reloadSubmitBarrier struct {
	Repository
	entered, release chan struct{}
}

func (r *reloadSubmitBarrier) SubmitConfirmedOfflineNameSyncWithLimits(ctx context.Context, key string, review database.ActressSyncReview, snapshot *actresscache.Snapshot, now time.Time, limits database.ActressSyncQueueLimits) (*database.ActressSyncJob, error) {
	close(r.entered)
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.Repository.SubmitConfirmedOfflineNameSyncWithLimits(ctx, key, review, snapshot, now, limits)
}

type reloadLookupBarrier struct {
	Repository
	once             sync.Once
	entered, release chan struct{}
}

func (r *reloadLookupBarrier) OfflineNameSyncJobByRequestKey(ctx context.Context, key string) (*database.ActressSyncJob, error) {
	r.once.Do(func() {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
		}
	})
	return r.Repository.OfflineNameSyncJobByRequestKey(ctx, key)
}
func TestReloadConfirmationRaceOrders(t *testing.T) {
	t.Run("confirmation admitted first pins A", func(t *testing.T) {
		f := newServiceFixture(t)
		repo := &reloadSubmitBarrier{Repository: f.repo, entered: make(chan struct{}), release: make(chan struct{})}
		s, cancel := f.service(t, repo)
		s.initialize()
		review := f.review(t, s)
		type result struct {
			job *JobView
			err error
		}
		confirmed := make(chan result, 1)
		go func() {
			j, e := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
			confirmed <- result{j, e}
		}()
		t.Cleanup(cancel)
		awaitServiceSignal(t, repo.entered)
		next := f.publish(t, "更新名")
		reloaded := make(chan error, 1)
		go func() { _, err := s.Reload(t.Context()); reloaded <- err }()
		close(repo.release)
		got := awaitServiceSignal(t, confirmed)
		require.NoError(t, got.err)
		require.Equal(t, f.snapshot.Digest(), got.job.SnapshotDigest)
		require.NoError(t, awaitServiceSignal(t, reloaded))
		require.Equal(t, next.Digest(), s.generation.snapshot.Digest())
	})
	t.Run("reload publishes before confirmation admission", func(t *testing.T) {
		f := newServiceFixture(t)
		repo := &reloadLookupBarrier{Repository: f.repo, entered: make(chan struct{}), release: make(chan struct{})}
		s, cancel := f.service(t, repo)
		s.initialize()
		review := f.review(t, s)
		confirmed := make(chan error, 1)
		go func() { _, e := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true); confirmed <- e }()
		t.Cleanup(cancel)
		awaitServiceSignal(t, repo.entered)
		f.publish(t, "更新名")
		_, err := s.Reload(t.Context())
		require.NoError(t, err)
		close(repo.release)
		require.ErrorIs(t, awaitServiceSignal(t, confirmed), ErrReviewExpired)
		requireServiceCounts(t, f.db, 0, 0)
	})
}

func TestReloadConcurrentReviewCannotResurrectOldTicket(t *testing.T) {
	f := newServiceFixture(t)
	repo := &blockedReviewRepository{Repository: f.repo, entered: make(chan struct{}, 1), release: make(chan struct{})}
	s, cancel := f.service(t, repo)
	s.initialize()
	result := make(chan error, 1)
	go func() {
		_, err := s.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
		result <- err
	}()
	t.Cleanup(cancel)
	awaitServiceSignal(t, repo.entered)
	f.publish(t, "更新名")
	_, err := s.Reload(t.Context())
	require.NoError(t, err)
	close(repo.release)
	require.ErrorIs(t, awaitServiceSignal(t, result), ErrStaleGeneration)
	require.Empty(t, s.tickets)
	requireServiceCounts(t, f.db, 0, 0)
}

func TestReloadConcurrentDiscoveryNeverMixesSnapshotAndIndex(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	// Pre-build two immutable artifacts, then switch only the trusted manifest.
	manifestA, err := os.ReadFile(filepath.Join(f.directory, "current.json"))
	require.NoError(t, err)
	b := f.publish(t, "更新名")
	manifestB, err := os.ReadFile(filepath.Join(f.directory, "current.json"))
	require.NoError(t, err)
	names := map[string]string{f.snapshot.Digest(): "新名", b.Digest(): "更新名"}
	stop := make(chan struct{})
	results := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				results <- nil
				return
			default:
			}
			page, err := s.Sources(t.Context(), "r18-only-900", 0, 100)
			if err != nil {
				results <- err
				return
			}
			if len(page.Items) != 1 || page.Items[0].JapaneseName != names[page.ArtifactDigest] {
				results <- ErrStaleGeneration
				return
			}
		}
	}()
	for i := range 20 {
		manifest := manifestA
		if i%2 == 0 {
			manifest = manifestB
		}
		require.NoError(t, os.WriteFile(filepath.Join(f.directory, "current.json"), manifest, 0600))
		_, err := s.Reload(t.Context())
		require.NoError(t, err)
	}
	close(stop)
	require.NoError(t, awaitServiceSignal(t, results))
	require.Empty(t, s.readSlots)
	require.Empty(t, s.reload)
}

func TestReloadStartupWaitCancellationAndStoppingReleaseAdmission(t *testing.T) {
	f := newServiceFixture(t)
	s, cancel := f.service(t, f.repo)
	// A cancelled request cannot publish while initialization is not yet ready.
	ctx, requestCancel := context.WithCancel(t.Context())
	requestCancel()
	_, err := s.Reload(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, s.reload)
	s.initialize()
	old := s.generation
	cancel()
	_, err = s.Reload(t.Context())
	require.ErrorIs(t, err, ErrStopping)
	require.Same(t, old, s.generation)
	require.Empty(t, s.reload)
}

func TestReloadAlreadyLeasedJobRetainsOriginalSnapshot(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	_, job := f.confirm(t, s)
	pinned, err := s.snapshotForJob(t.Context(), job.SnapshotDigest, nil)
	require.NoError(t, err)
	lease, err := f.repo.ClaimOfflineNameSync(t.Context(), job.ID, "reload-fixture-worker", f.now, workerLease)
	require.NoError(t, err)
	f.publish(t, "更新名")
	_, err = s.Reload(t.Context())
	require.NoError(t, err)
	_, err = f.repo.ApplyOfflineNameSync(t.Context(), *lease, pinned, f.now)
	require.NoError(t, err)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "新名", actor.JapaneseName)
}

func TestReloadAdmittedCancellationCannotPublishAcrossConfirmationGate(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	old := s.generation
	review := f.review(t, s)
	f.publish(t, "更新名")
	// Occupying final confirmation admission prevents publication regardless of
	// whether cancellation reaches validation or the final publication wait.
	s.confirm <- struct{}{}
	defer func() { <-s.confirm }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Reload(ctx); done <- err }()
	// Observe admission with a channel handshake, then restore its token before
	// cancellation. No timing assumptions or production test hooks are required.
	awaitServiceSignal(t, s.reload)
	s.reload <- struct{}{}
	cancel()
	require.ErrorIs(t, awaitServiceSignal(t, done), context.Canceled)
	require.Same(t, old, s.generation)
	require.Contains(t, s.tickets, review.ReviewID)
	require.Empty(t, s.reload)
	info, err := s.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, "reload_cancelled", info.ReloadProblemCode)
	require.Equal(t, old.snapshot.Digest(), info.ArtifactDigest)
	requireServiceCounts(t, f.db, 0, 0)
}

func TestReloadGenerationReadBoundsAndPreconditionValidation(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	for range cap(s.readSlots) {
		s.readSlots <- struct{}{}
	}
	_, err := s.SourcesAt(t.Context(), "", 0, 1, f.snapshot.Digest())
	require.ErrorIs(t, err, ErrReviewCapacity)
	for range cap(s.readSlots) {
		<-s.readSlots
	}
	_, err = s.SourcesAt(t.Context(), "", 0, 1, f.snapshot.Digest())
	require.NoError(t, err)
	require.Empty(t, s.readSlots)
	choices := []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}}
	for _, digest := range []string{"x", "../current.json", "https://example.invalid/evidence", string(make([]byte, 64))} {
		_, err = s.SourcesAt(t.Context(), "", 0, 1, digest)
		require.ErrorIs(t, err, ErrInvalid)
		_, err = s.ReviewAt(t.Context(), fixturePrincipal, choices, digest)
		require.ErrorIs(t, err, ErrInvalid)
	}
	require.Empty(t, s.readSlots)
	require.Empty(t, s.reviewSlots)
	require.Empty(t, s.tickets)
	requireServiceCounts(t, f.db, 0, 0)
}

// Compare every application table, not just the selected actor: reload must not
// repair aliases, credits, ownership, quarantine or render state as a side effect.
func reloadDatabaseState(t *testing.T, db *database.DB) map[string][]map[string]interface{} {
	t.Helper()
	var names []string
	require.NoError(t, db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&names).Error)
	result := make(map[string][]map[string]interface{})
	for _, name := range names {
		var rows []map[string]interface{}
		require.NoError(t, db.Table(name).Order("rowid").Find(&rows).Error)
		result[name] = rows
	}
	return result
}
