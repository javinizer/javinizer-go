package database

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Seed dependent data so fail-closed assertions protect actual overrides,
// suppression, translations and render state, not just empty tables.
func syncIntegrityDependents(t *testing.T, db *DB, actorID uint) {
	t.Helper()
	require.NoError(t, db.Create(&models.Movie{ContentID: "integrity-film", ID: "integrity-film", RenderGeneration: 17}).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: "integrity-film", ActressID: actorID, CreditedName: "Preserved credit", Origin: string(models.CreditOriginUser), UserOverride: true, OverrideName: "Manual override", Suppressed: true}).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actorID, Language: "en", DisplayName: "Preserved translation"}).Error)
	var actor models.Actress
	require.NoError(t, db.First(&actor, actorID).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Curated integrity alias", CanonicalName: canonicalActressName(&actor)}).Error)
	// A separate live grant must survive every selected-actor failure unchanged.
	other := lifecycleSyncManaged(t, db, 909, "別の旧名")
	otherSnapshot, options, key := lifecycleSyncEvidence(t)
	_, _, _ = lifecycleSyncConfirm(t, NewActressRepository(db), other.ID, key, "unrelated-integrity-job", otherSnapshot, options)
	require.NoError(t, db.Create(&models.Movie{ContentID: "unrelated-film", ID: "unrelated-film", RenderGeneration: 23}).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: "unrelated-film", ActressID: other.ID, CreditedName: "Unrelated credit", Origin: string(models.CreditOriginUser), UserOverride: true, OverrideName: "Unrelated override", Suppressed: true}).Error)

}

func TestOfflineNameSyncIntegrityGenerationExhaustion(t *testing.T) {
	for _, target := range []string{"lease", "binding"} {
		t.Run(target, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			syncIntegrityDependents(t, db, review.Proposals[0].ActressID)
			job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "exhaustion", review, snapshot, now)
			require.NoError(t, err)
			if target == "lease" {
				require.NoError(t, db.Model(&ActressSyncTask{}).Where("id = ?", job.Tasks[0].ID).Update("lease_generation", int64(math.MaxInt64)).Error)
			} else {
				require.NoError(t, db.Model(&actressSyncBinding{}).Where("id = ?", job.Tasks[0].BindingID).Update("revision", int64(math.MaxInt64)).Error)
			}
			before := previewDatabaseState(t, db)
			if target == "lease" {
				lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
				require.ErrorIs(t, err, ErrActressSyncLease)
				require.Nil(t, lease)
			} else {
				replacement, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "exhausted-replacement", review, snapshot, now)
				require.ErrorIs(t, err, ErrActressSyncConflict)
				require.Nil(t, replacement)
			}
			require.Equal(t, before, previewDatabaseState(t, db), "overflow must neither wrap a fence nor retire outstanding grants")
		})
	}
}

// Durable records may be damaged or independently maintained. Revalidation
// must reject them even when the public writer would never create this state.
func TestOfflineNameSyncIntegrityBindingRevalidation(t *testing.T) {
	cases := []struct {
		name, column string
		value        any
	}{
		{"missing", "", nil}, {"status", "status", "revoked"},
		{"revision", "revision", int64(2)}, {"actor", "actress_id", uint(999999)},
		{"source", "source_key", "different-source"}, {"digest", "snapshot_digest", strings.Repeat("f", 64)},
		{"token", "actor_token", strings.Repeat("e", 64)}, {"name", "japanese_name", "別名"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			syncIntegrityDependents(t, db, review.Proposals[0].ActressID)
			job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "binding-integrity", review, snapshot, now)
			require.NoError(t, err)
			lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
			require.NoError(t, err)
			id := job.Tasks[0].BindingID
			if tc.name == "missing" {
				require.NoError(t, db.Delete(&actressSyncBinding{}, id).Error)
			} else {
				require.NoError(t, db.Model(&actressSyncBinding{}).Where("id = ?", id).Update(tc.column, tc.value).Error)
			}
			before := previewDatabaseState(t, db)
			receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
			require.NoError(t, err)
			require.Equal(t, "invalidated", receipt.Status)
			reason := "binding_changed"
			if tc.name == "missing" {
				reason = "binding_missing"
			}
			require.Equal(t, reason, receipt.Reason)
			require.NotNil(t, receipt.FinishedAt)
			after := previewDatabaseState(t, db)
			syncIntegrityOmitRow(before, "actress_sync_tasks", lease.TaskID)
			syncIntegrityOmitRow(after, "actress_sync_tasks", lease.TaskID)
			if tc.name != "revision" {
				syncIntegrityOmitRow(before, "actress_sync_bindings", id)
				syncIntegrityOmitRow(after, "actress_sync_bindings", id)
			}
			require.Equal(t, before, after, "only own task and matching binding may change; never consume a newer grant")
			if tc.name != "missing" {
				var binding actressSyncBinding
				require.NoError(t, db.First(&binding, id).Error)
				expected := "revoked"
				if tc.name == "revision" {
					expected = "active"
				}
				require.Equal(t, expected, binding.Status)
			}
			_, err = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
			require.ErrorIs(t, err, ErrActressSyncLease)
		})
	}
}

