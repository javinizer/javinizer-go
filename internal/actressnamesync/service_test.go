package actressnamesync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

const fixturePrincipal = "api-token:verified-fixture-owner"

type serviceFixture struct {
	db        *database.DB
	repo      *database.ActressRepository
	dbPath    string
	directory string
	now       time.Time
	snapshot  *actresscache.Snapshot
	actor     models.Actress
	key       string
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	f := &serviceFixture{
		dbPath:    filepath.Join(t.TempDir(), "offline-names.db"),
		directory: t.TempDir(),
		now:       time.Date(2026, 10, 25, 2, 0, 0, 0, time.UTC),
	}
	f.openDB(t)
	f.actor = models.Actress{DMMID: 7, FirstName: "Managed", LastName: "Actor", JapaneseName: "旧名", Aliases: "Keep metadata", ThumbURL: "https://example.invalid/keep.jpg"}
	require.NoError(t, f.repo.ImportUpsert(t.Context(), &f.actor))
	var err error
	f.key, err = actresscache.SourceKey("r18dev", "r18-only-900")
	require.NoError(t, err)
	f.snapshot = f.publish(t, "新名")
	return f
}

func (f *serviceFixture) openDB(t *testing.T) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: f.dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	f.db, f.repo = db, database.NewActressRepository(db)
}

func (f *serviceFixture) reopen(t *testing.T) {
	t.Helper()
	require.NoError(t, f.db.Close())
	f.openDB(t)
}

func (f *serviceFixture) publish(t *testing.T, japaneseName string) *actresscache.Snapshot {
	t.Helper()
	dump := "COPY public.derived_actress (id, name_kanji, name_romaji) FROM stdin;\nr18-only-900\t" + japaneseName + "\tUntrusted source alias\nr18-only-901\t別新名\tOther source alias\n\\.\n"
	limits := offline.DefaultLimits()
	snapshot, receipt, err := r18dump.Convert(t.Context(), strings.NewReader(dump), actresscache.Provenance{SourceURL: "https://example.invalid/offline-dump", ParserRevision: "service-fixture-v1", ObservedAt: f.now}, limits.Source)
	require.NoError(t, err)
	store, err := artifactstore.Open(f.directory, limits.Store)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		result, err := store.Publish(t.Context(), bytes.NewReader(snapshot.Bytes()), receipt)
		require.NoError(t, err)
		require.True(t, result.Published)
		require.True(t, result.FilesSynced)
		require.True(t, result.DirectorySynced)
	} else {
		// Native publication intentionally fails closed on Windows and other
		// unsupported writers. Install known synthetic fixture bytes only, then
		// exercise the real native reader and every service test without skips.
		previous, err := os.ReadFile(filepath.Join(f.directory, "current.json"))
		if err == nil {
			require.NoError(t, os.WriteFile(filepath.Join(f.directory, "previous.json"), previous, 0600))
		} else {
			require.ErrorIs(t, err, os.ErrNotExist)
		}
		manifest := artifactstore.Manifest{Version: 1, Scope: "supplied-file-only", ArtifactSHA256: snapshot.Digest(), ArtifactBytes: int64(len(snapshot.Bytes())), Source: receipt}
		data, err := json.Marshal(manifest)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(f.directory, snapshot.Digest()+".ndjson"), snapshot.Bytes(), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(f.directory, "current.json"), append(data, '\n'), 0600))
	}
	view, err := store.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, snapshot.Digest(), view.Snapshot.Digest())
	require.False(t, view.Recovered)
	return view.Snapshot
}

func (f *serviceFixture) service(t *testing.T, repo Repository, options ...Options) (*Service, context.CancelFunc) {
	t.Helper()
	opts := Options{Enabled: true, ArtifactDirectory: f.directory, MaxAge: time.Hour}
	if len(options) != 0 {
		opts = options[0]
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	s, err := New(ctx, repo, opts)
	require.NoError(t, err)
	fixedNow := f.now
	s.now = func() time.Time { return fixedNow }
	return s, cancel
}

func (f *serviceFixture) review(t *testing.T, s *Service) *Review {
	t.Helper()
	r, err := s.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}})
	require.NoError(t, err)
	require.Len(t, r.Proposals, 1)
	return r
}

func (f *serviceFixture) confirm(t *testing.T, s *Service) (*Review, *JobView) {
	t.Helper()
	r := f.review(t, s)
	j, err := s.Confirm(t.Context(), fixturePrincipal, r.ReviewID, true)
	require.NoError(t, err)
	return r, j
}

