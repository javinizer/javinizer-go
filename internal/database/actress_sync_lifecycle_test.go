package database

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func lifecycleSyncEvidence(t *testing.T) (*actresscache.Snapshot, actresssync.Options, string) {
	t.Helper()
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	limits := actresscache.Limits{MaxBytes: 1 << 20, MaxRecordBytes: 1 << 16, MaxRecords: 10, MaxStringBytes: 4096, MaxTotalStringBytes: 1 << 18, MaxAliases: 32, MaxExternalIDs: 16, MaxTokensPerRecord: 1024}
	dump := "COPY public.derived_actress (id, name_kanji) FROM stdin;\n7001\t新しい名前\n\\.\n"
	snapshot, _, err := r18dump.Convert(t.Context(), strings.NewReader(dump), actresscache.Provenance{SourceURL: "https://example.invalid/fixture.sql", ParserRevision: "lifecycle-fixture-v1", ObservedAt: now}, r18dump.Limits{MaxInputBytes: 4096, MaxLineBytes: 1024, MaxRows: 10, Output: limits})
	require.NoError(t, err)
	records := snapshot.Records()
	require.Len(t, records, 1)
	key, err := actresscache.SourceKey(records[0].Source, records[0].SourceID)
	require.NoError(t, err)
	return snapshot, actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 10}, key
}

func lifecycleSyncManaged(t *testing.T, db *DB, id int, japaneseName string) models.Actress {
	t.Helper()
	actor := models.Actress{DMMID: id, FirstName: fmt.Sprintf("Managed%d", id), JapaneseName: japaneseName}
	require.NoError(t, NewActressRepository(db).ImportUpsert(t.Context(), &actor))
	return actor
}

func lifecycleSyncConfirm(t *testing.T, repo *ActressRepository, id uint, key, request string, snapshot *actresscache.Snapshot, options actresssync.Options) (*ActressSyncReview, *ActressSyncJob, *ActressSyncLease) {
	t.Helper()
	review, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: id, SourceKey: key}}, snapshot, options)
	require.NoError(t, err)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), request, *review, snapshot, options.Now)
	require.NoError(t, err)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "lifecycle-worker", options.Now, time.Minute)
	require.NoError(t, err)
	return review, job, lease
}

func lifecycleSyncAssertRetired(t *testing.T, db *DB, jobID string, actorID uint) {
	t.Helper()
	job, err := NewActressRepository(db).OfflineNameSyncJob(t.Context(), jobID)
	require.NoError(t, err)
	require.Len(t, job.Tasks, 1)
	require.Equal(t, "invalidated", job.Tasks[0].Status)
	require.NotEmpty(t, job.Tasks[0].Reason)
	var binding actressSyncBinding
	require.NoError(t, db.First(&binding, job.Tasks[0].BindingID).Error)
	require.Equal(t, actorID, binding.ActressID, "historical binding must not transfer to another identity")
	require.Equal(t, "revoked", binding.Status)
	var protected bool
	require.NoError(t, db.Raw("SELECT protected FROM actress_sync_name_guards WHERE actress_id = ?", actorID).Scan(&protected).Error)
	require.True(t, protected, "manual intent/deletion must outlive import provenance and the actor row")
}

func lifecycleSyncOpen(t *testing.T, path string) *DB {
	t.Helper()
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	return db
}

