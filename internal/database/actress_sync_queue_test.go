package database

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func queueSyncFixture(t *testing.T, n int) (*DB, *ActressRepository, *actresscache.Snapshot, []ActressSyncReview, time.Time, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "queue.db")
	db := lifecycleSyncOpen(t, path)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewActressRepository(db)
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	var dump strings.Builder
	dump.WriteString("COPY public.derived_actress (id, name_kanji) FROM stdin;\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&dump, "queue-%d\t新しい名前%d\n", i, i)
	}
	dump.WriteString("\\.\n")
	limits := actresscache.Limits{MaxBytes: 1 << 20, MaxRecordBytes: 1 << 16, MaxRecords: 100, MaxStringBytes: 4096, MaxTotalStringBytes: 1 << 18, MaxAliases: 32, MaxExternalIDs: 16, MaxTokensPerRecord: 1024}
	snapshot, _, err := r18dump.Convert(t.Context(), strings.NewReader(dump.String()), actresscache.Provenance{SourceURL: "https://example.invalid/queue-fixture.sql", ParserRevision: "queue-v1", ObservedAt: now}, r18dump.Limits{MaxInputBytes: 1 << 16, MaxLineBytes: 1024, MaxRows: 100, Output: limits})
	require.NoError(t, err)
	reviews := make([]ActressSyncReview, n)
	for i := range reviews {
		actor := lifecycleSyncManaged(t, db, 9000+i, fmt.Sprintf("旧名%d", i))
		key, err := actresscache.SourceKey("r18dev", fmt.Sprintf("queue-%d", i))
		require.NoError(t, err)
		review, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: actor.ID, SourceKey: key}}, snapshot, actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 100})
		require.NoError(t, err)
		reviews[i] = *review
	}
	return db, repo, snapshot, reviews, now, path
}

func TestOfflineNameSyncQueueAdmissionPreservesIdempotency(t *testing.T) {
	db, repo, snapshot, reviews, now, _ := queueSyncFixture(t, 2)
	limits := ActressSyncQueueLimits{MaxQueuedJobs: 1, MaxQueuedTasks: 1}
	first, err := repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "queue-first", reviews[0], snapshot, now, limits)
	require.NoError(t, err)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), first.ID, "queue-worker", now, time.Minute)
	require.NoError(t, err)
	_, err = repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "queue-second", reviews[1], snapshot, now, limits)
	require.ErrorIs(t, err, ErrActressSyncQueueFull, "live leases count toward admission")
	duplicate, err := repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "queue-first", reviews[0], nil, now.Add(24*time.Hour), limits)
	require.NoError(t, err)
	require.Equal(t, first.ID, duplicate.ID)
	require.Equal(t, "leased", duplicate.Tasks[0].Status)
	changed := reviews[0]
	changed.MaxAge++
	_, err = repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "queue-first", changed, snapshot, now, limits)
	require.ErrorIs(t, err, ErrActressSyncConflict, "idempotent payload conflict wins over queue pressure")
	var jobs, tasks, bindings int64
	require.NoError(t, db.Model(&ActressSyncJob{}).Count(&jobs).Error)
	require.NoError(t, db.Model(&ActressSyncTask{}).Count(&tasks).Error)
	require.NoError(t, db.Model(&actressSyncBinding{}).Count(&bindings).Error)
	require.EqualValues(t, 1, jobs)
	require.EqualValues(t, 1, tasks)
	require.EqualValues(t, 1, bindings, "rejected submission must not create source ownership")
	_, err = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
	require.NoError(t, err)
	second, err := repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "queue-second", reviews[1], snapshot, now, limits)
	require.NoError(t, err, "completed receipts do not consume queue capacity")
	require.NotEqual(t, first.ID, second.ID)
}

func TestOfflineNameSyncQueueAdmissionTaskBudgetAndCancellation(t *testing.T) {
	_, repo, snapshot, reviews, now, _ := queueSyncFixture(t, 3)
	batch := reviews[0]
	batch.Proposals = append(append([]ActressSyncProposal(nil), batch.Proposals...), reviews[1].Proposals...)
	_, err := repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "batch-too-large", batch, snapshot, now, ActressSyncQueueLimits{MaxQueuedJobs: 2, MaxQueuedTasks: 1})
	require.ErrorIs(t, err, ErrActressSyncQueueFull)
	limits := ActressSyncQueueLimits{MaxQueuedJobs: 2, MaxQueuedTasks: 2}
	first, err := repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "two-task-batch", batch, snapshot, now, limits)
	require.NoError(t, err)
	_, err = repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "task-budget-full", reviews[2], snapshot, now, limits)
	require.ErrorIs(t, err, ErrActressSyncQueueFull)
	require.NoError(t, repo.CancelOfflineNameSync(t.Context(), first.ID))
	_, err = repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "task-budget-full", reviews[2], snapshot, now, limits)
	require.NoError(t, err, "cancellation frees capacity without deleting historical work")
}

