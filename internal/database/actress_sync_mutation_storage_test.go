package database

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Exercise genuine storage failure boundaries without inventing successful SQL
// results or impossible lease states. The public transaction must preserve every
// persisted row even when its earlier canonical writes have already executed.
func TestConfirmedOfflineNameSyncCanonicalStorageFailureRollsBack(t *testing.T) {
	for _, stage := range []string{"movie membership", "render snapshot", "name update", "translation delete", "identity credits", "alias transition", "collision reconciliation", "quarantine", "render reload", "restore provenance", "restore protection"} {
		t.Run(stage, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			actorID := review.Proposals[0].ActressID
			movie := models.Movie{ContentID: "sync-storage", ID: "sync-storage", RenderGeneration: 9}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actorID, CreditedName: "Actor Managed", CreditedJapaneseName: "旧名", Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actorID, Language: "en", DisplayName: "Prior"}).Error)
			job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "storage-rollback", review, snapshot, now)
			require.NoError(t, err)
			lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "storage-worker", now, time.Minute)
			require.NoError(t, err)
			before := previewDatabaseState(t, db)
			fault := errors.New("fixture storage operation failed")
			reached, written := false, false
			observer := "test:sync_storage_observe"
			require.NoError(t, db.Callback().Update().After("gorm:update").Register(observer, func(tx *gorm.DB) {
				if tx.Statement.Table == "actresses" && tx.Error == nil && tx.RowsAffected == 1 {
					written = true
				}
			}))
			fail := func(op string, tx *gorm.DB) {
				table, sql := tx.Statement.Table, tx.Statement.SQL.String()
				match := false
				switch stage {
				case "movie membership":
					match = op == "row" && strings.Contains(sql, "FROM movie_credits") && strings.Contains(sql, "UNION")
				case "render snapshot":
					match = op == "query" && table == "movies" && !written
				case "name update":
					match = op == "update" && table == "actresses"
				case "translation delete":
					match = op == "delete" && table == "actress_translations"
				case "identity credits":
					match = op == "update" && table == "movie_credits"
				case "alias transition":
					match = op == "create" && table == "actress_aliases"
				case "collision reconciliation":
					match = op == "query" && table == "credit_collisions" && written
				case "quarantine":
					match = op == "query" && table == "actress_quarantine_state"
				case "render reload":
					match = op == "query" && table == "movies" && written
				case "restore provenance":
					match = op == "raw" && strings.HasPrefix(sql, "INSERT INTO actress_import_name_provenance")
				case "restore protection":
					match = op == "raw" && strings.HasPrefix(sql, "UPDATE actress_sync_name_guards SET protected = 0")
				}
				if match {
					reached = true
					_ = tx.AddError(fault)
				}
			}
			callback := "test:sync_canonical_storage_failure"
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) { fail("query", tx) }))
			require.NoError(t, db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) { fail("row", tx) }))
			require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register(callback, func(tx *gorm.DB) { fail("raw", tx) }))
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) { fail("update", tx) }))
			require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callback, func(tx *gorm.DB) { fail("delete", tx) }))
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) { fail("create", tx) }))
			receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
			require.NoError(t, db.Callback().Query().Remove(callback))
			require.NoError(t, db.Callback().Row().Remove(callback))
			require.NoError(t, db.Callback().Raw().Remove(callback))
			require.NoError(t, db.Callback().Update().Remove(callback))
			require.NoError(t, db.Callback().Delete().Remove(callback))
			require.NoError(t, db.Callback().Create().Remove(callback))
			require.NoError(t, db.Callback().Update().Remove(observer))
			require.True(t, reached, "failure must occur at the intended reachable storage operation")
			require.ErrorIs(t, err, fault)
			require.Nil(t, receipt)
			require.Equal(t, before, previewDatabaseState(t, db))
			recovered, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
			require.NoError(t, err, "storage failure must not consume the lease or grant")
			require.Equal(t, "applied", recovered.Status)
		})
	}
}

func TestConfirmedOfflineNameSyncManualProtectionFailureRollsBack(t *testing.T) {
	for _, operation := range []string{"protect", "protect binding", "protect task", "update", "rename", "canonical", "user owned", "merge target", "merge source"} {
		t.Run(operation, func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			actor, err := repo.FindByID(t.Context(), review.Proposals[0].ActressID)
			require.NoError(t, err)
			other := models.Actress{DMMID: 9, FirstName: "Other", JapaneseName: "別人"}
			require.NoError(t, repo.ImportUpsert(t.Context(), &other))
			_, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "manual-storage", review, snapshot, now)
			require.NoError(t, err)
			before := previewDatabaseState(t, db)
			fault := errors.New("fixture protection storage failure")
			reached, inserts := false, 0
			callback := "test:sync_manual_storage_failure"
			require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register(callback, func(tx *gorm.DB) {
				if !strings.HasPrefix(tx.Statement.SQL.String(), "INSERT INTO actress_sync_name_guards") {
					return
				}
				inserts++
				if operation == "protect binding" || operation == "protect task" || (operation == "merge source" && inserts != 2) {
					return
				}
				reached = true
				_ = tx.AddError(fault)
			}))
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				if (operation == "protect binding" && tx.Statement.Table == "actress_sync_bindings") || (operation == "protect task" && tx.Statement.Table == "actress_sync_tasks") {
					reached = true
					_ = tx.AddError(fault)
				}
			}))
			switch operation {
			case "update":
				err = repo.Update(t.Context(), actor)
			case "rename":
				err = repo.RenameIdentityFields(t.Context(), actor.ID, actor.FirstName, actor.LastName, actor.JapaneseName, actor.ThumbURL)
			case "canonical":
				err = repo.UpdateCanonicalFields(t.Context(), actor.ID, actor.FirstName, actor.LastName, actor.JapaneseName, actor.ThumbURL)
			case "user owned":
				err = repo.SetUserOwned(t.Context(), actor.ID)
			case "merge target", "merge source":
				_, err = repo.Merge(t.Context(), actor.ID, other.ID, map[string]string{"dmm_id": "target"})
			default:
				err = repo.ProtectOfflineSyncName(t.Context(), actor.ID)
			}
			require.NoError(t, db.Callback().Raw().Remove(callback))
			require.NoError(t, db.Callback().Update().Remove(callback))
			require.True(t, reached)
			require.ErrorIs(t, err, fault)
			require.Equal(t, before, previewDatabaseState(t, db))
		})
	}
}