func TestOfflineNameSyncLifecycleReplacementFencesEveryOldClaim(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 801, "旧名")
	review, job, first := lifecycleSyncConfirm(t, repo, actor.ID, key, "first-confirmation", snapshot, options)
	_, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "premature-worker", options.Now.Add(time.Second), time.Minute)
	require.Error(t, err)
	second, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "replacement-worker", options.Now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.Equal(t, first.TaskID, second.TaskID)
	require.Greater(t, second.Generation, first.Generation)
	before := previewDatabaseState(t, db)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *first, snapshot, options.Now.Add(time.Minute))
	require.ErrorIs(t, err, ErrActressSyncLease)
	wrongOwner := *second
	wrongOwner.Owner = first.Owner
	_, err = repo.ApplyOfflineNameSync(t.Context(), wrongOwner, snapshot, options.Now.Add(time.Minute))
	require.ErrorIs(t, err, ErrActressSyncLease)
	require.Equal(t, before, previewDatabaseState(t, db))

	// Reconfirming the same source is an explicit new one-shot authority, not
	// permission for either worker holding an older task to apply it.
	replacement, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "second-confirmation", *review, snapshot, options.Now.Add(time.Minute))
	require.NoError(t, err)
	require.NotEqual(t, job.ID, replacement.ID)
	retired, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "invalidated", retired.Tasks[0].Status)
	require.Equal(t, "confirmation_replaced", retired.Tasks[0].Reason)
	require.Equal(t, job.Tasks[0].BindingID, replacement.Tasks[0].BindingID)
	require.Greater(t, replacement.Tasks[0].BindingRevision, job.Tasks[0].BindingRevision)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *second, snapshot, options.Now.Add(time.Minute))
	require.ErrorIs(t, err, ErrActressSyncLease)
	newLease, err := repo.ClaimOfflineNameSync(t.Context(), replacement.ID, "current-worker", options.Now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	receipt, err := repo.ApplyOfflineNameSync(t.Context(), *newLease, snapshot, options.Now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "applied", receipt.Status)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *newLease, snapshot, options.Now.Add(time.Minute))
	require.ErrorIs(t, err, ErrActressSyncLease, "the successful claim must not grant a second update")
}

func TestOfflineNameSyncLifecycleManualIntentCannotBeRegrantedByImport(t *testing.T) {
	for _, method := range []string{"protect", "same-value update", "same-value rename names", "same-value rename identity", "same-value clear", "clear existing", "canonical same-value"} {
		t.Run(method, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			snapshot, options, key := lifecycleSyncEvidence(t)
			name := "旧名"
			if method == "same-value clear" {
				name = ""
			}
			actor := lifecycleSyncManaged(t, db, 802, name)
			review, job, lease := lifecycleSyncConfirm(t, repo, actor.ID, key, "manual-intent", snapshot, options)
			switch method {
			case "protect":
				require.NoError(t, repo.ProtectOfflineSyncName(t.Context(), actor.ID))
			case "same-value update":
				require.NoError(t, repo.Update(t.Context(), &actor))
			case "same-value rename names", "same-value clear":
				require.NoError(t, repo.RenameNameFields(t.Context(), actor.ID, actor.FirstName, actor.LastName, actor.JapaneseName))
			case "same-value rename identity":
				require.NoError(t, repo.RenameIdentityFields(t.Context(), actor.ID, actor.FirstName, actor.LastName, actor.JapaneseName, actor.ThumbURL))
			case "clear existing":
				name = ""
				require.NoError(t, repo.RenameNameFields(t.Context(), actor.ID, actor.FirstName, actor.LastName, ""))
			case "canonical same-value":
				require.NoError(t, repo.UpdateCanonicalFields(t.Context(), actor.ID, actor.FirstName, actor.LastName, actor.JapaneseName, actor.ThumbURL))
			}
			lifecycleSyncAssertRetired(t, db, job.ID, actor.ID)
			got, err := repo.FindByID(t.Context(), actor.ID)
			require.NoError(t, err)
			require.Equal(t, name, got.JapaneseName)
			// Ordinary import management and explicit sync protection are independent.
			// Even the import path that restores its transaction-local managed bit
			// must never restore the sync guard or the prior confirmation.
			incoming := models.Actress{ID: actor.ID, DMMID: actor.DMMID, FirstName: actor.FirstName, JapaneseName: "再輸入"}
			_, err = repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true, OverwriteLegacyNames: true})
			require.NoError(t, err)
			if method != "clear existing" && method != "canonical same-value" {
				require.Equal(t, importNamesManaged, lifecycleSyncProvenance(t, db, actor.ID))
			}
			lifecycleSyncAssertRetired(t, db, job.ID, actor.ID)
			_, err = repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: actor.ID, SourceKey: key}}, snapshot, options)
			require.ErrorIs(t, err, ErrActressSyncProtected)
			_, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "cannot-regrant", *review, snapshot, options.Now)
			require.ErrorIs(t, err, ErrActressSyncProtected)
			_, err = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, options.Now)
			require.ErrorIs(t, err, ErrActressSyncLease)
		})
	}
}

