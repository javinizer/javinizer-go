package r18devdump

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestImportSQLiteAuthorizationFailurePreservesPublishedDump(t *testing.T) {
	for _, tc := range []struct {
		name         string
		action       int
		argument     string
		errorContext string
	}{
		{"begin", sqlite3.SQLITE_TRANSACTION, "BEGIN", "begin tx:"},
		{"commit", sqlite3.SQLITE_TRANSACTION, "COMMIT", "commit:"},
		{"checkpoint", sqlite3.SQLITE_PRAGMA, "wal_checkpoint", "wal checkpoint:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := seedDump(t, "old00001\tOLD-001")
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			originalInfo, err := os.Stat(path)
			require.NoError(t, err)
			const incoming = "COPY public.derived_video (content_id, dvd_id) FROM stdin;\nnew00001\tNEW-001\n\\.\n"
			denied := 0
			// Inject a supported SQLite authorization error on the real connection.
			// All schema/data operations and rollback remain real SQLite operations.
			open := func(driverName, dsn string) (*sql.DB, error) {
				db, err := sql.Open(driverName, dsn)
				if err != nil {
					return nil, err
				}
				t.Cleanup(func() { _ = db.Close() })
				db.SetMaxOpenConns(1)
				db.SetMaxIdleConns(1)
				conn, err := db.Conn(t.Context())
				if err != nil {
					_ = db.Close()
					return nil, err
				}
				err = conn.Raw(func(raw any) error {
					raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(action int, argument, _, _ string) int {
						if action == tc.action && argument == tc.argument {
							denied++
							return sqlite3.SQLITE_DENY
						}
						return sqlite3.SQLITE_OK
					})
					return nil
				})
				closeErr := conn.Close()
				if err != nil {
					_ = db.Close()
					return nil, err
				}
				if closeErr != nil {
					_ = db.Close()
					return nil, closeErr
				}
				return db, nil
			}
			beforeSwap, afterSwap := false, false
			result, err := importWithOpenDB(t.Context(), strings.NewReader(incoming), path, ImportOptions{
				BeforeSwap: func() error { beforeSwap = true; return nil },
				AfterSwap:  func() { afterSwap = true },
			}, open)
			require.ErrorContains(t, err, tc.errorContext)
			var sqliteErr sqlite3.Error
			require.ErrorAs(t, err, &sqliteErr)
			require.Equal(t, sqlite3.ErrAuth, sqliteErr.Code)
			require.Equal(t, 1, denied)
			require.Equal(t, ImportResult{}, result)
			require.False(t, beforeSwap, "failed preparation must not detach the published dump")
			require.False(t, afterSwap)
			for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
				require.NoFileExists(t, path+suffix)
			}
			remaining, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, remaining)
			remainingInfo, err := os.Stat(path)
			require.NoError(t, err)
			require.True(t, os.SameFile(originalInfo, remainingInfo))
			require.Equal(t, originalInfo.Mode().Perm(), remainingInfo.Mode().Perm())
			old, err := Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = old.Close() })
			id, err := old.LookupByContentID(t.Context(), "old00001")
			require.NoError(t, err)
			require.Equal(t, "OLD-001", id)
			require.NoError(t, old.Close()) // Release the published handle before retry on Windows.

			result, err = Import(t.Context(), strings.NewReader(incoming), path, ImportOptions{})
			require.NoError(t, err)
			require.Equal(t, ImportResult{Rows: 1, Path: path}, result)
			for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
				require.NoFileExists(t, path+suffix)
			}
			fresh, err := Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = fresh.Close() })
			id, err = fresh.LookupByContentID(t.Context(), "new00001")
			require.NoError(t, err)
			require.Equal(t, "NEW-001", id)
			id, err = fresh.LookupByContentID(t.Context(), "old00001")
			require.ErrorIs(t, err, models.ErrDumpMiss)
			require.Empty(t, id, "successful retry must replace, not append to, the old dump")
		})
	}
}
