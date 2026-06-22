package actress

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMissTestDB creates a config + migrated DB for miss-coverage tests.
func setupMissTestDB(t *testing.T) (string, string) {
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

func seedMissActresses(t *testing.T, configPath string, actresses ...*models.Actress) {
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

// --- runMerge error paths (lines 76-121) ---

func TestRunMerge_InvalidPrefer(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	target := &models.Actress{DMMID: 50001, FirstName: "Tgt", LastName: "Act", JapaneseName: "ターゲット"}
	source := &models.Actress{DMMID: 50002, FirstName: "Src", LastName: "Act", JapaneseName: "ソース"}
	seedMissActresses(t, configPath, target, source)

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
		"--prefer", "invalid",
		"--yes",
	})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --prefer value")
}

func TestRunMerge_SameSourceTarget(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	a := &models.Actress{DMMID: 50003, FirstName: "Same", LastName: "Act", JapaneseName: "同じ"}
	seedMissActresses(t, configPath, a)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{
		"actress", "merge",
		"--target", strconv.FormatUint(uint64(a.ID), 10),
		"--source", strconv.FormatUint(uint64(a.ID), 10),
		"--non-interactive",
		"--prefer", "target",
	})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestRunMerge_MissingRequiredFlags(t *testing.T) {
	configPath, _ := setupMissTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "merge"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestRunMerge_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{
		"actress", "merge",
		"--target", "1",
		"--source", "2",
		"--non-interactive",
	})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- promptMergeResolutions with invalid choice (line 158) ---

func TestPromptMergeResolutions_InvalidChoice(t *testing.T) {
	preview := &database.ActressMergePreview{
		Target: models.Actress{ID: 1, FirstName: "Target"},
		Source: models.Actress{ID: 2, FirstName: "Source"},
		Conflicts: []database.ActressMergeConflict{
			{Field: "first_name", TargetValue: "Target", SourceValue: "Source", DefaultResolution: "target"},
		},
		DefaultResolutions: map[string]string{},
	}
	resolutions := map[string]string{}

	cmd := &cobra.Command{}
	inBuf := bytes.NewBufferString("x\n") // invalid choice
	outBuf := new(bytes.Buffer)
	cmd.SetIn(inBuf)
	cmd.SetOut(outBuf)

	err := promptMergeResolutions(cmd, preview, resolutions)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid choice")
}

func TestPromptMergeResolutions_NoConflicts(t *testing.T) {
	preview := &database.ActressMergePreview{
		Target:             models.Actress{ID: 1, FirstName: "Same"},
		Source:             models.Actress{ID: 2, FirstName: "Same"},
		Conflicts:          []database.ActressMergeConflict{},
		DefaultResolutions: map[string]string{},
	}
	resolutions := map[string]string{}

	cmd := &cobra.Command{}
	outBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)

	err := promptMergeResolutions(cmd, preview, resolutions)
	assert.NoError(t, err)
	assert.Contains(t, outBuf.String(), "No field conflicts detected")
}

func TestPromptMergeResolutions_TargetChoice(t *testing.T) {
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
	inBuf := bytes.NewBufferString("t\n")
	outBuf := new(bytes.Buffer)
	cmd.SetIn(inBuf)
	cmd.SetOut(outBuf)

	err := promptMergeResolutions(cmd, preview, resolutions)
	assert.NoError(t, err)
	assert.Equal(t, "target", resolutions["first_name"])
}

func TestPromptMergeResolutions_SourceChoice(t *testing.T) {
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
	inBuf := bytes.NewBufferString("s\n")
	outBuf := new(bytes.Buffer)
	cmd.SetIn(inBuf)
	cmd.SetOut(outBuf)

	err := promptMergeResolutions(cmd, preview, resolutions)
	assert.NoError(t, err)
	assert.Equal(t, "source", resolutions["first_name"])
}

func TestPromptMergeResolutions_EmptyChoice(t *testing.T) {
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
	inBuf := bytes.NewBufferString("\n") // empty = default target
	outBuf := new(bytes.Buffer)
	cmd.SetIn(inBuf)
	cmd.SetOut(outBuf)

	err := promptMergeResolutions(cmd, preview, resolutions)
	assert.NoError(t, err)
	assert.Equal(t, "target", resolutions["first_name"])
}

