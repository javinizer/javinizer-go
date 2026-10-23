package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestStartupLockFailureOrCancellationPreservesSchema(t *testing.T) {
	for _, stage := range []string{"lock directory rejected", "canceled lock acquisition", "canceled in-memory startup", "held file lock deadline", "metadata SQL rejected"} {
		t.Run(stage, func(t *testing.T) {
			dsn := filepath.Join(t.TempDir(), "state.db")
			if stage == "canceled in-memory startup" {
				dsn = ":memory:"
			}
			db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.Exec("CREATE TABLE application_state (value TEXT NOT NULL)").Error)
			require.NoError(t, db.Exec("INSERT INTO application_state VALUES ('preserved')").Error)
			type schemaEntry struct {
				Type string
				Name string
				SQL  string
			}
			snapshot := func() []schemaEntry {
				var entries []schemaEntry
				require.NoError(t, db.Raw("SELECT type, name, sql FROM sqlite_schema ORDER BY type, name").Scan(&entries).Error)
				return entries
			}
			before := snapshot()
			ctx := context.Background()
			var releaseHeldLock func()
			expectedError := error(context.Canceled)
			if stage == "lock directory rejected" {
				db.fs = afero.NewReadOnlyFs(afero.NewOsFs())
			} else if stage == "held file lock deadline" {
				sqlDB, handleErr := db.DB.DB()
				require.NoError(t, handleErr)
				locker, lockErr := newStartupMigrationLocker(dsn, db.fs)
				require.NoError(t, lockErr)
				require.NoError(t, locker.Lock(ctx, sqlDB))
				released := false
				releaseHeldLock = func() {
					if !released {
						require.NoError(t, locker.Unlock(context.Background(), sqlDB))
						released = true
					}
				}
				t.Cleanup(releaseHeldLock)
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
				expectedError = context.DeadlineExceeded
			} else if stage == "metadata SQL rejected" {
				sqlDB, handleErr := db.DB.DB()
				require.NoError(t, handleErr)
				sqlDB.SetMaxOpenConns(1)
				require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err = db.RunMigrationsOnStartup(ctx)
			if stage == "lock directory rejected" {
				require.ErrorIs(t, err, os.ErrPermission)
			} else if stage == "metadata SQL rejected" {
				require.ErrorContains(t, err, "ensure migration hash table")
				require.ErrorContains(t, err, "readonly")
			} else {
				require.ErrorIs(t, err, expectedError)
			}
			require.Equal(t, before, snapshot(), "rejected or canceled startup must preserve schema, including migration metadata")
			var value string
			require.NoError(t, db.Raw("SELECT value FROM application_state").Scan(&value).Error)
			require.Equal(t, "preserved", value)
			if releaseHeldLock != nil {
				releaseHeldLock()
			}
			db.fs = afero.NewOsFs()
			if stage == "metadata SQL rejected" {
				require.NoError(t, db.Exec("PRAGMA query_only = OFF").Error)
			}
			retryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(t, db.RunMigrationsOnStartup(retryCtx), "retry must succeed after refusal is removed, including releasing any acquired lock")
			require.NoError(t, db.Raw("SELECT value FROM application_state").Scan(&value).Error)
			require.Equal(t, "preserved", value)
		})
	}
}