func TestOfflineNameSyncQueueAdmissionAcrossIndependentConnections(t *testing.T) {
	db, repo, snapshot, reviews, now, path := queueSyncFixture(t, 2)
	other := lifecycleSyncOpen(t, path)
	t.Cleanup(func() { _ = other.Close() })
	require.NoError(t, other.Exec("PRAGMA busy_timeout = 0").Error)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admitted, conflict := make(chan error, 1), make(chan error, 1)
	release := make(chan struct{})
	var firstOnce, secondOnce, releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseFirst()
	const firstHook, secondHook = "test:queue_first_admission", "test:queue_second_contention"
	require.NoError(t, db.Callback().Raw().After("gorm:raw").Register(firstHook, func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "UPDATE actress_sync_jobs SET cancelled") {
			firstOnce.Do(func() {
				admitted <- tx.Error
				select {
				case <-release:
				case <-ctx.Done():
				}
			})
		}
	}))
	defer func() { require.NoError(t, db.Callback().Raw().Remove(firstHook)) }()
	require.NoError(t, other.Callback().Raw().After("gorm:raw").Register(secondHook, func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "UPDATE actress_sync_jobs SET cancelled") && isLocked(tx.Error) {
			secondOnce.Do(func() { conflict <- tx.Error })
		}
	}))
	defer func() { require.NoError(t, other.Callback().Raw().Remove(secondHook)) }()
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	limits := ActressSyncQueueLimits{MaxQueuedJobs: 1, MaxQueuedTasks: 1}
	go func() {
		_, err := repo.SubmitConfirmedOfflineNameSyncWithLimits(ctx, "independent-first", reviews[0], snapshot, now, limits)
		firstDone <- err
	}()
	select {
	case err := <-admitted:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("first submit did not acquire write admission")
	}
	go func() {
		_, err := NewActressRepository(other).SubmitConfirmedOfflineNameSyncWithLimits(ctx, "independent-second", reviews[1], snapshot, now, limits)
		secondDone <- err
	}()
	select {
	case err := <-conflict:
		require.True(t, isLocked(err), "the second connection must observe an actual held SQLite write lock")
	case <-ctx.Done():
		t.Fatal("second submit did not contend for write admission")
	}
	releaseFirst()
	await := func(ch <-chan error) error {
		select {
		case err := <-ch:
			return err
		case <-ctx.Done():
			t.Fatal("serialized submission did not finish")
			return ctx.Err()
		}
	}
	require.NoError(t, await(firstDone))
	require.ErrorIs(t, await(secondDone), ErrActressSyncQueueFull)
	var count int64
	require.NoError(t, db.Model(&ActressSyncTask{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestOfflineNameSyncQueueInputBounds(t *testing.T) {
	_, repo, snapshot, reviews, now, _ := queueSyncFixture(t, 1)
	for _, limits := range []ActressSyncQueueLimits{{MaxQueuedJobs: -1}, {MaxQueuedJobs: MaxActressSyncQueuedJobs + 1}, {MaxQueuedTasks: -1}, {MaxQueuedTasks: MaxActressSyncQueuedTasks + 1}} {
		_, err := repo.SubmitConfirmedOfflineNameSyncWithLimits(t.Context(), "bad-limits", reviews[0], snapshot, now, limits)
		require.ErrorIs(t, err, ErrActressSyncConflict)
	}
	for _, cursor := range []string{"unsafe", "name-" + strings.Repeat("f", 65), "name-" + strings.Repeat("G", 64)} {
		_, err := repo.DueOfflineNameSyncJobs(t.Context(), now, cursor, 1)
		require.ErrorIs(t, err, ErrActressSyncConflict)
		_, err = repo.ListOfflineNameSyncJobs(t.Context(), cursor, 1)
		require.ErrorIs(t, err, ErrActressSyncConflict)
	}
	for _, limit := range []int{-1, MaxActressSyncHistoryJobs + 1} {
		_, err := repo.DueOfflineNameSyncJobs(t.Context(), now, "", limit)
		require.ErrorIs(t, err, ErrActressSyncConflict)
		_, err = repo.ListOfflineNameSyncJobs(t.Context(), "", limit)
		require.ErrorIs(t, err, ErrActressSyncConflict)
	}
	_, err := repo.DueOfflineNameSyncJobs(t.Context(), now, "", MaxActressSyncDueJobs+1)
	require.ErrorIs(t, err, ErrActressSyncConflict)
	_, err = repo.DueOfflineNameSyncJobs(t.Context(), time.Time{}, "", 1)
	require.ErrorIs(t, err, ErrActressSyncConflict)
	_, err = repo.OfflineNameSyncJobByRequestKey(t.Context(), "")
	require.ErrorIs(t, err, ErrActressSyncConflict)
	_, err = repo.OfflineNameSyncJobByRequestKey(t.Context(), "missing-request")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = repo.ListOfflineNameSyncJobs(t.Context(), "name-"+strings.Repeat("0", 64), 1)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestOfflineNameSyncQueueDuePaginationSkipsBlockedAndLiveLeases(t *testing.T) {
	_, repo, snapshot, reviews, now, _ := queueSyncFixture(t, 6)
	var ids []string
	for i, review := range reviews {
		job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), fmt.Sprintf("due-%d", i), review, snapshot, now)
		require.NoError(t, err)
		ids = append(ids, job.ID)
	}
	sort.Strings(ids)
	_, err := repo.ClaimOfflineNameSync(t.Context(), ids[0], "live-lease", now, time.Minute)
	require.NoError(t, err)
	require.NoError(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), ids[1], ActressSyncProblemSnapshotMissing, now))
	require.NoError(t, repo.CancelOfflineNameSync(t.Context(), ids[2]))
	var got []string
	cursor := ""
	for {
		page, err := repo.DueOfflineNameSyncJobs(t.Context(), now, cursor, 1)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		require.Len(t, page, 1)
		require.Empty(t, page[0].Tasks, "discovery returns metadata, not unbounded receipt collections")
		cursor = page[0].ID
		got = append(got, cursor)
		require.LessOrEqual(t, len(got), len(ids))
	}
	require.Equal(t, ids[3:], got, "filters must run before LIMIT, not hide eligible later jobs")
	page, err := repo.DueOfflineNameSyncJobs(t.Context(), now.Add(time.Minute), "", 0)
	require.NoError(t, err)
	require.Len(t, page, 5, "expired leases and backoff become discoverable; cancellation stays terminal")
	require.Equal(t, ids[0], page[0].ID)
}

