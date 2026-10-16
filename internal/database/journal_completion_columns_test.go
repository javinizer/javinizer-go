package database

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func journalCompletionNative(t *testing.T, db *DB, install func(*sqlite3.SQLiteConn)) func() {
	t.Helper()
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	configure := func(fn func(*sqlite3.SQLiteConn)) {
		conn, err := sqlDB.Conn(context.Background())
		require.NoError(t, err)
		require.NoError(t, conn.Raw(func(raw any) error {
			fn(raw.(*sqlite3.SQLiteConn))
			return nil
		}))
		require.NoError(t, conn.Close())
	}
	configure(install)
	clear := func() {
		configure(func(native *sqlite3.SQLiteConn) {
			native.RegisterUpdateHook(nil)
			native.RegisterCommitHook(nil)
		})
	}
	t.Cleanup(clear)
	return clear
}

func seedJournalCompletionRow(t *testing.T, repo *BatchFileOperationRepository) *models.BatchFileOperation {
	t.Helper()
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{Roots: []string{"/old-root"}})
	op.NewPath = "/old-primary"
	op.InPlaceRenamed = true
	op.OriginalDirPath = "/old-directory"
	op.NFOPath = "/fresh-existing.nfo"
	op.NFOSnapshot = "fresh snapshot"
	op.MovieID = "FRESH-MOVIE"
	op.CreatedAt = time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	op.RevertedAt = &op.CreatedAt
	require.NoError(t, repo.Update(t.Context(), op))
	stored, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	return stored
}

func completionJournal(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
	return models.GeneratedFilesJSON{Roots: []string{"/committed-root"}}, true, nil
}

func TestJournalCompletionColumnsBecomeVisibleWithJournal(t *testing.T) {
	db, repo, other := columnGuardDBs(t)
	before := seedJournalCompletionRow(t, other)
	journalWritten, releaseJournal := make(chan struct{}), make(chan struct{})
	commitReached, releaseCommit := make(chan struct{}), make(chan struct{})
	var releaseJournalOnce, releaseCommitOnce sync.Once
	var writes, commits atomic.Int32
	journalCompletionNative(t, db, func(native *sqlite3.SQLiteConn) {
		native.RegisterUpdateHook(func(_ int, _ string, table string, _ int64) {
			if table == "batch_file_operations" && writes.Add(1) == 1 {
				close(journalWritten)
				<-releaseJournal
			}
		})
		native.RegisterCommitHook(func() int {
			commits.Add(1)
			close(commitReached)
			<-releaseCommit
			return 0
		})
	})
	columns := &JournalCompletionColumns{NewPath: "/new-primary", OriginalDirPath: "/new-directory", RevertStatus: models.RevertStatusFailed, FoundNFOPath: "/found.nfo", NFOPath: "/fallback.nfo"}
	ctx := WithJournalCompletionColumns(t.Context(), func() (*JournalCompletionColumns, error) { return columns, nil })
	done := make(chan error, 1)
	finished := false
	defer func() {
		releaseJournalOnce.Do(func() { close(releaseJournal) })
		releaseCommitOnce.Do(func() { close(releaseCommit) })
		if !finished {
			<-done
		}
	}()
	go func() { done <- repo.UpdateJournalInTx(ctx, before.ID, completionJournal) }()
	// Both probes use an independent SQLite connection pool. The first runs
	// after journal SQL and the second after BOTH updates, before COMMIT.
	<-journalWritten
	between, err := other.FindByID(t.Context(), before.ID)
	require.NoError(t, err)
	require.Equal(t, before, between, "journal must not become visible before its completion columns")
	require.Equal(t, int32(1), writes.Load())
	releaseJournalOnce.Do(func() { close(releaseJournal) })
	<-commitReached
	between, err = other.FindByID(t.Context(), before.ID)
	require.NoError(t, err)
	require.Equal(t, before, between, "neither update is visible before the shared commit")
	require.Equal(t, int32(2), writes.Load())
	require.Equal(t, int32(1), commits.Load())
	releaseCommitOnce.Do(func() { close(releaseCommit) })
	err = <-done
	finished = true
	require.NoError(t, err)
	after, err := other.FindByID(t.Context(), before.ID)
	require.NoError(t, err)
	expected := *before
	expected.GeneratedFiles = models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/committed-root"}})
	expected.NewPath = columns.NewPath
	expected.InPlaceRenamed = false
	expected.OriginalDirPath = columns.OriginalDirPath
	expected.RevertStatus = columns.RevertStatus
	expected.NFOPath = columns.FoundNFOPath
	expected.UpdatedAt = after.UpdatedAt
	require.Equal(t, &expected, after, "fresh snapshot, identity, original path, and revert timestamps survive the narrow write")
	require.False(t, after.UpdatedAt.Before(before.UpdatedAt))
}