func lifecycleSyncProvenance(t *testing.T, db *DB, actorID uint) int {
	t.Helper()
	state, err := importNameProvenanceTx(db.DB, actorID)
	require.NoError(t, err)
	return state
}

func TestOfflineNameSyncLifecycleProtectionSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual-protection.db")
	db := lifecycleSyncOpen(t, path)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 803, "")
	_, job, lease := lifecycleSyncConfirm(t, repo, actor.ID, key, "protected-across-restart", snapshot, options)
	require.NoError(t, repo.ProtectOfflineSyncName(t.Context(), actor.ID))
	require.NoError(t, db.Close())
	db = lifecycleSyncOpen(t, path)
	repo = NewActressRepository(db)
	incoming := models.Actress{ID: actor.ID, FirstName: actor.FirstName, JapaneseName: "再輸入"}
	_, err := repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, importNamesManaged, lifecycleSyncProvenance(t, db, actor.ID))
	lifecycleSyncAssertRetired(t, db, job.ID, actor.ID)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, options.Now)
	require.ErrorIs(t, err, ErrActressSyncLease)
	_, err = repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: actor.ID, SourceKey: key}}, snapshot, options)
	require.ErrorIs(t, err, ErrActressSyncProtected)
}

func TestOfflineNameSyncLifecycleIneligibleFieldsCannotBeConfirmed(t *testing.T) {
	for _, state := range []string{"user", "legacy import", "revoked import", "unverified", "quarantined"} {
		t.Run(state, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			snapshot, options, key := lifecycleSyncEvidence(t)
			actor := models.Actress{DMMID: 804, FirstName: "Protected", JapaneseName: "", Origin: ActressOriginImport, Verified: true}
			switch state {
			case "user":
				actor.Origin = ActressOriginUser
				require.NoError(t, repo.Create(t.Context(), &actor))
			case "legacy import":
				require.NoError(t, db.Create(&actor).Error)
			default:
				require.NoError(t, repo.ImportUpsert(t.Context(), &actor))
				switch state {
				case "revoked import":
					require.NoError(t, db.Model(&actor).Update("first_name", "Known manual edit").Error)
				case "unverified":
					require.NoError(t, db.Model(&actor).Update("verified", false).Error)
				case "quarantined":
					require.NoError(t, db.Model(&actor).Update("ambiguity_quarantined", true).Error)
				}
			}
			require.NoError(t, db.First(&actor, actor.ID).Error)
			before := previewDatabaseState(t, db)
			_, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: actor.ID, SourceKey: key}}, snapshot, options)
			require.ErrorIs(t, err, ErrActressSyncProtected)
			// A caller cannot bypass review eligibility by constructing a matching
			// token and explicitly confirming an empty protected field.
			forged := ActressSyncReview{SnapshotDigest: snapshot.Digest(), MaxAge: options.MaxAge, Proposals: []ActressSyncProposal{{ActressID: actor.ID, SourceKey: key, ActorToken: syncActorToken(&actor), BeforeName: "", AfterName: "新しい名前"}}}
			_, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "forged-review", forged, snapshot, options.Now)
			require.ErrorIs(t, err, ErrActressSyncProtected)
			require.Equal(t, before, previewDatabaseState(t, db), "rejected eligibility must not create jobs, bindings or grants")
		})
	}
}