// Timeouts only guard deadlocks. Worker ordering is established by channels,
// returned transactions, or direct deterministic workPass calls, never sleeps.
func awaitServiceSignal[T any](t *testing.T, signal <-chan T) T {
	t.Helper()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case result := <-signal:
		return result
	case <-timer.C:
		t.Fatal("timed out waiting for service synchronization")
		var zero T
		return zero
	}
}

func startService(t *testing.T, s *Service, cancel context.CancelFunc) func() {
	t.Helper()
	done := make(chan struct{})
	go func() { s.Run(); close(done) }()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); awaitServiceSignal(t, done) }) }
	t.Cleanup(stop)
	awaitServiceSignal(t, s.Ready())
	return stop
}

type applyResult struct {
	task *database.ActressSyncTask
	err  error
}

type observedApplyRepository struct {
	Repository
	applied chan applyResult
}

func (r *observedApplyRepository) ApplyOfflineNameSync(ctx context.Context, lease database.ActressSyncLease, snapshot *actresscache.Snapshot, now time.Time) (*database.ActressSyncTask, error) {
	task, err := r.Repository.ApplyOfflineNameSync(ctx, lease, snapshot, now)
	r.applied <- applyResult{task: task, err: err}
	return task, err
}

func TestServicePublishedReviewRunReceiptAndDurableReplay(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	info, err := s.Info(t.Context())
	require.NoError(t, err)
	require.True(t, info.Available)
	require.Equal(t, f.snapshot.Digest(), info.ArtifactDigest)
	require.Equal(t, 2, info.RecordCount)
	require.True(t, info.RestartRequiredForConfigChanges)
	page, err := s.Sources(t.Context(), "r18-only-900", 0, 1)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, f.key, page.Items[0].SourceKey)

	before, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.NoError(t, f.db.Create(&models.Movie{ContentID: "service-name-film", ID: "service-name-film", Title: "Fixture"}).Error)
	credit := models.MovieCredit{MovieContentID: "service-name-film", ActressID: f.actor.ID, CreditedName: "Reported old name", CreditedJapaneseName: "Reported Japanese", Source: "dmm", Origin: string(models.CreditOriginScrape), UserOverride: true, OverrideName: "Manual display", Suppressed: true}
	require.NoError(t, f.db.Create(&credit).Error)
	require.NoError(t, f.db.Create(&models.ActressTranslation{ActressID: f.actor.ID, Language: "en", DisplayName: "Old translation"}).Error)
	review := f.review(t, s)
	unconfirmed := f.review(t, s)
	require.Equal(t, "旧名", review.Proposals[0].BeforeName)
	require.Equal(t, "新名", review.Proposals[0].AfterName)
	// Returned UI data cannot alter server-held authority.
	review.Proposals[0].AfterName = "Client forged patch"
	review.Proposals[0].Source.JapaneseName = "Client forged evidence"
	_, err = s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, false)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.Confirm(t.Context(), "api-token:other-owner", review.ReviewID, true)
	require.ErrorIs(t, err, ErrPrincipal)
	_, err = s.Confirm(t.Context(), fixturePrincipal, strings.Repeat("0", 64), true)
	require.ErrorIs(t, err, ErrReviewExpired)
	unchanged, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, before, unchanged)
	requireServiceCounts(t, f.db, 0, 0)
	job, err := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.NoError(t, err)
	require.Equal(t, "pending", job.Status)
	unchanged, err = f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, before, unchanged, "confirmation enqueues but does not mutate names")

	f.reopen(t)
	observed := &observedApplyRepository{Repository: f.repo, applied: make(chan applyResult, 1)}
	restarted, cancel := f.service(t, observed)
	stop := startService(t, restarted, cancel)
	result := awaitServiceSignal(t, observed.applied)
	require.NoError(t, result.err)
	require.Equal(t, "applied", result.task.Status)
	stop()
	// Reopen a second time to prove both receipt and principal-bound replay are
	// durable, not cached by the first service or repository instance.
	f.reopen(t)
	reader, _ := f.service(t, f.repo, Options{Enabled: false})
	reader.initialize()
	replayed, err := reader.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.NoError(t, err)
	require.Equal(t, job.ID, replayed.ID)
	require.Equal(t, "completed", replayed.Status)
	require.Equal(t, 1, replayed.Counts["applied"])
	_, err = reader.Confirm(t.Context(), "api-token:other-owner", review.ReviewID, true)
	require.ErrorIs(t, err, ErrReviewExpired)
	_, err = reader.Confirm(t.Context(), fixturePrincipal, unconfirmed.ReviewID, true)
	require.ErrorIs(t, err, ErrReviewExpired, "an unconfirmed nonce must not survive restart")
	receipts, err := reader.Tasks(t.Context(), job.ID, 0, 1)
	require.NoError(t, err)
	require.Len(t, receipts.Items, 1)
	require.Nil(t, receipts.NextOffset)
	require.Equal(t, "旧名", receipts.Items[0].BeforeName)
	require.Equal(t, "新名", receipts.Items[0].AfterName)
	require.Equal(t, "applied", receipts.Items[0].Status)
	require.NotNil(t, receipts.Items[0].FinishedAt)
	after, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "新名", after.JapaneseName)
	require.Equal(t, before.DMMID, after.DMMID)
	require.Equal(t, before.Origin, after.Origin)
	require.Equal(t, before.Verified, after.Verified)
	require.Equal(t, before.FirstName, after.FirstName)
	require.Equal(t, before.LastName, after.LastName)
	require.Equal(t, before.ThumbURL, after.ThumbURL)
	require.Equal(t, before.Aliases, after.Aliases)
	var retainedCredit models.MovieCredit
	require.NoError(t, f.db.First(&retainedCredit, credit.ID).Error)
	require.Equal(t, credit, retainedCredit)
	var translations, sourceAliases int64
	require.NoError(t, f.db.Model(&models.ActressTranslation{}).Where("actress_id = ?", f.actor.ID).Count(&translations).Error)
	require.Zero(t, translations)
	require.NoError(t, f.db.Model(&models.ActressAlias{}).Where("alias_name = ?", "Untrusted source alias").Count(&sourceAliases).Error)
	require.Zero(t, sourceAliases)
	requireServiceCounts(t, f.db, 1, 1)
}

