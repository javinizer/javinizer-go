package database

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// syncStorageFault selects a reachable storage operation, not a synthetic
// domain state. After-write faults prove the enclosing transaction rolls back
// even when SQLite successfully executed an earlier statement.
type syncStorageFault struct {
	name      string
	operation string
	callback  string
	table     string
	sql       string
	nth       int
	after     bool
}

func injectSyncStorageFault(t *testing.T, db *DB, fault syncStorageFault) (error, func()) {
	t.Helper()
	injected := errors.New("injected offline name storage failure")
	matches, reached := 0, 0
	var originalError error
	var affected int64
	callbackName := "test:offline_name_storage_fault"
	hook := func(tx *gorm.DB) {
		if tx.Statement == nil || (fault.table != "" && tx.Statement.Table != fault.table) || (fault.sql != "" && !strings.Contains(tx.Statement.SQL.String(), fault.sql)) {
			return
		}
		matches++
		nth := fault.nth
		if nth == 0 {
			nth = 1
		}
		if matches != nth {
			return
		}
		reached++
		originalError = tx.Error
		affected = tx.RowsAffected
		_ = tx.AddError(injected)
	}
	var register func(string, func(*gorm.DB)) error
	var remove func(string) error
	switch fault.callback {
	case "query":
		register = db.Callback().Query().Before("gorm:query").Register
		remove = db.Callback().Query().Remove
	case "row":
		register = db.Callback().Row().Before("gorm:row").Register
		remove = db.Callback().Row().Remove
	case "raw":
		register = db.Callback().Raw().After("gorm:raw").Register
		remove = db.Callback().Raw().Remove
	case "create":
		register = db.Callback().Create().After("gorm:create").Register
		remove = db.Callback().Create().Remove
	case "update":
		if fault.after {
			register = db.Callback().Update().After("gorm:update").Register
		} else {
			register = db.Callback().Update().Before("gorm:update").Register
		}
		remove = db.Callback().Update().Remove
	default:
		t.Fatalf("unsupported fault callback %q", fault.callback)
	}
	require.NoError(t, register(callbackName, hook))
	removed := false
	t.Cleanup(func() {
		if !removed {
			_ = remove(callbackName)
		}
	})
	return injected, func() {
		t.Helper()
		require.NoError(t, remove(callbackName))
		removed = true
		require.Equal(t, 1, reached, "the targeted storage stage must actually be reached: %s", fault.name)
		require.NoError(t, originalError, "the injected failure must be the first error")
		if fault.callback == "create" || (fault.callback == "update" && fault.after) {
			require.Positive(t, affected, "the write must execute before the after-write fault")
		}
	}
}

