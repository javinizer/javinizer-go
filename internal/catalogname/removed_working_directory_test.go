package catalogname

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestLoadAfterWorkingDirectoryRemoved(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux removed-working-directory contract; Windows cannot remove its active working directory")
	}
	// Keep cwd-changing subtests serial. t.Chdir retains the old directory
	// handle, so its cleanup still restores cwd after the child is removed.
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	originalPWD, hadPWD := os.LookupEnv("PWD")
	parent := t.TempDir()
	emptyChild := filepath.Join(parent, "empty-cwd")
	require.Equal(t, parent, filepath.Dir(emptyChild))
	require.NoError(t, os.Mkdir(emptyChild, 0700))
	path := filepath.Join(parent, "catalog.db")
	actress := models.Actress{FirstName: "Fixture", LastName: "Performer", Origin: database.ActressOriginUser, Verified: true}
	func() {
		db, err := database.New(&database.Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
		require.NoError(t, err)
		defer func() { require.NoError(t, db.Close()) }()
		require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
		require.NoError(t, database.NewActressRepository(db).Create(t.Context(), &actress))
	}() // Close the writer before taking the stable main-file snapshot.
	require.NotZero(t, actress.ID)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	allowedFiles := []string{"catalog.db-wal", "catalog.db-shm"}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(emptyChild) {
			// Startup migrations may also have created a backup before Load runs.
			allowedFiles = append(allowedFiles, entry.Name())
		}
	}
	baseline, err := Load(t.Context(), path)
	require.NoError(t, err)
	want := baseline.Resolve("Fixture Performer")
	require.True(t, want.Resolved)
	require.Equal(t, actress.ID, want.CatalogID)
	require.Equal(t, actress.FirstName, want.FirstName)
	require.Equal(t, actress.LastName, want.LastName)
	require.Equal(t, "catalog_consistent", want.Confidence)
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, before, after, "loading must not change persisted catalog rows or schema")
		require.Equal(t, want, baseline.Resolve("Fixture Performer"), "the earlier snapshot stays detached")
	}
	assertLoad := func(t *testing.T, input string) {
		t.Helper()
		snapshot, err := Load(t.Context(), input)
		require.NoError(t, err)
		require.NotNil(t, snapshot)
		require.Equal(t, want, snapshot.Resolve("Fixture Performer"))
		assertUnchanged(t)
	}
	assertRestored := func() {
		t.Helper()
		wd, err := os.Getwd()
		require.NoError(t, err)
		require.Equal(t, originalWD, wd)
		pwd, present := os.LookupEnv("PWD")
		require.Equal(t, hadPWD, present)
		require.Equal(t, originalPWD, pwd)
	}
	t.Run("unavailable cwd rejects relative catalog without initializing it", func(t *testing.T) {
		t.Chdir(emptyChild)
		require.NoError(t, os.Remove(emptyChild), "remove only the newly created empty child")
		wd, err := os.Getwd()
		require.Empty(t, wd)
		require.ErrorIs(t, err, os.ErrNotExist)
		snapshot, err := Load(t.Context(), "catalog.db")
		require.Nil(t, snapshot)
		require.ErrorIs(t, err, os.ErrNotExist)
		require.ErrorContains(t, err, "catalog path:")
		require.NotContains(t, err.Error(), "catalog file:", "reject during path admission, not later stat")
		assertUnchanged(t)
		assertLoad(t, path)
	})
	assertRestored()
	t.Run("relative catalog recovers from a surviving directory", func(t *testing.T) {
		t.Chdir(parent)
		assertLoad(t, "catalog.db")
	})
	assertRestored()
	assertUnchanged(t)
	require.NoDirExists(t, emptyChild)
	entries, err = os.ReadDir(parent)
	require.NoError(t, err)
	for _, entry := range entries {
		// Read-only SQLite may use WAL locking sidecars; forbid unexpected files,
		// not the documented shared-memory behavior of a coherent WAL reader.
		require.Contains(t, allowedFiles, entry.Name())
		require.False(t, entry.IsDir())
	}
}