func requireServiceCounts(t *testing.T, db *database.DB, wantJobs, wantTasks int64) {
	t.Helper()
	var jobs, tasks int64
	require.NoError(t, db.Model(&database.ActressSyncJob{}).Count(&jobs).Error)
	require.NoError(t, db.Model(&database.ActressSyncTask{}).Count(&tasks).Error)
	require.Equal(t, wantJobs, jobs)
	require.Equal(t, wantTasks, tasks)
}

func TestServiceHistoricalSnapshotRecoveryAfterManifestRotation(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	_, job := f.confirm(t, s)
	f.publish(t, "次世代名")
	latest := f.publish(t, "最新世代名")
	f.reopen(t)
	recovered, _ := f.service(t, f.repo)
	recovered.initialize()
	info, err := recovered.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, latest.Digest(), info.ArtifactDigest)
	_, historical := recovered.workPass(t.Context(), "", nil)
	require.NotNil(t, historical)
	require.Equal(t, f.snapshot.Digest(), historical.Digest())
	receipts, err := recovered.Tasks(t.Context(), job.ID, 0, 10)
	require.NoError(t, err)
	require.Equal(t, "applied", receipts.Items[0].Status)
	require.Equal(t, "新名", receipts.Items[0].AfterName, "never substitute current/previous source generations")
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "新名", actor.JapaneseName)
	current, err := recovered.Sources(t.Context(), "r18-only-900", 0, 1)
	require.NoError(t, err)
	require.Equal(t, "最新世代名", current.Items[0].JapaneseName)
}