func TestOfflineNameSyncIntegrityEvidenceNameMismatchRollsBack(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	syncIntegrityDependents(t, db, review.Proposals[0].ActressID)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "evidence-integrity", review, snapshot, now)
	require.NoError(t, err)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
	require.NoError(t, err)
	// Matching task/binding text is not sufficient authority: the immutable
	// snapshot must independently agree with the proposed canonical value.
	require.NoError(t, db.Model(&ActressSyncTask{}).Where("id = ?", lease.TaskID).Update("after_name", "Not in evidence").Error)
	require.NoError(t, db.Model(&actressSyncBinding{}).Where("id = ?", job.Tasks[0].BindingID).Update("japanese_name", "Not in evidence").Error)
	before := previewDatabaseState(t, db)
	result, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
	require.ErrorIs(t, err, ErrActressSyncStale)
	require.Nil(t, result)
	require.Equal(t, before, previewDatabaseState(t, db))
}

func TestOfflineNameSyncIntegrityEvidenceBounds(t *testing.T) {
	_, _, snapshot, review, now, _ := durableNameFixture(t)
	key := review.Proposals[0].SourceKey
	for _, tc := range []struct {
		name, key string
		at        time.Time
		age       time.Duration
	}{
		{"oversized key", strings.Repeat("x", 513), now, time.Hour},
		{"zero clock", key, time.Time{}, time.Hour},
		{"zero age", key, now, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence, err := syncNameEvidence(snapshot, tc.key, tc.at, tc.age)
			require.ErrorIs(t, err, ErrActressSyncStale)
			require.Equal(t, actresscache.Evidence{}, evidence)
		})
	}
}

func TestOfflineNameSyncIntegrityOversizedDurableTaskList(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "oversized-history", review, snapshot, now)
	require.NoError(t, err)
	// Historical/corrupt storage is independently bounded even if current public
	// submission always limits its writes. Keep all real SQLite constraints active.
	tasks := make([]ActressSyncTask, actresssync.MaxSelection)
	for i := range tasks {
		tasks[i] = job.Tasks[0]
		tasks[i].ID = 0
		tasks[i].ActressID = uint(10000 + i)
	}
	require.NoError(t, db.CreateInBatches(tasks, 50).Error)
	before := previewDatabaseState(t, db)
	result, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.ErrorIs(t, err, ErrActressSyncConflict)
	require.Nil(t, result)
	require.ErrorIs(t, repo.RecordOfflineNameSyncJobProblem(t.Context(), job.ID, ActressSyncProblemSnapshotMissing, now), ErrActressSyncConflict)
	require.Equal(t, before, previewDatabaseState(t, db))
}

func TestOfflineNameSyncIntegrityClaimCASRejectsIgnoredWrite(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "claim-cas", review, snapshot, now)
	require.NoError(t, err)
	// A real SQLite trigger can reject a write without returning a SQL error.
	// Never fabricate RowsAffected: the CAS must observe SQLite's actual zero.
	require.NoError(t, db.Exec(`CREATE TRIGGER test_ignore_lease BEFORE UPDATE OF lease_generation ON actress_sync_tasks BEGIN SELECT RAISE(IGNORE); END`).Error)
	before := previewDatabaseState(t, db)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
	require.ErrorIs(t, err, ErrActressSyncLease)
	require.Nil(t, lease)
	require.Equal(t, before, previewDatabaseState(t, db))
	require.NoError(t, db.Exec("DROP TRIGGER test_ignore_lease").Error)
	lease, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, lease.Generation)
}