func TestOfflineNameSyncStorageFailuresPropagateAndRollback(t *testing.T) {
	faults := []syncStorageFault{
		{name: "review actor read", operation: "review", callback: "query", table: "actresses"},
		{name: "review provenance read", operation: "review", callback: "row", sql: "FROM actress_import_name_provenance"},
		{name: "review guard read", operation: "review", callback: "row", sql: "FROM actress_sync_name_guards"},
		{name: "submit write admission", operation: "submit", callback: "raw", sql: "UPDATE actress_sync_jobs SET cancelled"},
		{name: "submit idempotency lookup", operation: "submit", callback: "query", table: "actress_sync_jobs"},
		{name: "submit job creation", operation: "submit", callback: "create", table: "actress_sync_jobs"},
		{name: "submit actor revalidation", operation: "submit", callback: "query", table: "actresses"},
		{name: "submit provenance revalidation", operation: "submit", callback: "row", sql: "FROM actress_import_name_provenance"},
		{name: "submit guard revalidation", operation: "submit", callback: "row", sql: "FROM actress_sync_name_guards"},
		{name: "submit source lookup", operation: "submit", callback: "query", table: "actress_sync_bindings"},
		{name: "submit binding creation", operation: "submit", callback: "create", table: "actress_sync_bindings"},
		{name: "submit task creation", operation: "submit", callback: "create", table: "actress_sync_tasks"},
		{name: "submit final job capture", operation: "submit", callback: "query", table: "actress_sync_jobs", nth: 2},
		{name: "submit final task capture", operation: "submit", callback: "query", table: "actress_sync_tasks"},
		{name: "duplicate request job read", operation: "duplicate", callback: "query", table: "actress_sync_jobs", nth: 2},
		{name: "duplicate request task read", operation: "duplicate", callback: "query", table: "actress_sync_tasks"},
		{name: "replacement retires prior task", operation: "replace", callback: "update", table: "actress_sync_tasks", after: true},
		{name: "replacement updates binding", operation: "replace", callback: "update", table: "actress_sync_bindings", after: true},
		{name: "replacement creates new task", operation: "replace", callback: "create", table: "actress_sync_tasks"},
		{name: "claim write admission", operation: "claim", callback: "raw", sql: "UPDATE actress_sync_jobs SET cancelled"},
		{name: "claim job read", operation: "claim", callback: "query", table: "actress_sync_jobs"},
		{name: "claim task selection", operation: "claim", callback: "query", table: "actress_sync_tasks"},
		{name: "claim lease write", operation: "claim", callback: "update", table: "actress_sync_tasks", after: true},
		{name: "cancel job write", operation: "cancel", callback: "update", table: "actress_sync_jobs", after: true},
		{name: "cancel task write", operation: "cancel", callback: "update", table: "actress_sync_tasks", after: true},
		{name: "read durable job", operation: "read", callback: "query", table: "actress_sync_jobs"},
		{name: "read durable task receipts", operation: "read", callback: "query", table: "actress_sync_tasks"},
		{name: "apply lease CAS write", operation: "apply", callback: "update", table: "actress_sync_tasks", after: true},
		{name: "apply task read", operation: "apply", callback: "query", table: "actress_sync_tasks"},
		{name: "apply job read", operation: "apply", callback: "query", table: "actress_sync_jobs"},
		{name: "apply binding read", operation: "apply", callback: "query", table: "actress_sync_bindings"},
		{name: "apply actor read", operation: "apply", callback: "query", table: "actresses"},
		{name: "apply provenance read", operation: "apply", callback: "row", sql: "FROM actress_import_name_provenance"},
		{name: "apply guard read", operation: "apply", callback: "row", sql: "FROM actress_sync_name_guards"},
		// Expired real evidence takes the ordinary skipped completion path. These
		// faults must roll back completion without a canonical mutation or fake CAS.
		{name: "skipped binding consumption", operation: "expired", callback: "update", table: "actress_sync_bindings", after: true},
		{name: "skipped receipt write", operation: "expired", callback: "update", table: "actress_sync_tasks", nth: 2, after: true},
		{name: "skipped final receipt read", operation: "expired", callback: "query", table: "actress_sync_tasks", nth: 2},
	}
	for _, fault := range faults {
		t.Run(fault.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			snapshot, options, key := lifecycleSyncEvidence(t)
			actor := lifecycleSyncManaged(t, db, 818, "保存前")
			choices := []ActressSyncChoice{{ActressID: actor.ID, SourceKey: key}}
			review, err := repo.ReviewOfflineNameSync(t.Context(), choices, snapshot, options)
			require.NoError(t, err)
			var job *ActressSyncJob
			var lease *ActressSyncLease
			applyAt := options.Now
			if fault.operation != "review" && fault.operation != "submit" {
				job, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "stored-request", *review, snapshot, options.Now)
				require.NoError(t, err)
			}
			if fault.operation == "expired" {
				applyAt = options.Now.Add(2 * time.Hour)
			}
			if fault.operation == "apply" || fault.operation == "expired" || fault.operation == "cancel" {
				lease, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "storage-worker", applyAt, time.Minute)
				require.NoError(t, err)
			}
			before := previewDatabaseState(t, db)
			injected, remove := injectSyncStorageFault(t, db, fault)
			var result any
			var operationErr error
			switch fault.operation {
			case "review":
				result, operationErr = repo.ReviewOfflineNameSync(t.Context(), choices, snapshot, options)
			case "submit", "replace":
				result, operationErr = repo.SubmitConfirmedOfflineNameSync(t.Context(), "failed-request", *review, snapshot, options.Now)
			case "duplicate":
				result, operationErr = repo.SubmitConfirmedOfflineNameSync(t.Context(), "stored-request", *review, snapshot, options.Now)
			case "claim":
				result, operationErr = repo.ClaimOfflineNameSync(t.Context(), job.ID, "storage-worker", options.Now, time.Minute)
			case "cancel":
				operationErr = repo.CancelOfflineNameSync(t.Context(), job.ID)
			case "read":
				result, operationErr = repo.OfflineNameSyncJob(t.Context(), job.ID)
			case "apply", "expired":
				result, operationErr = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, applyAt)
			default:
				t.Fatalf("unsupported operation %q", fault.operation)
			}
			remove()
			require.ErrorIs(t, operationErr, injected)
			require.Nil(t, result, "storage failures must not expose an uncommitted review, job, lease or receipt")
			require.Equal(t, before, previewDatabaseState(t, db), "storage failures must preserve all actor, job, binding, lease and dependent application state")
		})
	}
}

