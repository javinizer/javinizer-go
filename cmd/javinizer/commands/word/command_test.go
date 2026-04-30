package word_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/word"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	fn()

	require.NoError(t, wOut.Close())
	require.NoError(t, wErr.Close())

	return <-outC, <-errC
}

func setupWordTestDB(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "data", "test.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))

	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(testCfg, configPath))

	db, err := database.New(testCfg)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate())
	require.NoError(t, db.Close())

	return configPath, dbPath
}

func TestWordList_Success(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "add", "TestWord", "ReplacedWord"})
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := word.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"word", "add", "AnotherWord", "AnotherReplacement"})
	captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := word.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"word", "list"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "=== Word Replacements ===")
	assert.Contains(t, stdout, "TestWord")
	assert.Contains(t, stdout, "ReplacedWord")
	assert.Contains(t, stdout, "AnotherWord")
	assert.Contains(t, stdout, "AnotherReplacement")
	assert.Contains(t, stdout, "Total: 2 replacements")
}

func TestWordList_Empty(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "list"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "No word replacements configured")
}

func TestWordAdd_Remove_Roundtrip(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "add", "MyWord", "MyReplacement"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout, "Word replacement added")

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := word.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"word", "list"})
	stdout2, _ := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout2, "MyWord")
	assert.Contains(t, stdout2, "MyReplacement")

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := word.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"word", "remove", "MyWord"})
	stdout3, _ := captureOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout3, "Word replacement removed")

	var found models.WordReplacement
	err = db.DB.Where("original = ?", "MyWord").First(&found).Error
	assert.Error(t, err, "word replacement should be removed")
}

func TestWordExport_WithData(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "add", "WordA", "A"})
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := word.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"word", "add", "WordB", "B"})
	captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	rootCmd3 := &cobra.Command{Use: "root"}
	rootCmd3.PersistentFlags().String("config", configPath, "config file")
	cmd3 := word.NewCommand()
	rootCmd3.AddCommand(cmd3)

	rootCmd3.SetArgs([]string{"word", "export"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd3.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, `"original": "WordA"`)
	assert.Contains(t, stdout, `"replacement": "A"`)
	assert.Contains(t, stdout, `"original": "WordB"`)
	assert.Contains(t, stdout, "Exported 2 word replacement(s)")
}

func TestWordExport_Empty(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "export"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "[]")
	assert.Contains(t, stdout, "Exported 0 word replacement(s)")
}

func TestWordImport_Valid(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "words.json")
	importData := []byte(`[
		{"original": "ImportA", "replacement": "A"},
		{"original": "ImportB", "replacement": "B"}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "import", importPath})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Imported: 2")
	assert.Contains(t, stdout, "Skipped: 0")
	assert.Contains(t, stdout, "Errors: 0")

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewWordReplacementRepository(db)
	replacements, err := repo.List()
	require.NoError(t, err)
	assert.Equal(t, 2, len(replacements))
}

func TestWordImport_SkipsDefaults(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "words.json")
	importData := []byte(`[
		{"original": "F***", "replacement": "Custom"},
		{"original": "CustomWord", "replacement": "CustomReplacement"}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "import", importPath})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Imported: 1")
	assert.Contains(t, stdout, "Skipped: 1")

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewWordReplacementRepository(db)
	replacements, err := repo.List()
	require.NoError(t, err)
	assert.Equal(t, 1, len(replacements))
	assert.Equal(t, "CustomWord", replacements[0].Original)
}

func TestWordImport_IncludeDefaults(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "words.json")
	importData := []byte(`[
		{"original": "F***", "replacement": "CustomOverride"},
		{"original": "CustomWord", "replacement": "CustomReplacement"}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "import", importPath, "--include-defaults"})
	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Imported: 2")
	assert.Contains(t, stdout, "Skipped: 0")

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewWordReplacementRepository(db)
	replacements, err := repo.List()
	require.NoError(t, err)
	assert.Equal(t, 2, len(replacements))
}

func TestWordImport_InvalidJSON(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`{bad}`), 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "import", importPath})

	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse JSON")
	})
}

func TestWordImport_EmptyArray(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "empty.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`[]`), 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "import", importPath})

	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no word replacements found")
	})
}

func TestWordExportImport_Roundtrip(t *testing.T) {
	configPath, _ := setupWordTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := word.NewCommand()
	rootCmd.AddCommand(cmd)

	rootCmd.SetArgs([]string{"word", "add", "TestExport", "Exported"})
	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	tmpDir := t.TempDir()
	exportPath := filepath.Join(tmpDir, "words.json")

	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")
	cmd2 := word.NewCommand()
	rootCmd2.AddCommand(cmd2)

	rootCmd2.SetArgs([]string{"word", "export", exportPath})
	captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})

	fileData, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	assert.Contains(t, string(fileData), `"original": "TestExport"`)
	assert.Contains(t, string(fileData), `"replacement": "Exported"`)
}
