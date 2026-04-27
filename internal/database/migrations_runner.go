package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/javinizer/javinizer-go/internal/configutil"
	dbmigrations "github.com/javinizer/javinizer-go/internal/database/migrations"
	"github.com/pressly/goose/v3"
	gooselock "github.com/pressly/goose/v3/lock"
)

const (
	schemaMigrationsTable = "schema_migrations"
	migrationLockRetry    = 250 * time.Millisecond
)

var startupMigrationMu sync.Mutex

// RunMigrationsOnStartup applies all pending versioned database migrations.
//
// The migration flow is fail-fast:
// - acquire startup migration lock
// - check pending migrations
// - create a pre-migration SQLite backup snapshot when pending work exists
// - apply migrations
func (db *DB) RunMigrationsOnStartup(ctx context.Context) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	sqlDB, err := db.DB.DB()
	if err != nil {
		return fmt.Errorf("get sql database handle: %w", err)
	}

	if err := EnsureMigrationHashTable(sqlDB); err != nil {
		return fmt.Errorf("ensure migration hash table: %w", err)
	}

	migrationLocker, err := newStartupMigrationLocker(db.dsn)
	if err != nil {
		return fmt.Errorf("initialize startup migration lock: %w", err)
	}
	if err := migrationLocker.Lock(ctx, sqlDB); err != nil {
		return fmt.Errorf("acquire startup migration lock: %w", err)
	}
	defer func() {
		unlockErr := migrationLocker.Unlock(context.Background(), sqlDB)
		if err == nil && unlockErr != nil {
			err = fmt.Errorf("release startup migration lock: %w", unlockErr)
		}
	}()

	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		sqlDB,
		dbmigrations.Filesystem(),
		goose.WithTableName(schemaMigrationsTable),
		goose.WithGoMigrations(dbmigrations.GoMigrations()...),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("initialize migration provider: %w", err)
	}

	pending, err := provider.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("check pending migrations: %w", err)
	}

	backupPath := ""
	if pending {
		backupPath, err = createSQLiteBackupSnapshot(ctx, sqlDB, db.dsn)
		if err != nil {
			return fmt.Errorf("create pre-migration backup: %w", err)
		}
	}

	baselineContent, err := fs.ReadFile(dbmigrations.Filesystem(), "000001_baseline.sql")
	if err != nil {
		return fmt.Errorf("read baseline migration: %w", err)
	}
	baselineHash := ComputeMigrationHash(baselineContent)

	storedHash, err := GetStoredHash(sqlDB, "000001_baseline.sql")
	if err != nil {
		return fmt.Errorf("get stored baseline hash: %w", err)
	}

	if storedHash != "" && storedHash != baselineHash {
		return fmt.Errorf(
			"baseline migration hash mismatch: stored=%s current=%s. "+
				"Migration file was modified after being applied. "+
				"Manual intervention required",
			storedHash[:12], baselineHash[:12],
		)
	}

	if _, err := provider.Up(ctx); err != nil {
		if backupPath != "" {
			restoreTarget := db.dsn
			if parsedPath, ok := sqliteFilePathFromDSN(db.dsn); ok {
				restoreTarget = parsedPath
			}
			return fmt.Errorf(
				"database migration failed: %w (backup created at %q; restore with: cp %q %q)",
				err,
				backupPath,
				backupPath,
				restoreTarget,
			)
		}
		return fmt.Errorf("database migration failed: %w", err)
	}

	if storedHash == "" {
		if err := StoreMigrationHash(sqlDB, "000001_baseline.sql", baselineHash); err != nil {
			return fmt.Errorf("store baseline hash: %w", err)
		}
	}

	return nil
}

type processMigrationLocker struct{}

func (processMigrationLocker) Lock(_ context.Context, _ *sql.DB) error {
	startupMigrationMu.Lock()
	return nil
}

func (processMigrationLocker) Unlock(_ context.Context, _ *sql.DB) error {
	startupMigrationMu.Unlock()
	return nil
}

type fileMigrationLocker struct {
	fileLock *flock.Flock
}

func (l *fileMigrationLocker) Lock(ctx context.Context, _ *sql.DB) error {
	locked, err := l.fileLock.TryLockContext(ctx, migrationLockRetry)
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("unable to acquire file lock %q", l.fileLock.Path())
	}
	return nil
}

func (l *fileMigrationLocker) Unlock(_ context.Context, _ *sql.DB) error {
	if err := l.fileLock.Unlock(); err != nil {
		return err
	}
	return l.fileLock.Close()
}

func newStartupMigrationLocker(dsn string) (gooselock.Locker, error) {
	dbPath, ok := sqliteFilePathFromDSN(dsn)
	if !ok {
		return processMigrationLocker{}, nil
	}
	lockPath := dbPath + ".migration.lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), configutil.DirPerm); err != nil {
		return nil, fmt.Errorf("create migration lock directory: %w", err)
	}
	return &fileMigrationLocker{fileLock: flock.New(lockPath)}, nil
}

func createSQLiteBackupSnapshot(ctx context.Context, db *sql.DB, dsn string) (string, error) {
	dbPath, ok := sqliteFilePathFromDSN(dsn)
	if !ok {
		return "", nil
	}

	backupPath := fmt.Sprintf("%s.%s.backup", dbPath, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(filepath.Dir(backupPath), configutil.DirPerm); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}

	query := fmt.Sprintf("VACUUM INTO %s", quoteSQLiteStringLiteral(backupPath))
	if _, err := db.ExecContext(ctx, query); err != nil {
		return "", fmt.Errorf("sqlite vacuum into backup file: %w", err)
	}

	return backupPath, nil
}

func sqliteFilePathFromDSN(dsn string) (string, bool) {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return "", false
	}

	lower := strings.ToLower(trimmed)
	if lower == ":memory:" || strings.HasPrefix(lower, "file::memory:") || strings.Contains(lower, "mode=memory") {
		return "", false
	}

	if strings.HasPrefix(lower, "file:") {
		pathPart := trimmed[len("file:"):]
		if idx := strings.Index(pathPart, "?"); idx >= 0 {
			pathPart = pathPart[:idx]
		}
		if pathPart == "" {
			return "", false
		}
		if unescaped, err := url.PathUnescape(pathPart); err == nil {
			pathPart = unescaped
		}
		return pathPart, true
	}

	pathPart := trimmed
	if idx := strings.Index(pathPart, "?"); idx >= 0 {
		pathPart = pathPart[:idx]
	}
	return pathPart, true
}

func quoteSQLiteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
