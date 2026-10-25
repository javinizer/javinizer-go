package database

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Two independent single-connection pools use the same disposable SQLite file.
// The first transaction is held AFTER a real successful write. busy_timeout=0
// lets the competitor report an actual SQLite lock conflict, not merely enter a
// goroutine/callback. Only that observed conflict releases the winning writer.
// Timeouts bound a broken test; no sleep determines the successful ordering.
func TestOfflineNameSyncWriteLockRaces(t *testing.T) {
	for _, opponent := range []string{"cancel", "manual edit"} {
		for _, applyFirst := range []bool{true, false} {
			ordering := "opponent first"
			if applyFirst {
				ordering = "apply first"
			}
			t.Run(opponent+"/"+ordering, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "write-race.db")
				firstDB := lifecycleSyncOpen(t, path)
				t.Cleanup(func() { _ = firstDB.Close() })
				secondDB := lifecycleSyncOpen(t, path)
				t.Cleanup(func() { _ = secondDB.Close() })
				require.NoError(t, secondDB.Exec("PRAGMA busy_timeout = 0").Error)
				var busyTimeout int
				require.NoError(t, secondDB.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error)
				require.Zero(t, busyTimeout)
				firstRepo, secondRepo := NewActressRepository(firstDB), NewActressRepository(secondDB)
				snapshot, options, key := lifecycleSyncEvidence(t)
				actor := lifecycleSyncManaged(t, firstDB, 830, "競合前")
				_, job, lease := lifecycleSyncConfirm(t, firstRepo, actor.ID, key, "write-race", snapshot, options)
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				type result struct {
					receipt *ActressSyncTask
					err     error
				}
				invoke := func(repo *ActressRepository, operation string) result {
					switch operation {
					case "apply":
						receipt, err := repo.ApplyOfflineNameSync(ctx, *lease, snapshot, options.Now)
						return result{receipt, err}
					case "cancel":
						return result{err: repo.CancelOfflineNameSync(ctx, job.ID)}
					default:
						return result{err: repo.UpdateCanonicalFields(ctx, actor.ID, actor.FirstName, actor.LastName, "手動の名前", actor.ThumbURL)}
					}
				}
				firstOperation, secondOperation := opponent, "apply"
				if applyFirst {
					firstOperation, secondOperation = "apply", opponent
				}
				matches := func(operation string, tx *gorm.DB) bool {
					switch operation {
					case "apply":
						return tx.Statement.Table == "actress_sync_tasks"
					case "cancel":
						return tx.Statement.Table == "actress_sync_jobs"
					default:
						return strings.HasPrefix(tx.Statement.SQL.String(), "INSERT INTO actress_sync_name_guards")
					}
				}
				type admission struct {
					err  error
					rows int64
				}
				firstWritten := make(chan admission, 1)
				conflict := make(chan error, 1)
				release := make(chan struct{})
				var releaseOnce, firstOnce, conflictOnce sync.Once
				releaseWriter := func() { releaseOnce.Do(func() { close(release) }) }
				defer releaseWriter()
				firstHook := func(tx *gorm.DB) {
					if matches(firstOperation, tx) {
						firstOnce.Do(func() {
							firstWritten <- admission{tx.Error, tx.RowsAffected}
							<-release
						})
					}
				}
				secondHook := func(tx *gorm.DB) {
					if matches(secondOperation, tx) && isLocked(tx.Error) {
						conflictOnce.Do(func() { conflict <- tx.Error })
					}
				}
				register := func(db *DB, operation, name string, hook func(*gorm.DB)) func() {
					if operation == "manual edit" {
						require.NoError(t, db.Callback().Raw().After("gorm:raw").Register(name, hook))
						return func() { require.NoError(t, db.Callback().Raw().Remove(name)) }
					}
					require.NoError(t, db.Callback().Update().After("gorm:update").Register(name, hook))
					return func() { require.NoError(t, db.Callback().Update().Remove(name)) }
				}
				removeFirst := register(firstDB, firstOperation, "test:sync_race_first_writer", firstHook)
				removeSecond := register(secondDB, secondOperation, "test:sync_race_lock_conflict", secondHook)
				firstDone, secondDone := make(chan result, 1), make(chan result, 1)
				go func() { firstDone <- invoke(firstRepo, firstOperation) }()
				select {
				case admitted := <-firstWritten:
					require.NoError(t, admitted.err)
					require.EqualValues(t, 1, admitted.rows, "first writer must hold a real successfully modified SQLite row")
				case <-ctx.Done():
					t.Fatal("first writer did not reach successful write admission")
				}
				go func() { secondDone <- invoke(secondRepo, secondOperation) }()
				select {
				case err := <-conflict:
					require.True(t, isLocked(err), "competing SQL must actually encounter the held write lock")
				case <-ctx.Done():
					t.Fatal("competing connection did not observe the held SQLite write lock")
				}
				releaseWriter()
				await := func(ch <-chan result) result {
					select {
					case value := <-ch:
						return value
					case <-ctx.Done():
						t.Fatal("serialized operation failed to finish")
						return result{}
					}
				}
				firstResult, secondResult := await(firstDone), await(secondDone)
				removeFirst()
				removeSecond()
				require.NoError(t, firstResult.err)
				if applyFirst && opponent == "manual edit" {
					// Canonical manual edits retain their existing no-auto-retry contract.
					// The failed competing intent is not silently considered committed. A
					// real caller retry after lock release must supersede the import update.
					require.True(t, isLocked(secondResult.err))
					afterApply, err := firstRepo.FindByID(t.Context(), actor.ID)
					require.NoError(t, err)
					require.Equal(t, "新しい名前", afterApply.JapaneseName)
					require.Equal(t, ActressOriginImport, afterApply.Origin)
					secondResult = invoke(secondRepo, secondOperation)
				}
				if applyFirst {
					require.NoError(t, secondResult.err)
				} else {
					require.ErrorIs(t, secondResult.err, ErrActressSyncLease)
					require.Nil(t, secondResult.receipt)
				}
				persisted, err := firstRepo.OfflineNameSyncJob(t.Context(), job.ID)
				require.NoError(t, err)
				require.Len(t, persisted.Tasks, 1)
				if applyFirst {
					require.NotNil(t, firstResult.receipt)
					require.Equal(t, "applied", firstResult.receipt.Status)
					require.Equal(t, "applied", persisted.Tasks[0].Status, "later cancel/edit cannot erase an already committed receipt")
				} else if opponent == "cancel" {
					require.Equal(t, "cancelled", persisted.Tasks[0].Status)
				} else {
					require.Equal(t, "invalidated", persisted.Tasks[0].Status)
				}
				finalActor, err := firstRepo.FindByID(t.Context(), actor.ID)
				require.NoError(t, err)
				if opponent == "manual edit" {
					require.Equal(t, "手動の名前", finalActor.JapaneseName)
					require.Equal(t, ActressOriginUser, finalActor.Origin)
					require.Equal(t, importNamesRevoked, lifecycleSyncProvenance(t, firstDB, actor.ID))
					var protected bool
					require.NoError(t, firstDB.Raw("SELECT protected FROM actress_sync_name_guards WHERE actress_id = ?", actor.ID).Scan(&protected).Error)
					require.True(t, protected)
				} else {
					require.True(t, persisted.Cancelled)
					expected := "競合前"
					if applyFirst {
						expected = "新しい名前"
					}
					require.Equal(t, expected, finalActor.JapaneseName)
				}
				beforeRetry := previewDatabaseState(t, firstDB)
				_, err = secondRepo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, options.Now)
				require.ErrorIs(t, err, ErrActressSyncLease)
				require.Equal(t, beforeRetry, previewDatabaseState(t, firstDB))
			})
		}
	}
}
