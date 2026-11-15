package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/mattn/go-sqlite3"
	gooselock "github.com/pressly/goose/v3/lock"
	"github.com/stretchr/testify/require"
)

// Keep acquisition and successful release on a real file lock. The only fault
// is the selected Unlock boundary, before the underlying lock is released.
type migrationReleaseFaultLock struct {
	*flock.Flock
	unlockErr error
	calls     []string
}

func (l *migrationReleaseFaultLock) TryLockContext(ctx context.Context, retry time.Duration) (bool, error) {
	l.calls = append(l.calls, "lock")
	return l.Flock.TryLockContext(ctx, retry)
}

func (l *migrationReleaseFaultLock) Unlock() error {
	l.calls = append(l.calls, "unlock")
	if l.unlockErr != nil {
		return l.unlockErr
	}
	return l.Flock.Unlock()
}

func (l *migrationReleaseFaultLock) Close() error {
	l.calls = append(l.calls, "close")
	return l.Flock.Close()
}

type migrationReleaseObserver struct {
	gooselock.Locker
	unlockContext context.Context
	unlockDB      *sql.DB
	unlockCalls   int
}

func (l *migrationReleaseObserver) Unlock(ctx context.Context, sqlDB *sql.DB) error {
	l.unlockContext = ctx
	l.unlockDB = sqlDB
	l.unlockCalls++
	return l.Locker.Unlock(ctx, sqlDB)
}

func TestFileMigrationLockerReleaseOwnership(t *testing.T) {
	for _, failUnlock := range []bool{false, true} {
		name := "successful release closes once"
		if failUnlock {
			name = "failed unlock does not close"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "migration.lock")
			fileLock := &migrationReleaseFaultLock{Flock: flock.New(path)}
			t.Cleanup(func() { require.NoError(t, fileLock.Flock.Close()) })
			unlockErr := errors.New("fixture migration lock release failed")
			if failUnlock {
				fileLock.unlockErr = unlockErr
			}
			locker := &fileMigrationLocker{fileLock: fileLock}
			require.NoError(t, locker.Lock(t.Context(), nil))
			require.True(t, fileLock.Locked())

			err := locker.Unlock(context.Background(), nil)
			if failUnlock {
				require.ErrorIs(t, err, unlockErr)
				require.Equal(t, []string{"lock", "unlock"}, fileLock.calls)
				require.True(t, fileLock.Locked(), "a failed unlock must not silently close and release the resource")
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"lock", "unlock", "close"}, fileLock.calls)
				require.False(t, fileLock.Locked())
			}
			probe := flock.New(path)
			t.Cleanup(func() { require.NoError(t, probe.Close()) })
			acquired, err := probe.TryLock()
			require.NoError(t, err)
			require.Equal(t, !failUnlock, acquired, "a separate lock must observe the actual ownership state")
		})
	}
}