func TestServiceMissingOrCorruptHistoricalSourcePersistsBlockedDiagnostics(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "corrupt"}[corrupt], func(t *testing.T) {
			f := newServiceFixture(t)
			s, _ := f.service(t, f.repo)
			s.initialize()
			_, job := f.confirm(t, s)
			f.publish(t, "次世代名")
			f.publish(t, "最新世代名")
			object := filepath.Join(f.directory, f.snapshot.Digest()+".ndjson")
			code := database.ActressSyncProblemSnapshotMissing
			if corrupt {
				code = database.ActressSyncProblemSnapshotInvalid
				require.NoError(t, os.WriteFile(object, []byte("corrupt synthetic fixture"), 0600))
			} else {
				require.NoError(t, os.Remove(object))
			}
			f.reopen(t)
			recovered, _ := f.service(t, f.repo)
			recovered.initialize()
			recovered.workPass(t.Context(), "", nil)
			f.reopen(t)
			reader, _ := f.service(t, f.repo, Options{Enabled: false})
			reader.initialize()
			blocked, err := reader.Job(t.Context(), job.ID)
			require.NoError(t, err)
			require.Equal(t, "blocked", blocked.Status)
			receipts, err := reader.Tasks(t.Context(), job.ID, 0, 10)
			require.NoError(t, err)
			require.Equal(t, "pending", receipts.Items[0].Status)
			require.Equal(t, code, receipts.Items[0].ProblemCode)
			require.Equal(t, 1, receipts.Items[0].ProblemAttempts)
			require.Greater(t, receipts.Items[0].RetryAfterMS, f.now.UnixMilli())
			require.Nil(t, receipts.Items[0].FinishedAt)
			cancelled, err := reader.Cancel(t.Context(), job.ID)
			require.NoError(t, err)
			require.True(t, cancelled.Cancelled)
			require.Equal(t, "cancelled", cancelled.Status)
			require.Equal(t, 1, cancelled.Counts["cancelled"])
			receipts, err = reader.Tasks(t.Context(), job.ID, 0, 10)
			require.NoError(t, err)
			require.Empty(t, receipts.Items[0].ProblemCode, "terminal receipts suppress historical retry diagnostics")
			actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
			require.NoError(t, err)
			require.Equal(t, "旧名", actor.JapaneseName)
		})
	}
}

func TestServiceExpiredLeaseRecoveryFencesFormerWorker(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	_, job := f.confirm(t, s)
	lease, err := f.repo.ClaimOfflineNameSync(t.Context(), job.ID, "crashed-worker", f.now, time.Second)
	require.NoError(t, err)
	f.reopen(t)
	recovered, _ := f.service(t, f.repo)
	recovered.initialize()
	at := f.now.Add(999 * time.Millisecond)
	recovered.now = func() time.Time { return at }
	recovered.workPass(t.Context(), "", nil)
	beforeExpiry, err := recovered.Job(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "running", beforeExpiry.Status)
	at = f.now.Add(time.Second)
	recovered.workPass(t.Context(), "", nil)
	finished, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", finished.Tasks[0].Status)
	require.Greater(t, finished.Tasks[0].LeaseGeneration, lease.Generation)
	_, err = f.repo.ApplyOfflineNameSync(t.Context(), *lease, f.snapshot, at)
	require.ErrorIs(t, err, database.ErrActressSyncLease)
}

func TestServiceExpiredLeaseWithMissingSourceBecomesBlockedAndFenced(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	_, job := f.confirm(t, s)
	oldLease, err := f.repo.ClaimOfflineNameSync(t.Context(), job.ID, "crashed-worker", f.now, time.Second)
	require.NoError(t, err)
	f.publish(t, "次世代名")
	f.publish(t, "最新世代名")
	object := filepath.Join(f.directory, f.snapshot.Digest()+".ndjson")
	require.NoError(t, os.Remove(object))
	f.reopen(t)
	recovered, _ := f.service(t, f.repo)
	recovered.initialize()
	failureAt := f.now.Add(time.Second)
	recovered.now = func() time.Time { return failureAt }
	recovered.workPass(t.Context(), "", nil)
	blocked, err := recovered.Job(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "blocked", blocked.Status)
	durable, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	task := durable.Tasks[0]
	require.Equal(t, "pending", task.Status)
	require.Empty(t, task.LeaseOwner)
	require.Zero(t, task.LeaseUntilMS)
	require.Equal(t, oldLease.Generation, task.LeaseGeneration)
	require.Equal(t, database.ActressSyncProblemSnapshotMissing, task.ProblemCode)
	require.Equal(t, 1, task.ProblemAttempts)
	require.Greater(t, task.RetryAfterMS, failureAt.UnixMilli())
	_, err = f.repo.ApplyOfflineNameSync(t.Context(), *oldLease, f.snapshot, failureAt)
	require.ErrorIs(t, err, database.ErrActressSyncLease)

	// Both pending state and retry deadline survive a full DB/service restart.
	f.reopen(t)
	retry, _ := f.service(t, f.repo)
	retry.initialize()
	at := failureAt.Add(time.Millisecond)
	retry.now = func() time.Time { return at }
	retry.workPass(t.Context(), "", nil)
	still, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", still.Tasks[0].Status)
	require.Equal(t, 1, still.Tasks[0].ProblemAttempts)
	require.Equal(t, task.RetryAfterMS, still.Tasks[0].RetryAfterMS)
	// Repair only the exact synthetic retained object; the current snapshot is
	// deliberately different and is never sufficient to satisfy the old job.
	require.NoError(t, os.WriteFile(object, f.snapshot.Bytes(), 0600))
	at = time.UnixMilli(task.RetryAfterMS)
	retry.workPass(t.Context(), "", nil)
	finished, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", finished.Tasks[0].Status)
	require.Equal(t, "新名", finished.Tasks[0].AfterName)
	require.Greater(t, finished.Tasks[0].LeaseGeneration, oldLease.Generation)
	require.Empty(t, finished.Tasks[0].ProblemCode)
}