func TestJournalCompletionColumnsPreserveFreshMetadataAndNFO(t *testing.T) {
	for _, tc := range []struct {
		name, stored, found, fallback, want string
		nullStored                          bool
	}{
		{name: "preserve-known", stored: "/known.nfo", fallback: "/fallback.nfo", want: "/known.nfo"},
		{name: "fill-empty", fallback: "/fallback.nfo", want: "/fallback.nfo"},
		{name: "fill-null", nullStored: true, fallback: "/fallback.nfo", want: "/fallback.nfo"},
		{name: "found-overrides", stored: "/known.nfo", found: "/found.nfo", fallback: "/fallback.nfo", want: "/found.nfo"},
		{name: "empty-stays-empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, repo, other := columnGuardDBs(t)
			before := seedJournalCompletionRow(t, other)
			var stored any = tc.stored
			if tc.nullStored {
				stored = nil
			}
			require.NoError(t, db.Exec("UPDATE batch_file_operations SET nfo_path = ?, revert_status = ? WHERE id = ?", stored, models.RevertStatusFailed, before.ID).Error)
			before, err := other.FindByID(t.Context(), before.ID)
			require.NoError(t, err)
			ctx := WithJournalCompletionColumns(t.Context(), func() (*JournalCompletionColumns, error) {
				return &JournalCompletionColumns{FoundNFOPath: tc.found, NFOPath: tc.fallback}, nil
			})
			require.NoError(t, repo.UpdateJournalInTx(ctx, before.ID, completionJournal))
			after, err := other.FindByID(t.Context(), before.ID)
			require.NoError(t, err)
			expected := *before
			expected.GeneratedFiles = models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/committed-root"}})
			expected.NewPath, expected.OriginalDirPath, expected.InPlaceRenamed = "", "", false
			expected.NFOPath, expected.UpdatedAt = tc.want, after.UpdatedAt
			require.Equal(t, &expected, after, "empty status preserves fresh status; zero primary fields still apply")
		})
	}
}