func TestOfflineNameSyncLifecycleMergeFencesUnchangedTargetAndDeletedSource(t *testing.T) {
	for _, boundRole := range []string{"target", "source"} {
		t.Run(boundRole, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			snapshot, options, key := lifecycleSyncEvidence(t)
			managed := lifecycleSyncManaged(t, db, 805, "同じ名前")
			other := models.Actress{DMMID: 806, FirstName: managed.FirstName, JapaneseName: managed.JapaneseName, Origin: ActressOriginUser, Verified: true}
			require.NoError(t, repo.Create(t.Context(), &other))
			_, job, lease := lifecycleSyncConfirm(t, repo, managed.ID, key, "before-merge", snapshot, options)
			target, source := managed, other
			if boundRole == "source" {
				target, source = other, managed
			}
			result, err := repo.Merge(t.Context(), target.ID, source.ID, map[string]string{"dmm_id": "target"})
			require.NoError(t, err)
			require.Equal(t, target.JapaneseName, result.MergedActress.JapaneseName)
			require.Equal(t, target.FirstName, result.MergedActress.FirstName)
			lifecycleSyncAssertRetired(t, db, job.ID, managed.ID)
			var count int64
			require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", source.ID).Count(&count).Error)
			require.Zero(t, count)
			_, err = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, options.Now)
			require.ErrorIs(t, err, ErrActressSyncLease)
			_, err = repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: target.ID, SourceKey: key}}, snapshot, options)
			require.ErrorIs(t, err, ErrActressSyncProtected)
		})
	}
}

func TestOfflineNameSyncLifecycleRawIdentityReplacementFencesVictims(t *testing.T) {
	for _, operation := range []string{"delete and reinsert", "insert replace id", "insert replace dmm", "update replace id", "update replace dmm"} {
		t.Run(operation, func(t *testing.T) {
			db := newCreditTestDB(t)
			sqlDB, err := db.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			sqlDB.SetMaxIdleConns(1)
			for _, pragma := range []string{"foreign_keys", "recursive_triggers"} {
				require.NoError(t, db.Exec("PRAGMA "+pragma+" = OFF").Error)
				var enabled int
				require.NoError(t, db.Raw("PRAGMA "+pragma).Scan(&enabled).Error)
				require.Zero(t, enabled)
			}
			repo := NewActressRepository(db)
			snapshot, options, key := lifecycleSyncEvidence(t)
			victim := lifecycleSyncManaged(t, db, 807, "置換前")
			survivor := lifecycleSyncManaged(t, db, 808, "別の人物")
			_, job, lease := lifecycleSyncConfirm(t, repo, victim.ID, key, "replace-victim", snapshot, options)
			switch operation {
			case "delete and reinsert":
				require.NoError(t, db.Exec("DELETE FROM actresses WHERE id = ?", victim.ID).Error)
				// Reinsert every original scalar, including timestamps. Comparing the
				// actor token alone must not resurrect a deleted identity's authority.
				require.NoError(t, db.Create(&victim).Error)
			case "insert replace id", "insert replace dmm":
				id := victim.ID
				if operation == "insert replace dmm" {
					id = survivor.ID + 100
				}
				require.NoError(t, db.Exec("INSERT OR REPLACE INTO actresses (id, dmm_id, first_name, last_name, japanese_name, thumb_url, aliases, verified, origin, name_key, ambiguity_quarantined, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, victim.DMMID, victim.FirstName, victim.LastName, victim.JapaneseName, victim.ThumbURL, victim.Aliases, victim.Verified, victim.Origin, victim.NameKey, victim.AmbiguityQuarantined, victim.CreatedAt, victim.UpdatedAt).Error)
			case "update replace id":
				require.NoError(t, db.Exec("UPDATE OR REPLACE actresses SET id = ? WHERE id = ?", victim.ID, survivor.ID).Error)
			case "update replace dmm":
				require.NoError(t, db.Exec("UPDATE OR REPLACE actresses SET dmm_id = ? WHERE id = ?", victim.DMMID, survivor.ID).Error)
			}
			lifecycleSyncAssertRetired(t, db, job.ID, victim.ID)
			before := previewDatabaseState(t, db)
			_, err = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, options.Now)
			require.ErrorIs(t, err, ErrActressSyncLease)
			require.Equal(t, before, previewDatabaseState(t, db))
			if operation == "delete and reinsert" || operation == "insert replace id" || operation == "update replace id" {
				_, err = repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: victim.ID, SourceKey: key}}, snapshot, options)
				require.ErrorIs(t, err, ErrActressSyncProtected)
			}
		})
	}
}

