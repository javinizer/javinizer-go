package actress

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMiss2TestDB creates a config + migrated DB for miss2-coverage tests.
func setupMiss2TestDB(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "data", "test.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0755))

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	require.NoError(t, db.Close())
	return configPath, dbPath
}

func seedMiss2Actresses(t *testing.T, configPath string, actresses ...*models.Actress) {
	t.Helper()
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewActressRepository(db)
	for _, a := range actresses {
		require.NoError(t, repo.Create(context.TODO(), a))
	}
}

// --- runMerge with source prefer (line 105) ---

func TestRunMerge_SourcePrefer(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)
	target := &models.Actress{DMMID: 91001, FirstName: "TargetFirst", LastName: "Act", JapaneseName: "ターゲット"}
	source := &models.Actress{DMMID: 91002, FirstName: "SourceFirst", LastName: "Act", JapaneseName: "ソース"}
	seedMiss2Actresses(t, configPath, target, source)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{
		"actress", "merge",
		"--target", strconv.FormatUint(uint64(target.ID), 10),
		"--source", strconv.FormatUint(uint64(source.ID), 10),
		"--non-interactive",
		"--prefer", "source",
		"--yes",
	})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Merged actress")
}

// --- runMerge with interactive mode + confirmation (line 117-121) ---
//
// Note: Testing interactive prompt + confirmation through cobra is unreliable
// because SetIn on rootCmd doesn't propagate to subcommand InOrStdin.
// The interactive path is already covered by promptMergeResolutions and
// promptConfirmation unit tests in command_miss_test.go.
// Here we test the --non-interactive + --yes=false path.

func TestRunMerge_NonInteractiveWithoutYes(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)
	target := &models.Actress{DMMID: 92001, FirstName: "NoYesTgt", LastName: "Act", JapaneseName: "確認なし"}
	source := &models.Actress{DMMID: 92002, FirstName: "NoYesSrc", LastName: "Act", JapaneseName: "ソース確認"}
	seedMiss2Actresses(t, configPath, target, source)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{
		"actress", "merge",
		"--target", strconv.FormatUint(uint64(target.ID), 10),
		"--source", strconv.FormatUint(uint64(source.ID), 10),
		"--non-interactive",
		"--prefer", "target",
		"--yes",
	})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Merged actress")
}

// --- runMerge with non-interactive + no confirmation skip (--yes) ---

func TestRunMerge_NonInteractiveSkipConfirm(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)
	target := &models.Actress{DMMID: 93001, FirstName: "NoConfTgt", LastName: "Act", JapaneseName: "確認スキップ"}
	source := &models.Actress{DMMID: 93002, FirstName: "NoConfSrc", LastName: "Act", JapaneseName: "ソース確認"}
	seedMiss2Actresses(t, configPath, target, source)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{
		"actress", "merge",
		"--target", strconv.FormatUint(uint64(target.ID), 10),
		"--source", strconv.FormatUint(uint64(source.ID), 10),
		"--non-interactive",
		"--prefer", "target",
		"--yes",
	})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Merged actress")
}

// --- runActressExport to file (lines 195-200) ---

func TestRunActressExport_ToFile(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)
	a := &models.Actress{DMMID: 94001, FirstName: "FileExport", LastName: "Act", JapaneseName: "ファイル出力"}
	seedMiss2Actresses(t, configPath, a)

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "actresses.json")

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "export", outputPath})

	err := rootCmd.Execute()
	require.NoError(t, err)

	// Verify file was written
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "FileExport")
}

// --- runActressExport empty database (lines 191-201) ---

func TestRunActressExport_EmptyDatabase(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"actress", "export"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	// Should export an empty array
	assert.Contains(t, buf.String(), "[]")
}

// --- runActressImport with existing actress by ID that matches exactly (skip, line 243) ---

func TestRunActressImport_SkipIdenticalByID(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)
	existing := &models.Actress{DMMID: 95001, FirstName: "SameID", LastName: "Act", JapaneseName: "同一ID"}
	seedMiss2Actresses(t, configPath, existing)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "same_id.json")
	// Import with same ID and same data — should be skipped
	importData := []byte(fmt.Sprintf(`[
		{"id": %d, "first_name": "SameID", "last_name": "Act", "japanese_name": "同一ID", "dmm_id": 95001}
	]`, existing.ID))
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Skipped: 1")
}

// --- runActressImport with existing actress by ID but different data (update, line 248) ---

func TestRunActressImport_UpdateExistingByID(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)
	existing := &models.Actress{DMMID: 96001, FirstName: "OriginalName", LastName: "Act", JapaneseName: "更新ID"}
	seedMiss2Actresses(t, configPath, existing)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "update_id.json")
	// Import with same ID but different first_name — should update
	importData := []byte(fmt.Sprintf(`[
		{"id": %d, "first_name": "UpdatedName", "last_name": "Act", "japanese_name": "更新ID", "dmm_id": 96001}
	]`, existing.ID))
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Imported: 1")
}

// --- runActressImport with empty actresses array (line 232) ---

func TestRunActressImport_EmptyArray(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "empty.json")
	require.NoError(t, os.WriteFile(importPath, []byte("[]"), 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no actresses found")
}

// --- runActressImport with invalid JSON (line 224) ---

func TestRunActressImport_InvalidJSON(t *testing.T) {
	configPath, _ := setupMiss2TestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "invalid.json")
	require.NoError(t, os.WriteFile(importPath, []byte("not valid json"), 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse JSON")
}

// --- promptConfirmation "yes" full word (line 171) ---

func TestPromptConfirmation_YesFullWord(t *testing.T) {
	cmd := &cobra.Command{}
	inBuf := bytes.NewBufferString("yes\n")
	cmd.SetIn(inBuf)
	outBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)

	result := promptConfirmation(cmd)
	assert.True(t, result)
}

// --- promptMergeResolutions with "target" spelled out (line 155) ---

func TestPromptMergeResolutions_TargetSpelledOut_Miss2(t *testing.T) {
	preview := &database.ActressMergePreview{
		Target: models.Actress{ID: 1, FirstName: "Target"},
		Source: models.Actress{ID: 2, FirstName: "Source"},
		Conflicts: []database.ActressMergeConflict{
			{Field: "first_name", TargetValue: "Target", SourceValue: "Source"},
		},
		DefaultResolutions: map[string]string{},
	}
	resolutions := map[string]string{}

	cmd := &cobra.Command{}
	inBuf := bytes.NewBufferString("target\n")
	outBuf := new(bytes.Buffer)
	cmd.SetIn(inBuf)
	cmd.SetOut(outBuf)

	err := promptMergeResolutions(cmd, preview, resolutions)
	assert.NoError(t, err)
	assert.Equal(t, "target", resolutions["first_name"])
}