func TestOfflineNameSyncIntegrityFinishCASRollsBackBinding(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "finish-cas", review, snapshot, now)
	require.NoError(t, err)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
	require.NoError(t, err)
	before := previewDatabaseState(t, db)
	// Completion cannot use a leased task without admission to applying, even
	// with an otherwise correct token; binding retirement must roll back too.
	err = db.Transaction(func(tx *gorm.DB) error {
		return finishOfflineNameTaskTx(tx, job.Tasks[0], *lease, "applied", "", "consumed", now)
	})
	require.ErrorIs(t, err, ErrActressSyncLease)
	require.Equal(t, before, previewDatabaseState(t, db))
}

func TestOfflineNameSyncIntegrityUnavailableIdentity(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "protected"
		if missing {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			syncIntegrityDependents(t, db, review.Proposals[0].ActressID)
			job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "unavailable", review, snapshot, now)
			require.NoError(t, err)
			lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
			require.NoError(t, err)
			if missing {
				// A dangling historical reference must not authorize mutation of another
				// live identity; retain the original actor and its dependent state.
				require.NoError(t, db.Model(&ActressSyncTask{}).Where("id = ?", lease.TaskID).Update("actress_id", 999999).Error)
				require.NoError(t, db.Model(&actressSyncBinding{}).Where("id = ?", job.Tasks[0].BindingID).Update("actress_id", 999999).Error)
			} else {
				require.NoError(t, db.Exec("INSERT INTO actress_sync_name_guards (actress_id, protected) VALUES (?, 1) ON CONFLICT(actress_id) DO UPDATE SET protected = 1", review.Proposals[0].ActressID).Error)
			}
			before := previewDatabaseState(t, db)
			receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
			require.NoError(t, err)
			require.Equal(t, "invalidated", receipt.Status)
			require.Equal(t, "identity_unavailable", receipt.Reason)
			after := previewDatabaseState(t, db)
			syncIntegrityOmitRow(before, "actress_sync_tasks", lease.TaskID)
			syncIntegrityOmitRow(after, "actress_sync_tasks", lease.TaskID)
			syncIntegrityOmitRow(before, "actress_sync_bindings", job.Tasks[0].BindingID)
			syncIntegrityOmitRow(after, "actress_sync_bindings", job.Tasks[0].BindingID)
			require.Equal(t, before, after)
		})
	}
}

func TestOfflineNameSyncIntegrityCancellationAfterWrites(t *testing.T) {
	for _, stage := range []string{"submit", "claim", "finish"} {
		t.Run(stage, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			syncIntegrityDependents(t, db, review.Proposals[0].ActressID)
			var job *ActressSyncJob
			var lease *ActressSyncLease
			var err error
			if stage != "submit" {
				job, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "cancel-writes", review, snapshot, now)
				require.NoError(t, err)
			}
			if stage == "finish" {
				lease, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
				require.NoError(t, err)
			}
			before := previewDatabaseState(t, db)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reached := 0
			callback := "test:sync_cancel_after_write"
			hook := func(tx *gorm.DB) {
				if tx.Statement.Table != "actress_sync_tasks" || tx.Error != nil || tx.RowsAffected != 1 {
					return
				}
				if stage == "finish" && !strings.Contains(tx.Statement.SQL.String(), "finished_at") {
					return
				}
				reached++
				cancel()
			}
			var remove func(string) error
			if stage == "submit" {
				require.NoError(t, db.Callback().Create().After("gorm:create").Register(callback, hook))
				remove = db.Callback().Create().Remove
			} else {
				require.NoError(t, db.Callback().Update().After("gorm:update").Register(callback, hook))
				remove = db.Callback().Update().Remove
			}
			var result any
			switch stage {
			case "submit":
				result, err = repo.SubmitConfirmedOfflineNameSync(ctx, "cancel-writes", review, snapshot, now)
			case "claim":
				result, err = repo.ClaimOfflineNameSync(ctx, job.ID, "worker", now, time.Minute)
			case "finish":
				result, err = repo.ApplyOfflineNameSync(ctx, *lease, snapshot, now)
			}
			require.NoError(t, remove(callback))
			require.Equal(t, 1, reached, "cancel only after SQLite actually wrote the target row")
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, result)
			require.Equal(t, before, previewDatabaseState(t, db), "cancellation rolls back canonical writes, receipt, binding and lease together")
		})
	}
}