func TestOfflineNameSyncLifecycleAliasConflictRollsBackEntireApply(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 809, "共有の旧名")
	other := models.Actress{DMMID: 810, JapaneseName: actor.JapaneseName, Origin: ActressOriginUser, Verified: true}
	require.NoError(t, repo.Create(t.Context(), &other))
	alias := models.ActressAlias{AliasName: "Accepted stage name", CanonicalName: actor.JapaneseName}
	require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &alias))
	translation := models.ActressTranslation{ActressID: actor.ID, Language: "en", DisplayName: "Preserve this translation", SourceName: actor.JapaneseName}
	require.NoError(t, db.Create(&translation).Error)
	movie := models.Movie{ContentID: "sync-alias-rollback", ID: "sync-alias-rollback", RenderGeneration: 11}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actor.ID, CreditedName: actor.FullName(), Origin: string(models.CreditOriginUser), Suppressed: true, UserOverride: true, OverrideName: "Pinned display"}
	require.NoError(t, db.Create(&credit).Error)
	_, job, lease := lifecycleSyncConfirm(t, repo, actor.ID, key, "alias-conflict", snapshot, options)
	before := previewDatabaseState(t, db)
	_, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, options.Now)
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
	require.Equal(t, before, previewDatabaseState(t, db), "name, provenance, guard, translation, credit, alias, render and lease changes must roll back together")
	persisted, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "leased", persisted.Tasks[0].Status)
	require.Nil(t, persisted.Tasks[0].FinishedAt)
}

func TestOfflineNameSyncLifecycleCancellationAfterWriteSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel-apply.db")
	db := lifecycleSyncOpen(t, path)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 811, "キャンセル前")
	_, job, lease := lifecycleSyncConfirm(t, repo, actor.ID, key, "cancel-after-write", snapshot, options)
	before := previewDatabaseState(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cancelledAfterWrite := false
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:lifecycle_cancel_name_write", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "actresses" && tx.Error == nil && tx.RowsAffected == 1 {
			cancelledAfterWrite = true
			cancel()
		}
	}))
	_, err := repo.ApplyOfflineNameSync(ctx, *lease, snapshot, options.Now)
	require.Error(t, err)
	require.True(t, cancelledAfterWrite, "fixture must cancel after the canonical UPDATE, not merely before transaction admission")
	require.NoError(t, db.Callback().Update().Remove("test:lifecycle_cancel_name_write"))
	require.NoError(t, db.Close())
	db = lifecycleSyncOpen(t, path)
	repo = NewActressRepository(db)
	require.Equal(t, before, previewDatabaseState(t, db), "reopen must not expose a changed identity or an independently committed applying state")
	persisted, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "leased", persisted.Tasks[0].Status)
	receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, options.Now.Add(time.Millisecond))
	require.NoError(t, err, "a rolled-back cancellation must not consume the valid lease")
	require.Equal(t, "applied", receipt.Status)
}