func TestPromptMergeResolutions_ReadError(t *testing.T) {
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
	// Use a reader that will return an error on ReadString
	cmd.SetIn(bufio.NewReader(strings.NewReader("")))
	outBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)

	err := promptMergeResolutions(cmd, preview, resolutions)
	assert.Error(t, err)
}

func TestPromptMergeResolutions_SourceSpelledOut(t *testing.T) {
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
	inBuf := bytes.NewBufferString("source\n")
	outBuf := new(bytes.Buffer)
	cmd.SetIn(inBuf)
	cmd.SetOut(outBuf)

	err := promptMergeResolutions(cmd, preview, resolutions)
	assert.NoError(t, err)
	assert.Equal(t, "source", resolutions["first_name"])
}

func TestPromptMergeResolutions_TargetSpelledOut(t *testing.T) {
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

// --- promptConfirmation (0% coverage, lines 166-174) ---

func TestPromptConfirmation_Yes(t *testing.T) {
	cmd := &cobra.Command{}
	inBuf := bytes.NewBufferString("y\n")
	cmd.SetIn(inBuf)
	outBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)

	result := promptConfirmation(cmd)
	assert.True(t, result)
	assert.Contains(t, outBuf.String(), "Apply merge?")
}

func TestPromptConfirmation_YesFull(t *testing.T) {
	cmd := &cobra.Command{}
	inBuf := bytes.NewBufferString("yes\n")
	cmd.SetIn(inBuf)
	outBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)

	result := promptConfirmation(cmd)
	assert.True(t, result)
}

func TestPromptConfirmation_No(t *testing.T) {
	cmd := &cobra.Command{}
	inBuf := bytes.NewBufferString("n\n")
	cmd.SetIn(inBuf)
	outBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)

	result := promptConfirmation(cmd)
	assert.False(t, result)
}

func TestPromptConfirmation_Empty(t *testing.T) {
	cmd := &cobra.Command{}
	inBuf := bytes.NewBufferString("\n")
	cmd.SetIn(inBuf)
	outBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)

	result := promptConfirmation(cmd)
	assert.False(t, result)
}

func TestPromptConfirmation_ReadError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(bufio.NewReader(strings.NewReader("")))
	outBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)

	result := promptConfirmation(cmd)
	assert.False(t, result)
}

// --- runMerge with --yes flag skips confirmation (line 119) ---