func TestStartupMigrationReleaseFailurePrecedence(t *testing.T) {
	for _, rejectMigration := range []bool{false, true} {
		name := "successful migration reports release failure"
		if rejectMigration {
			name = "migration failure wins over release failure"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "application.db")
			db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			sqlDB, err := db.DB.DB()
			require.NoError(t, err)
			require.NoError(t, db.Exec("CREATE TABLE application_state (value TEXT NOT NULL)").Error)
			require.NoError(t, db.Exec("INSERT INTO application_state VALUES ('preserved')").Error)
			provider := newMigrationProvider(t, sqlDB)
			pending, err := provider.HasPending(t.Context())
			require.NoError(t, err)
			require.True(t, pending)
			if rejectMigration {
				require.NoError(t, db.Exec(`CREATE TRIGGER reject_first_version BEFORE INSERT ON schema_migrations WHEN NEW.version_id = 1 BEGIN SELECT RAISE(ABORT, 'fixture migration version rejected'); END`).Error)
			}
			unlockErr := errors.New("fixture migration lock release failed")
			fileLock := &migrationReleaseFaultLock{Flock: flock.New(path + ".migration.lock"), unlockErr: unlockErr}
			t.Cleanup(func() { require.NoError(t, fileLock.Flock.Close()) })
			locker := &migrationReleaseObserver{Locker: &fileMigrationLocker{fileLock: fileLock}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			err = db.runMigrationsWithLocker(ctx, sqlDB, locker)
			if rejectMigration {
				require.ErrorContains(t, err, "database migration failed:")
				require.ErrorContains(t, err, "backup created at")
				require.NotErrorIs(t, err, unlockErr, "cleanup must not replace or join the original migration error")
				var sqliteErr sqlite3.Error
				require.ErrorAs(t, err, &sqliteErr)
				require.Equal(t, sqlite3.ErrConstraintTrigger, sqliteErr.ExtendedCode)
				require.EqualError(t, sqliteErr, "fixture migration version rejected")
				require.False(t, db.Migrator().HasTable("movies"), "the failed version insertion must roll back baseline table creation")
				var applied int
				require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version_id > 0").Scan(&applied))
				require.Zero(t, applied)
			} else {
				require.ErrorIs(t, err, unlockErr)
				require.EqualError(t, err, "release startup migration lock: "+unlockErr.Error())
				require.True(t, db.Migrator().HasTable("movies"), "release failure must not undo a committed migration")
			}
			require.Equal(t, []string{"lock", "unlock"}, fileLock.calls)
			require.Equal(t, 1, locker.unlockCalls, "the acquired startup lock must be released exactly once on either result")
			require.Same(t, sqlDB, locker.unlockDB)
			require.NotNil(t, locker.unlockContext)
			require.Nil(t, locker.unlockContext.Done(), "release must use a background context independent of the caller")
			require.NoError(t, locker.unlockContext.Err())
			require.True(t, fileLock.Locked())
			pending, err = provider.HasPending(t.Context())
			require.NoError(t, err)
			require.Equal(t, rejectMigration, pending)
			hash, err := GetStoredHash(sqlDB, "000001_baseline.sql")
			require.NoError(t, err)
			if rejectMigration {
				require.Empty(t, hash)
			} else {
				require.Len(t, hash, 64)
			}
			var value string
			require.NoError(t, sqlDB.QueryRow("SELECT value FROM application_state").Scan(&value))
			require.Equal(t, "preserved", value)
			backups, err := filepath.Glob(path + ".*.backup")
			require.NoError(t, err)
			require.Len(t, backups, 1)

			probe := flock.New(path + ".migration.lock")
			t.Cleanup(func() { require.NoError(t, probe.Close()) })
			acquired, err := probe.TryLock()
			require.NoError(t, err)
			require.False(t, acquired, "failed release must retain the actual file lock")
			// Release the injected failure's retained resource explicitly, rather
			// than changing the lock pathname or inventing a filesystem failure.
			require.NoError(t, fileLock.Flock.Unlock())
			acquired, err = probe.TryLock()
			require.NoError(t, err)
			require.True(t, acquired)
		})
	}
}

func TestStartupMigrationFailedAcquisitionDoesNotRelease(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	fileLock := &migrationReleaseFaultLock{Flock: flock.New(filepath.Join(t.TempDir(), "migration.lock"))}
	t.Cleanup(func() { require.NoError(t, fileLock.Flock.Close()) })
	locker := &migrationReleaseObserver{Locker: &fileMigrationLocker{fileLock: fileLock}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = db.runMigrationsWithLocker(ctx, sqlDB, locker)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualError(t, err, "acquire startup migration lock: context canceled")
	require.Equal(t, []string{"lock"}, fileLock.calls)
	require.Zero(t, locker.unlockCalls)
	require.False(t, fileLock.Locked())
	require.False(t, db.Migrator().HasTable("schema_migrations_hash"), "acquisition failure must stop before touching migration state")
	require.NoError(t, sqlDB.Ping())
}
