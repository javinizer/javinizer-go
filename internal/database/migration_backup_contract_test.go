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

// Startup creates the lock directory first and the backup directory second.
// Delegate all real filesystem work except that specific backup-stage call.
type rejectBackupDirectoryFS struct {
	afero.Fs
	mkdirCalls int
}

func (fs *rejectBackupDirectoryFS) MkdirAll(path string, mode os.FileMode) error {
	fs.mkdirCalls++
	if fs.mkdirCalls == 2 {
		return &os.PathError{Op: "mkdir", Path: path, Err: os.ErrPermission}
	}
	return fs.Fs.MkdirAll(path, mode)
}

func TestMigrationBackupFailurePreservesPendingDatabaseAndReleasesLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Exec("CREATE TABLE application_state (value TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO application_state VALUES ('preserved')").Error)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	// Initialize bookkeeping before the snapshot; the runner may ensure these
	// tables before discovering that a backup cannot be created.
	require.NoError(t, ensureMigrationHashTable(t.Context(), pool))
	provider := newMigrationProvider(t, pool)
	pending, err := provider.HasPending(t.Context())
	require.NoError(t, err)
	require.True(t, pending)
	before := previewDatabaseState(t, db)
	fs := &rejectBackupDirectoryFS{Fs: afero.NewOsFs()}
	db.fs = fs

	err = db.RunMigrationsOnStartup(t.Context())
	require.ErrorIs(t, err, os.ErrPermission)
	require.ErrorContains(t, err, "create pre-migration backup: create backup directory:")
	require.Equal(t, 2, fs.mkdirCalls, "the migration lock directory must succeed before the backup fault")
	require.Equal(t, before, previewDatabaseState(t, db))
	require.False(t, db.Migrator().HasTable("movies"))
	pending, err = provider.HasPending(t.Context())
	require.NoError(t, err)
	require.True(t, pending)
	backups, err := filepath.Glob(path + ".*.backup")
	require.NoError(t, err)
	require.Empty(t, backups)

	db.fs = afero.NewOsFs()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, db.RunMigrationsOnStartup(ctx), "the failed backup must release the startup lock for a later attempt")
	pending, err = newMigrationProvider(t, pool).HasPending(t.Context())
	require.NoError(t, err)
	require.False(t, pending)
	var value string
	require.NoError(t, db.Raw("SELECT value FROM application_state").Scan(&value).Error)
	require.Equal(t, "preserved", value)
	backups, err = filepath.Glob(path + ".*.backup")
	require.NoError(t, err)
	require.Len(t, backups, 1)
}