func TestOfflineNameSyncLifecycleExplicitGuardIsVisibleInReadOnlyPreview(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	snapshot, options, _ := previewFixture(t)
	actor := lifecycleSyncManaged(t, db, 7, "旧名")
	plan, err := repo.PreviewOfflineSync(t.Context(), []uint{actor.ID}, snapshot, options)
	require.NoError(t, err)
	require.Equal(t, actresssync.EvidenceAvailable, plan.Items[0].Outcome)
	beforeActor, err := repo.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ProtectOfflineSyncName(t.Context(), actor.ID))
	afterActor, err := repo.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, beforeActor, afterActor, "protection must not need a canonical value or timestamp change")
	require.Equal(t, importNamesManaged, lifecycleSyncProvenance(t, db, actor.ID))
	beforePreview := previewDatabaseState(t, db)
	plan, err = repo.PreviewOfflineSync(t.Context(), []uint{actor.ID}, snapshot, options)
	require.NoError(t, err)
	require.Equal(t, actresssync.Protected, plan.Items[0].Outcome)
	require.True(t, plan.Items[0].Target.Protected)
	require.Nil(t, plan.Items[0].Evidence)
	require.Equal(t, beforePreview, previewDatabaseState(t, db), "reading the guard must not consume, repair or change any state")
}

func TestOfflineNameSyncLifecycleSourceKeyNeverTransfersToAnotherActor(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	owner := lifecycleSyncManaged(t, db, 812, "元の所有者")
	other := lifecycleSyncManaged(t, db, 813, "別の所有者")
	ownerReview, job, _ := lifecycleSyncConfirm(t, repo, owner.ID, key, "source-owner", snapshot, options)
	otherReview, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: other.ID, SourceKey: key}}, snapshot, options)
	require.NoError(t, err)
	assertConflict := func(stage string) {
		t.Helper()
		before := previewDatabaseState(t, db)
		rejected, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "reassign-"+stage, *otherReview, snapshot, options.Now)
		require.ErrorIs(t, err, ErrActressSyncConflict, stage)
		require.Nil(t, rejected, stage)
		require.Equal(t, before, previewDatabaseState(t, db), stage+": failed reassignment must roll back the new job too")
		var binding actressSyncBinding
		require.NoError(t, db.Where("source_key = ?", key).Take(&binding).Error)
		require.Equal(t, owner.ID, binding.ActressID, stage)
	}
	assertConflict("active")
	require.NoError(t, repo.CancelOfflineNameSync(t.Context(), job.ID))
	assertConflict("cancelled")
	// Cancellation retires work, not source identity. A new explicit confirmation
	// for the same actor may proceed, but consumption does not free its source key.
	next, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "owner-reconfirmed", *ownerReview, snapshot, options.Now)
	require.NoError(t, err)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), next.ID, "source-owner-worker", options.Now, time.Minute)
	require.NoError(t, err)
	receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, options.Now)
	require.NoError(t, err)
	require.Equal(t, "applied", receipt.Status)
	assertConflict("consumed")
	require.NoError(t, repo.Delete(t.Context(), owner.ID))
	assertConflict("deleted")
}

func TestOfflineNameSyncLifecycleMalformedConfirmationAdmissionIsReadOnly(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 814, "入場前")
	review, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: actor.ID, SourceKey: key}}, snapshot, options)
	require.NoError(t, err)
	before := previewDatabaseState(t, db)
	for _, tc := range []struct {
		name   string
		mutate func(*ActressSyncReview)
	}{
		{"non-hex snapshot digest", func(r *ActressSyncReview) { r.SnapshotDigest = strings.Repeat("g", 64) }},
		{"non-hex actor token", func(r *ActressSyncReview) { r.Proposals[0].ActorToken = strings.Repeat("z", 64) }},
		{"empty source key", func(r *ActressSyncReview) { r.Proposals[0].SourceKey = "" }},
		{"oversized source key", func(r *ActressSyncReview) { r.Proposals[0].SourceKey = strings.Repeat("x", 513) }},
		{"empty selection", func(r *ActressSyncReview) { r.Proposals = nil }},
		{"zero actor ID", func(r *ActressSyncReview) { r.Proposals[0].ActressID = 0 }},
		{"duplicate source", func(r *ActressSyncReview) { r.Proposals = append(r.Proposals, r.Proposals[0]) }},
		{"duplicate actor", func(r *ActressSyncReview) {
			p := r.Proposals[0]
			p.SourceKey += "x"
			r.Proposals = append(r.Proposals, p)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			malformed := *review
			malformed.Proposals = append([]ActressSyncProposal(nil), review.Proposals...)
			tc.mutate(&malformed)
			job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "malformed-review", malformed, snapshot, options.Now)
			require.Error(t, err)
			require.Nil(t, job)
			require.Equal(t, before, previewDatabaseState(t, db))
		})
	}
	for _, invalidKey := range []string{"", strings.Repeat("x", 513)} {
		_, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: actor.ID, SourceKey: invalidKey}}, snapshot, options)
		require.ErrorIs(t, err, ErrActressSyncConflict)
	}
	require.Equal(t, before, previewDatabaseState(t, db))
}