func TestOfflineNameSyncQueueHistoryKeysetAndRequestReplayAfterReopen(t *testing.T) {
	db, repo, snapshot, reviews, now, path := queueSyncFixture(t, 3)
	var expected []ActressSyncJob
	for i, review := range reviews {
		at := now
		if i == 2 {
			at = now.Add(time.Second)
		}
		job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), fmt.Sprintf("history-%d", i), review, snapshot, at)
		require.NoError(t, err)
		expected = append(expected, *job)
	}
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].CreatedAt.Equal(expected[j].CreatedAt) {
			return expected[i].ID > expected[j].ID
		}
		return expected[i].CreatedAt.After(expected[j].CreatedAt)
	})
	var got []string
	cursor := ""
	for range expected {
		page, err := repo.ListOfflineNameSyncJobs(t.Context(), cursor, 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Empty(t, page[0].Tasks)
		cursor = page[0].ID
		got = append(got, cursor)
	}
	for i := range expected {
		require.Equal(t, expected[i].ID, got[i])
	}
	page, err := repo.ListOfflineNameSyncJobs(t.Context(), cursor, 1)
	require.NoError(t, err)
	require.Empty(t, page)
	page, err = repo.ListOfflineNameSyncJobs(t.Context(), "", 0)
	require.NoError(t, err)
	require.Len(t, page, 3)
	job, err := repo.OfflineNameSyncJobByRequestKey(t.Context(), "history-0")
	require.NoError(t, err)
	require.NoError(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotInvalid, now))
	require.NoError(t, db.Close())
	reopened := lifecycleSyncOpen(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	gotJob, err := NewActressRepository(reopened).OfflineNameSyncJobByRequestKey(t.Context(), "history-0")
	require.NoError(t, err)
	require.Equal(t, job.ID, gotJob.ID)
	require.Equal(t, job.RequestHash, gotJob.RequestHash)
	require.Equal(t, ActressSyncProblemSnapshotInvalid, gotJob.Tasks[0].ProblemCode)
	require.Equal(t, now.Add(5*time.Second).UnixMilli(), gotJob.Tasks[0].RetryAfterMS)
	require.Equal(t, 1, gotJob.Tasks[0].ProblemAttempts)
}