func TestJournalCompletionColumnsNilReplayAndIndependentPersist(t *testing.T) {
	for _, mode := range []string{"absent", "nil-callback", "nil-payload", "columns-without-journal", "journal-without-columns"} {
		t.Run(mode, func(t *testing.T) {
			db, repo, other := columnGuardDBs(t)
			before := seedJournalCompletionRow(t, other)
			writes := columnGuardHooks(t, db, false)
			ctx := t.Context()
			calls, journalDone := 0, false
			if mode == "nil-callback" {
				ctx = WithJournalCompletionColumns(ctx, nil)
			} else if mode != "absent" {
				ctx = WithJournalCompletionColumns(ctx, func() (*JournalCompletionColumns, error) {
					calls++
					require.True(t, journalDone, "completion resolution follows journal admission")
					if mode == "columns-without-journal" {
						return &JournalCompletionColumns{NewPath: "/column-only"}, nil
					}
					return nil, nil
				})
			}
			require.NoError(t, repo.UpdateJournalInTx(ctx, before.ID, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
				journalDone = true
				return models.GeneratedFilesJSON{Roots: []string{"/new"}}, mode == "journal-without-columns", nil
			}))
			after, err := other.FindByID(t.Context(), before.ID)
			require.NoError(t, err)
			expected := *before
			switch mode {
			case "columns-without-journal":
				expected.NewPath, expected.OriginalDirPath, expected.InPlaceRenamed = "/column-only", "", false
				expected.UpdatedAt = after.UpdatedAt
			case "journal-without-columns":
				expected.GeneratedFiles = models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/new"}})
				expected.UpdatedAt = after.UpdatedAt
			}
			require.Equal(t, &expected, after)
			if mode == "columns-without-journal" || mode == "journal-without-columns" {
				require.Equal(t, int32(1), writes.Load())
			} else {
				require.Zero(t, writes.Load(), "exact replay must not change timestamps or fields")
			}
			if mode == "absent" || mode == "nil-callback" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestJournalCompletionColumnsFailuresRollBackBoth(t *testing.T) {
	for _, failure := range []string{"journal-callback", "completion-callback", "journal-sql", "completion-sql", "commit"} {
		t.Run(failure, func(t *testing.T) {
			db, repo, other := columnGuardDBs(t)
			before := seedJournalCompletionRow(t, other)
			if failure == "journal-sql" || failure == "completion-sql" {
				column := "generated_files"
				if failure == "completion-sql" {
					column = "new_path"
				}
				require.NoError(t, db.Exec("CREATE TRIGGER reject_completion BEFORE UPDATE OF "+column+" ON batch_file_operations BEGIN SELECT RAISE(ABORT, 'injected completion SQL failure'); END").Error)
			}
			var writes, commits atomic.Int32
			clear := journalCompletionNative(t, db, func(native *sqlite3.SQLiteConn) {
				native.RegisterUpdateHook(func(_ int, _ string, table string, _ int64) {
					if table == "batch_file_operations" {
						writes.Add(1)
					}
				})
				native.RegisterCommitHook(func() int {
					commits.Add(1)
					if failure == "commit" {
						return 1
					}
					return 0
				})
			})
			sentinel := errors.New("injected callback failure")
			calls := 0
			ctx := WithJournalCompletionColumns(t.Context(), func() (*JournalCompletionColumns, error) {
				calls++
				if failure == "completion-callback" {
					return nil, sentinel
				}
				return &JournalCompletionColumns{NewPath: "/must-rollback", RevertStatus: models.RevertStatusNoOp, FoundNFOPath: "/must-rollback.nfo"}, nil
			})
			err := repo.UpdateJournalInTx(ctx, before.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
				if failure == "journal-callback" {
					return models.GeneratedFilesJSON{}, true, sentinel
				}
				return completionJournal(current)
			})
			require.Error(t, err)
			if failure == "journal-callback" || failure == "completion-callback" {
				require.ErrorIs(t, err, sentinel)
			}
			if failure == "journal-callback" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			wantWrites := map[string]int32{"completion-sql": 1, "commit": 2}[failure]
			require.Equal(t, wantWrites, writes.Load(), "proves which SQL actually executed before rollback")
			if failure == "commit" {
				require.Equal(t, int32(1), commits.Load(), "the native SQLite COMMIT itself was rejected")
			} else {
				require.Zero(t, commits.Load())
			}
			after, err := other.FindByID(t.Context(), before.ID)
			require.NoError(t, err)
			require.Equal(t, before, after, "journal, metadata, status, and timestamps roll back together")
			clear()
			require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS reject_completion").Error)
			retryCtx := WithJournalCompletionColumns(t.Context(), func() (*JournalCompletionColumns, error) {
				return &JournalCompletionColumns{NewPath: "/retried"}, nil
			})
			require.NoError(t, repo.UpdateJournalInTx(retryCtx, before.ID, completionJournal), "rollback released the transaction and connection for retry")
			retried, err := other.FindByID(t.Context(), before.ID)
			require.NoError(t, err)
			require.Equal(t, "/retried", retried.NewPath)
			require.NotEqual(t, before.GeneratedFiles, retried.GeneratedFiles)
		})
	}
}

func TestJournalCompletionColumnsMissingRowSkipsCallbacks(t *testing.T) {
	db, repo, _ := columnGuardDBs(t)
	calls := 0
	ctx := WithJournalCompletionColumns(t.Context(), func() (*JournalCompletionColumns, error) {
		calls++
		return &JournalCompletionColumns{}, nil
	})
	writes := columnGuardHooks(t, db, false)
	err := repo.UpdateJournalInTx(ctx, 999999, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		calls++
		return models.GeneratedFilesJSON{}, true, nil
	})
	require.ErrorIs(t, err, ErrNotFound)
	require.Zero(t, calls)
	require.Zero(t, writes.Load())
}