func TestOfflineNameSyncLifecycleConcurrentClaimHasOneWinner(t *testing.T) {
	db := lifecycleSyncOpen(t, filepath.Join(t.TempDir(), "concurrent-claim.db"))
	t.Cleanup(func() { _ = db.Close() })
	// Let both callers reach SQLite independently instead of merely queueing on
	// database/sql's single-connection fixture pool.
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)
	repo := NewActressRepository(db)
	snapshot, options, key := lifecycleSyncEvidence(t)
	actor := lifecycleSyncManaged(t, db, 815, "取得前")
	review, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{ActressID: actor.ID, SourceKey: key}}, snapshot, options)
	require.NoError(t, err)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "concurrent-claim", *review, snapshot, options.Now)
	require.NoError(t, err)
	type claimResult struct {
		lease *ActressSyncLease
		err   error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	for _, owner := range []string{"claim-worker-a", "claim-worker-b"} {
		go func(owner string) {
			<-start
			lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, owner, options.Now, time.Minute)
			results <- claimResult{lease: lease, err: err}
		}(owner)
	}
	close(start)
	var winner *ActressSyncLease
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			require.ErrorIs(t, result.err, gorm.ErrRecordNotFound)
			require.Nil(t, result.lease)
			continue
		}
		require.Nil(t, winner, "two simultaneous claimers must not both receive a usable lease")
		winner = result.lease
	}
	require.NotNil(t, winner)
	require.EqualValues(t, 1, winner.Generation)
	receipt, err := repo.ApplyOfflineNameSync(t.Context(), *winner, snapshot, options.Now)
	require.NoError(t, err)
	require.Equal(t, "applied", receipt.Status)
	_, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "late-worker", options.Now.Add(time.Hour), time.Minute)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestOfflineNameSyncLifecycleBatchSubmitRollbackAndIndependentReceipts(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	_, options, limits := previewFixture(t)
	dump := "COPY public.derived_actress (id, name_kanji) FROM stdin;\nbatch-one\t更新された一人目\nbatch-two\t更新された二人目\n\\.\n"
	snapshot, _, err := r18dump.Convert(t.Context(), strings.NewReader(dump), actresscache.Provenance{SourceURL: "https://example.invalid/batch.sql", ParserRevision: "lifecycle-batch-v1", ObservedAt: options.Now}, r18dump.Limits{MaxInputBytes: 4096, MaxLineBytes: 1024, MaxRows: 10, Output: limits})
	require.NoError(t, err)
	require.Len(t, snapshot.Records(), 2)
	actors := []models.Actress{
		lifecycleSyncManaged(t, db, 816, "最初の一人目"),
		lifecycleSyncManaged(t, db, 817, "最初の二人目"),
	}
	choices := make([]ActressSyncChoice, len(actors))
	for i, sourceID := range []string{"batch-one", "batch-two"} {
		key, err := actresscache.SourceKey("r18dev", sourceID)
		require.NoError(t, err)
		choices[i] = ActressSyncChoice{ActressID: actors[i].ID, SourceKey: key}
	}
	review, err := repo.ReviewOfflineNameSync(t.Context(), choices, snapshot, options)
	require.NoError(t, err)
	require.Len(t, review.Proposals, 2)
	before := previewDatabaseState(t, db)

	// Only the second proposal is stale, not malformed: the first task must be
	// inserted before the evidence mismatch rejects and rolls back the batch.
	stale := *review
	stale.Proposals = append([]ActressSyncProposal(nil), review.Proposals...)
	stale.Proposals[1].AfterName = "証拠と異なる二人目"
	insertedTasks := 0
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("test:lifecycle_batch_task_insert", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "actress_sync_tasks" && tx.Error == nil && tx.RowsAffected == 1 {
			insertedTasks++
		}
	}))
	rejected, submitErr := repo.SubmitConfirmedOfflineNameSync(t.Context(), "batch-confirmation", stale, snapshot, options.Now)
	require.NoError(t, db.Callback().Create().Remove("test:lifecycle_batch_task_insert"))
	require.ErrorIs(t, submitErr, ErrActressSyncStale)
	require.Nil(t, rejected)
	require.Equal(t, 1, insertedTasks, "the test must reach a real first-task insertion before the later proposal fails")
	require.Equal(t, before, previewDatabaseState(t, db), "the entire batch, including first binding/task and job, must roll back")

	oversized := *review
	oversized.Proposals = make([]ActressSyncProposal, actresssync.MaxSelection+1)
	for i := range oversized.Proposals {
		proposal := review.Proposals[0]
		proposal.ActressID = uint(10000 + i)
		proposal.SourceKey, err = actresscache.SourceKey("r18dev", fmt.Sprintf("oversized-%d", i))
		require.NoError(t, err)
		oversized.Proposals[i] = proposal
	}
	rejected, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "oversized-batch", oversized, snapshot, options.Now)
	require.ErrorIs(t, err, ErrActressSyncConflict)
	require.Nil(t, rejected)
	require.Equal(t, before, previewDatabaseState(t, db))

	// Reuse the failed request key: no partial request or binding may remain.
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "batch-confirmation", *review, snapshot, options.Now)
	require.NoError(t, err)
	require.Len(t, job.Tasks, 2)
	require.Less(t, job.Tasks[0].ID, job.Tasks[1].ID)
	require.NotEqual(t, job.Tasks[0].BindingID, job.Tasks[1].BindingID)
	leases := make([]*ActressSyncLease, 2)
	for i := range leases {
		leases[i], err = repo.ClaimOfflineNameSync(t.Context(), job.ID, fmt.Sprintf("batch-worker-%d", i), options.Now, time.Minute)
		require.NoError(t, err)
		require.Equal(t, job.Tasks[i].ID, leases[i].TaskID, "claims must follow task order while skipping already leased work")
	}
	// Completion order need not match claim order; each receipt is independent.
	for _, i := range []int{1, 0} {
		receipt, err := repo.ApplyOfflineNameSync(t.Context(), *leases[i], snapshot, options.Now.Add(time.Second))
		require.NoError(t, err)
		require.Equal(t, "applied", receipt.Status)
		require.Equal(t, actors[i].ID, receipt.ActressID)
		require.Equal(t, review.Proposals[i].BeforeName, receipt.BeforeName)
		require.Equal(t, review.Proposals[i].AfterName, receipt.AfterName)
		require.NotNil(t, receipt.FinishedAt)
		actor, err := repo.FindByID(t.Context(), actors[i].ID)
		require.NoError(t, err)
		require.Equal(t, review.Proposals[i].AfterName, actor.JapaneseName)
	}
	finished, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Len(t, finished.Tasks, 2)
	for i, task := range finished.Tasks {
		require.Equal(t, job.Tasks[i].ID, task.ID)
		require.Equal(t, "applied", task.Status)
		var binding actressSyncBinding
		require.NoError(t, db.First(&binding, task.BindingID).Error)
		require.Equal(t, "consumed", binding.Status)
	}
	_, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "batch-late-worker", options.Now.Add(time.Hour), time.Minute)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