type failingApplyRepository struct {
	Repository
	err error
}

func (r *failingApplyRepository) ApplyOfflineNameSync(context.Context, database.ActressSyncLease, *actresscache.Snapshot, time.Time) (*database.ActressSyncTask, error) {
	return nil, r.err
}

func TestServiceApplyFailurePersistsBoundedDiagnosticAndRetries(t *testing.T) {
	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	_, job := f.confirm(t, s)
	failing, _ := f.service(t, &failingApplyRepository{Repository: f.repo, err: errors.New("synthetic transaction failure with private details")})
	failing.initialize()
	failing.workPass(t.Context(), "", nil)
	f.reopen(t)
	recovered, _ := f.service(t, f.repo)
	recovered.initialize()
	blocked, err := recovered.Job(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "blocked", blocked.Status)
	receipts, err := recovered.Tasks(t.Context(), job.ID, 0, 1)
	require.NoError(t, err)
	require.Equal(t, database.ActressSyncProblemApplyFailed, receipts.Items[0].ProblemCode)
	require.Equal(t, 1, receipts.Items[0].ProblemAttempts)
	require.Equal(t, "pending", receipts.Items[0].Status)
	require.Empty(t, receipts.Items[0].Reason)
	retryAt := time.UnixMilli(receipts.Items[0].RetryAfterMS)
	recovered.now = func() time.Time { return retryAt }
	recovered.workPass(t.Context(), "", nil)
	finished, err := recovered.Job(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", finished.Status)
	require.Equal(t, 1, finished.Counts["applied"])
}

type shutdownRepository struct {
	Repository
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
	result    chan error
}

func (r *shutdownRepository) ApplyOfflineNameSync(ctx context.Context, lease database.ActressSyncLease, snapshot *actresscache.Snapshot, now time.Time) (*database.ActressSyncTask, error) {
	close(r.entered)
	<-ctx.Done()
	close(r.cancelled)
	<-r.release
	task, err := r.Repository.ApplyOfflineNameSync(ctx, lease, snapshot, now)
	r.result <- err
	return task, err
}

func TestServiceShutdownCancelsInflightWorkAndJoinsRun(t *testing.T) {
	f := newServiceFixture(t)
	admission, _ := f.service(t, f.repo)
	admission.initialize()
	_, job := f.confirm(t, admission)
	repo := &shutdownRepository{Repository: f.repo, entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}), result: make(chan error, 1)}
	s, cancel := f.service(t, repo)
	done := make(chan struct{})
	go func() { s.Run(); close(done) }()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(repo.release) }) }
	t.Cleanup(func() { cancel(); release(); awaitServiceSignal(t, done) })
	awaitServiceSignal(t, s.Ready())
	awaitServiceSignal(t, repo.entered)
	cancel()
	awaitServiceSignal(t, repo.cancelled)
	select {
	case <-done:
		t.Fatal("Run returned before its in-flight repository call joined")
	default:
	}
	_, err := s.Info(context.Background())
	require.ErrorIs(t, err, ErrStopping)
	release()
	require.True(t, errors.Is(awaitServiceSignal(t, repo.result), context.Canceled))
	awaitServiceSignal(t, done)
	actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
	durable, err := f.repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "leased", durable.Tasks[0].Status, "shutdown cannot create a committed applying state")
	require.Empty(t, durable.Tasks[0].ProblemCode, "shutdown is not an operational apply failure")
	// A new lifetime may recover the abandoned lease at exact expiry.
	recovered, _ := f.service(t, f.repo)
	recovered.now = func() time.Time { return f.now.Add(workerLease) }
	recovered.initialize()
	recovered.workPass(t.Context(), "", nil)
	finished, err := recovered.Job(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", finished.Status)
	require.Equal(t, 1, finished.Counts["applied"])
}
