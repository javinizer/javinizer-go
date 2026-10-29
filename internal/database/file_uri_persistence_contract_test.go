package database_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

func TestFileURIConnectionSettingsAndPersistence(t *testing.T) {
	for _, tc := range []struct {
		name, query, journal string
		busy                 int
	}{
		{"defaults_without_query", "", "wal", 5000},
		{"explicit_settings", "?_journal_mode=DELETE&_busy_timeout=1234", "delete", 1234},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owned database.sqlite")
			// Escape the URI path, not its separators or Windows drive colon.
			uriPath := (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
			dsn := "file:" + uriPath + tc.query
			open := func() *database.DB {
				db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn})
				require.NoError(t, err)
				sqlDB, err := db.DB.DB()
				require.NoError(t, err)
				// These pragmas are connection-local; hold one connection in the pool.
				sqlDB.SetMaxOpenConns(1)
				sqlDB.SetMaxIdleConns(1)
				t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
				var journal string
				var busy int
				require.NoError(t, db.Raw("PRAGMA journal_mode").Scan(&journal).Error)
				require.NoError(t, db.Raw("PRAGMA busy_timeout").Scan(&busy).Error)
				require.Equal(t, tc.journal, journal)
				require.Equal(t, tc.busy, busy)
				return db
			}
			db := open()
			require.NoError(t, db.Exec("CREATE TABLE uri_contract (id INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error)
			require.NoError(t, db.Exec("INSERT INTO uri_contract (id, value) VALUES (?, ?)", 1, "persisted fixture").Error)
			require.NoError(t, db.Close())
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.True(t, info.Mode().IsRegular())
			reopened := open()
			var value string
			require.NoError(t, reopened.Raw("SELECT value FROM uri_contract WHERE id = ?", 1).Scan(&value).Error)
			require.Equal(t, "persisted fixture", value)
			require.NoError(t, reopened.Close())
			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			require.Len(t, entries, 1, "closed handles must leave only the actual database file")
			require.Equal(t, filepath.Base(path), entries[0].Name())
		})
	}
}