func TestOfflineNameSyncPublicReviewEvidenceRejectionsAreReadOnly(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 819, "未確認")
	otherSource, _, _ := previewFixture(t)
	record := otherSource.Records()[0]
	otherKey, err := actresscache.SourceKey(record.Source, record.SourceID)
	require.NoError(t, err)
	missingKey, err := actresscache.SourceKey("r18dev", "not-in-snapshot")
	require.NoError(t, err)
	before := previewDatabaseState(t, db)
	for _, tc := range []struct {
		name     string
		snapshot *actresscache.Snapshot
		key      string
		now      time.Time
	}{
		{"nil snapshot", nil, key, options.Now},
		{"empty snapshot", &actresscache.Snapshot{}, key, options.Now},
		{"missing evidence", snapshot, missingKey, options.Now},
		{"unsupported evidence source", otherSource, otherKey, options.Now},
		{"future evidence", snapshot, key, options.Now.Add(-time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestOptions := options
			requestOptions.Now = tc.now
			review, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: actor.ID, SourceKey: tc.key}}, tc.snapshot, requestOptions)
			require.ErrorIs(t, err, ErrActressSyncStale)
			require.Nil(t, review)
			require.Equal(t, before, previewDatabaseState(t, db))
		})
	}
}

func TestOfflineNameSyncPublicSelectionAndHashRejectionsAreReadOnly(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 820, "選択前")
	validChoice := ActressSyncChoice{ActressID: actor.ID, SourceKey: key}
	oversized := make([]ActressSyncChoice, actresssync.MaxSelection+1)
	for i := range oversized {
		oversized[i] = ActressSyncChoice{ActressID: uint(10000 + i), SourceKey: fmt.Sprintf("distinct-source-%d", i)}
	}
	before := previewDatabaseState(t, db)
	for _, choices := range [][]ActressSyncChoice{
		nil,
		{{ActressID: 0, SourceKey: key}},
		{validChoice, {ActressID: actor.ID, SourceKey: key + "-different"}},
		{validChoice, {ActressID: actor.ID + 1, SourceKey: key}},
		oversized,
	} {
		review, err := repo.ReviewOfflineNameSync(t.Context(), choices, snapshot, options)
		require.Error(t, err)
		require.Nil(t, review)
		require.Equal(t, before, previewDatabaseState(t, db))
	}
	review, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{validChoice}, snapshot, options)
	require.NoError(t, err)
	for _, field := range []string{"snapshot digest", "actor token"} {
		t.Run(field, func(t *testing.T) {
			short := *review
			short.Proposals = append([]ActressSyncProposal(nil), review.Proposals...)
			if field == "snapshot digest" {
				short.SnapshotDigest = "abc"
			} else {
				short.Proposals[0].ActorToken = "abc"
			}
			job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "short-hash", short, snapshot, options.Now)
			require.ErrorIs(t, err, ErrActressSyncConflict)
			require.Nil(t, job)
			require.Equal(t, before, previewDatabaseState(t, db))
		})
	}
}

func TestOfflineNameSyncPublicLeaseAndMissingIdentityRejectionsAreReadOnly(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 821, "境界前")
	_, job, lease := lifecycleSyncConfirm(t, repo, actor.ID, key, "boundary-job", snapshot, options)
	before := previewDatabaseState(t, db)
	for _, invalid := range []ActressSyncLease{
		{},
		{TaskID: lease.TaskID, Generation: 0, Owner: lease.Owner},
		{TaskID: lease.TaskID, Generation: lease.Generation, Owner: ""},
		{TaskID: lease.TaskID, Generation: lease.Generation, Owner: strings.Repeat("x", 129)},
	} {
		receipt, err := repo.ApplyOfflineNameSync(t.Context(), invalid, snapshot, options.Now)
		require.ErrorIs(t, err, ErrActressSyncLease)
		require.Nil(t, receipt)
		require.Equal(t, before, previewDatabaseState(t, db))
	}
	require.ErrorIs(t, repo.ProtectOfflineSyncName(t.Context(), 0), ErrActressSyncProtected)
	require.Equal(t, before, previewDatabaseState(t, db))
	require.ErrorIs(t, repo.ProtectOfflineSyncName(t.Context(), actor.ID+10000), gorm.ErrRecordNotFound)
	require.Equal(t, before, previewDatabaseState(t, db))
	require.ErrorIs(t, repo.CancelOfflineNameSync(t.Context(), "missing-job"), gorm.ErrRecordNotFound)
	require.Equal(t, before, previewDatabaseState(t, db))
	require.NoError(t, repo.CancelOfflineNameSync(t.Context(), job.ID))
	cancelled := previewDatabaseState(t, db)
	newLease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "cancelled-worker", options.Now, time.Minute)
	require.ErrorIs(t, err, ErrActressSyncLease)
	require.Nil(t, newLease)
	require.Equal(t, cancelled, previewDatabaseState(t, db))
}