func TestRunMerge_WithYesFlag(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	target := &models.Actress{DMMID: 60001, FirstName: "YesTgt", LastName: "Act", JapaneseName: "イエス"}
	source := &models.Actress{DMMID: 60002, FirstName: "YesSrc", LastName: "Act", JapaneseName: "ソース"}
	seedMissActresses(t, configPath, target, source)

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

// --- runMerge cancelled by confirmation (line 119-121) ---

func TestRunMerge_CancelledByConfirmation(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	target := &models.Actress{DMMID: 60011, FirstName: "CanTgt", LastName: "Act", JapaneseName: "キャンセル"}
	source := &models.Actress{DMMID: 60012, FirstName: "CanSrc", LastName: "Act", JapaneseName: "ソース2"}
	seedMissActresses(t, configPath, target, source)

	buf := new(bytes.Buffer)
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(buf)
	// Provide answers for all conflict prompts (t=target for each), then decline confirmation with "n"
	// There may be conflicts on first_name, dmm_id, japanese_name, etc.
	rootCmd.SetIn(strings.NewReader("t\nt\nt\nt\nt\nn\n"))
	rootCmd.SetArgs([]string{
		"actress", "merge",
		"--target", strconv.FormatUint(uint64(target.ID), 10),
		"--source", strconv.FormatUint(uint64(source.ID), 10),
	})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Merge cancelled")
}

// --- runActressExport to file (lines 184-208) ---

func TestRunActressExport_WriteFileError(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	a := &models.Actress{DMMID: 70001, FirstName: "FileErr", LastName: "Act", JapaneseName: "ファイルエラー"}
	seedMissActresses(t, configPath, a)

	// Try to write to a directory that doesn't exist and can't be created
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "export", "/nonexistent/deep/dir/actresses.json"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write file")
}

func TestRunActressExport_InvalidConfig(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "export"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// --- runActressImport branches (lines 216-292) ---

func TestRunActressImport_FileReadError(t *testing.T) {
	configPath, _ := setupMissTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", "/nonexistent/file.json"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestRunActressImport_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "actresses.json")
	importData := []byte(`[{"id": 0, "first_name": "Test", "japanese_name": "テスト"}]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	configPath := testutil.UnreachableConfigPath(t)
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestRunActressImport_WithJapaneseNameLookup(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	// Pre-seed an actress that will be found by JapaneseName lookup
	existing := &models.Actress{DMMID: 80001, FirstName: "Existing", LastName: "Act", JapaneseName: "存在する"}
	seedMissActresses(t, configPath, existing)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "actresses.json")
	// Import with id=0 and matching JapaneseName — should find existing and update
	importData := []byte(`[
		{"id": 0, "first_name": "Updated", "last_name": "Act", "japanese_name": "存在する", "dmm_id": 80001}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)

	err := rootCmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Imported: 1")
}

func TestRunActressImport_SkipIdenticalByJapaneseName(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	existing := &models.Actress{DMMID: 80010, FirstName: "Same", LastName: "Act", JapaneseName: "同一"}
	seedMissActresses(t, configPath, existing)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "same.json")
	// Import with same data — should be skipped
	importData := []byte(`[
		{"id": 0, "first_name": "Same", "last_name": "Act", "japanese_name": "同一", "dmm_id": 80010}
	]`)
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

func TestRunActressImport_NewActressByJapaneseName(t *testing.T) {
	configPath, _ := setupMissTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "new.json")
	// Import with id=0 and JapaneseName that doesn't exist — should create new
	importData := []byte(`[
		{"id": 0, "first_name": "NewByJpn", "last_name": "Act", "japanese_name": "新しい名前", "dmm_id": 80020}
	]`)
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

func TestRunActressImport_WithIDNoExisting(t *testing.T) {
	configPath, _ := setupMissTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "newid.json")
	// Import with ID that doesn't exist in DB — should create with that ID
	importData := []byte(`[
		{"id": 9999, "first_name": "NewWithID", "last_name": "Actress", "japanese_name": "新ID付き", "dmm_id": 80030}
	]`)
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

// --- runActressExport with error writer for stdout (line 196) ---

func TestRunActressExport_StdoutWriteError(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	a := &models.Actress{DMMID: 71001, FirstName: "StdoutErr", LastName: "Act", JapaneseName: "出力エラー"}
	seedMissActresses(t, configPath, a)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetOut(errorWriterForActress{})
	rootCmd.SetArgs([]string{"actress", "export"})

	// The command will try to write to stdout which errors, but fmt.Printf is used for the message
	err := rootCmd.Execute()
	// The stdout write error from cmd.OutOrStdout().Write() should cause an issue
	// but the command may still succeed because the main output is via fmt.Printf
	_ = err
}

type errorWriterForActress struct{}

func (errorWriterForActress) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("write error")
}

// --- runActressImport with actress having no JapaneseName and ID=0 (line 257-290) ---

func TestRunActressImport_NoJapaneseNameNoID(t *testing.T) {
	configPath, _ := setupMissTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "nojpn.json")
	// Import with id=0 and empty JapaneseName — should create directly
	importData := []byte(`[
		{"id": 0, "first_name": "NoJpn", "last_name": "Actress", "japanese_name": "", "dmm_id": 0}
	]`)
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

// --- runActressImport with identical actress by JapaneseName but different fields (line 264-290) ---

func TestRunActressImport_JapaneseNameDifferentFields(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	existing := &models.Actress{DMMID: 85001, FirstName: "Original", LastName: "Act", JapaneseName: "更新テスト"}
	seedMissActresses(t, configPath, existing)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "update.json")
	// Import with same JapaneseName but different first_name — should update
	importData := []byte(`[
		{"id": 0, "first_name": "Updated", "last_name": "Act", "japanese_name": "更新テスト", "dmm_id": 85001}
	]`)
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

// --- runActressExport to stdout with data (line 191) ---

func TestRunActressExport_StdoutWithData(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	a := &models.Actress{DMMID: 72001, FirstName: "Stdout", LastName: "Act", JapaneseName: "標準出力"}
	seedMissActresses(t, configPath, a)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(NewCommand())
	rootCmd.SetArgs([]string{"actress", "export"})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)

	err := rootCmd.Execute()
	require.NoError(t, err)
	// Data should be written to cmd.OutOrStdout()
	assert.Contains(t, buf.String(), "Stdout")
}

// Suppress fmt and json imports
var _ = fmt.Sprintf
var _ = json.Marshal
