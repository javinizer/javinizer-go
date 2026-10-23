package word

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestReplacementDatabaseRejectionAndImportContinuation(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(dir, "database.sqlite")
	cfg.System.TempDir = filepath.Join(dir, "temp")
	cfg.System.VersionCheckEnabled = false
	cfg.Logging.Output = "stdout"
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, config.Save(cfg, path))
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	deps, err := commandutil.NewDependencies(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, deps.Close()) })
	repo := database.NewWordReplacementRepository(deps.DB)
	require.NoError(t, repo.Upsert(context.Background(), &models.WordReplacement{Original: "protected-fixture", Replacement: "kept"}))
	// SQLite policies reject only the fixture; normal startup seeding is unaffected.
	require.NoError(t, deps.DB.Exec(`CREATE TRIGGER reject_fixture_insert BEFORE INSERT ON word_replacements WHEN NEW.original = 'rejected-fixture' BEGIN SELECT RAISE(ABORT, 'replacement policy'); END`).Error)
	require.NoError(t, deps.DB.Exec(`CREATE TRIGGER reject_fixture_delete BEFORE DELETE ON word_replacements WHEN OLD.original = 'protected-fixture' BEGIN SELECT RAISE(ABORT, 'replacement policy'); END`).Error)
	cmd := &cobra.Command{}
	err = runWordAdd(cmd, []string{"rejected-fixture", "not stored"}, path)
	require.EqualError(t, err, "failed to add word replacement: create word replacement rejected-fixture: replacement policy")
	err = runWordRemove(cmd, []string{"protected-fixture"}, path)
	require.EqualError(t, err, "failed to remove word replacement: delete word replacement protected-fixture: replacement policy")
	kept, err := repo.FindByOriginal(context.Background(), "protected-fixture")
	require.NoError(t, err)
	require.Equal(t, "kept", kept.Replacement)
	input := filepath.Join(dir, "import.json")
	require.NoError(t, os.WriteFile(input, []byte(`[{"original":"rejected-fixture","replacement":"not stored"},{"original":"accepted-fixture","replacement":"stored"}]`), 0600))
	var commandErr error
	output, _ := testutil.CaptureOutput(t, func() { commandErr = runWordImport(cmd, []string{input}, path) })
	require.NoError(t, commandErr)
	require.Contains(t, output, "Imported: 1, Skipped: 0, Errors: 1")
	_, err = repo.FindByOriginal(context.Background(), "rejected-fixture")
	require.True(t, database.IsNotFound(err))
	accepted, err := repo.FindByOriginal(context.Background(), "accepted-fixture")
	require.NoError(t, err)
	require.Equal(t, "stored", accepted.Replacement)
	// Exporting to a directory fails without changing stored mappings.
	err = runWordExport(cmd, []string{dir}, path)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, dir, pathErr.Path)
	require.ErrorContains(t, err, "failed to write file:")
	kept, err = repo.FindByOriginal(context.Background(), "protected-fixture")
	require.NoError(t, err)
	require.Equal(t, "kept", kept.Replacement)
}