func TestOfflineNameSyncIntegrityCanonicalPostconditionsRollback(t *testing.T) {
	for _, stage := range []string{"changed identity", "missing protection write", "final identity read"} {
		t.Run(stage, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			actorID := review.Proposals[0].ActressID
			syncIntegrityDependents(t, db, actorID)
			job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "canonical-postcondition", review, snapshot, now)
			require.NoError(t, err)
			lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
			require.NoError(t, err)
			before := previewDatabaseState(t, db)
			switch stage {
			case "changed identity":
				actor, err := repo.FindByID(t.Context(), actorID)
				require.NoError(t, err)
				actor.DMMID++ // The mutation boundary must independently verify identity.
				err = db.Transaction(func(tx *gorm.DB) error { return applyConfirmedJapaneseNameTx(tx, actor, "新名") })
				require.ErrorIs(t, err, ErrActressSyncProtected)
			case "missing protection write":
				require.NoError(t, db.Exec(`CREATE TRIGGER test_ignore_protection BEFORE UPDATE OF protected ON actress_sync_name_guards WHEN NEW.protected = 0 BEGIN SELECT RAISE(IGNORE); END`).Error)
				receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
				require.ErrorContains(t, err, "sync name protection state disappeared")
				require.Nil(t, receipt)
				require.NoError(t, db.Exec("DROP TRIGGER test_ignore_protection").Error)
			case "final identity read":
				// Mark the real completed quarantine reconciliation, then fail the next
				// actor read rather than depending on incidental preceding query counts.
				reconciled, reached := false, false
				callback := "test:sync_final_identity_read"
				require.NoError(t, db.Callback().Raw().After("gorm:raw").Register(callback, func(tx *gorm.DB) {
					if tx.Error == nil && strings.HasPrefix(tx.Statement.SQL.String(), "UPDATE actress_quarantine_state SET consumed_revision") {
						reconciled = true
					}
				}))
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
					if reconciled && tx.Statement.Table == "actresses" {
						reached = true
						_ = tx.AddError(gorm.ErrInvalidDB)
					}
				}))
				receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
				require.NoError(t, db.Callback().Raw().Remove(callback))
				require.NoError(t, db.Callback().Query().Remove(callback))
				require.True(t, reconciled)
				require.True(t, reached)
				require.ErrorIs(t, err, gorm.ErrInvalidDB)
				require.Nil(t, receipt)
			}
			require.Equal(t, before, previewDatabaseState(t, db), "postcondition failure rolls back canonical name, aliases, credits, ownership and render state")
		})
	}
}

func TestOfflineNameSyncIntegritySubmissionSnapshotMismatch(t *testing.T) {
	db, repo, _, review, now, _ := durableNameFixture(t)
	syncIntegrityDependents(t, db, review.Proposals[0].ActressID)
	other, _, _ := previewFixture(t)
	before := previewDatabaseState(t, db)
	for _, snapshot := range []*actresscache.Snapshot{nil, other} {
		job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "snapshot-mismatch", review, snapshot, now)
		require.ErrorIs(t, err, ErrActressSyncStale)
		require.Nil(t, job)
		require.Equal(t, before, previewDatabaseState(t, db), "stale snapshot cannot create a job or retire any grant")
	}
}

// Exclude only the authorized receipt/grant row, retaining all other records
// in the same tables so broad accidental UPDATEs cannot hide in the assertion.
func syncIntegrityOmitRow(state map[string][]map[string]interface{}, table string, id uint) {
	rows := state[table]
	filtered := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		if fmt.Sprint(row["id"]) != fmt.Sprint(id) {
			filtered = append(filtered, row)
		}
	}
	state[table] = filtered
}
