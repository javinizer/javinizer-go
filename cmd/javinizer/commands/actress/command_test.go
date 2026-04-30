package actress_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/actress"
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

func setupActressTestDB(t *testing.T) (string, string) {
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

func seedActresses(t *testing.T, configPath string, actresses ...*models.Actress) {
	t.Helper()
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := database.NewActressRepository(db)
	for _, actress := range actresses {
		require.NoError(t, repo.Create(actress))
	}
}

func TestActressCommand_MergeFlags(t *testing.T) {
	cmd := actress.NewCommand()
	mergeCmd, _, err := cmd.Find([]string{"merge"})
	require.NoError(t, err)
	require.NotNil(t, mergeCmd)

	assert.NotNil(t, mergeCmd.Flags().Lookup("target"))
	assert.NotNil(t, mergeCmd.Flags().Lookup("source"))
	assert.NotNil(t, mergeCmd.Flags().Lookup("non-interactive"))
	assert.NotNil(t, mergeCmd.Flags().Lookup("prefer"))
	assert.NotNil(t, mergeCmd.Flags().Lookup("yes"))
}

func TestActressCommand_MergeNonInteractive(t *testing.T) {
	configPath, _ := setupActressTestDB(t)
	target := &models.Actress{DMMID: 80001, FirstName: "Target", LastName: "Actor", JapaneseName: "ターゲット"}
	source := &models.Actress{DMMID: 80002, FirstName: "Source", LastName: "Actor", JapaneseName: "ソース"}
	seedActresses(t, configPath, target, source)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{
		"actress", "merge",
		"--target", strconv.FormatUint(uint64(target.ID), 10),
		"--source", strconv.FormatUint(uint64(source.ID), 10),
		"--non-interactive",
		"--prefer", "source",
	})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Merged actress")

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := database.NewActressRepository(db)

	merged, err := repo.FindByID(target.ID)
	require.NoError(t, err)
	assert.Equal(t, 80002, merged.DMMID)

	_, err = repo.FindByID(source.ID)
	require.Error(t, err)
}

func TestActressCommand_MergeInteractive(t *testing.T) {
	configPath, _ := setupActressTestDB(t)
	target := &models.Actress{DMMID: 0, FirstName: "Target", LastName: "Actor", JapaneseName: "共通"}
	source := &models.Actress{DMMID: 1, FirstName: "Source", LastName: "Actor", JapaneseName: "共通"}
	seedActresses(t, configPath, target, source)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{
		"actress", "merge",
		"--target", strconv.FormatUint(uint64(target.ID), 10),
		"--source", strconv.FormatUint(uint64(source.ID), 10),
		"--yes",
	})
	rootCmd.SetIn(strings.NewReader("s\n"))

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Detected 1 conflicting field")
	assert.Contains(t, stdout, "Merged actress")

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := database.NewActressRepository(db)

	merged, err := repo.FindByID(target.ID)
	require.NoError(t, err)
	assert.Equal(t, "Source", merged.FirstName)
}

func TestActressExport_WithData(t *testing.T) {
	configPath, _ := setupActressTestDB(t)
	a1 := &models.Actress{DMMID: 90001, FirstName: "Aki", LastName: "Toyoda", JapaneseName: "豊田あき"}
	a2 := &models.Actress{DMMID: 90002, FirstName: "Hina", LastName: "Sato", JapaneseName: "さとはな"}
	seedActresses(t, configPath, a1, a2)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{"actress", "export"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, `"first_name": "Aki"`)
	assert.Contains(t, stdout, `"last_name": "Toyoda"`)
	assert.Contains(t, stdout, `"first_name": "Hina"`)
	assert.Contains(t, stdout, "Exported 2 actress(es)")
}

func TestActressExport_Empty(t *testing.T) {
	configPath, _ := setupActressTestDB(t)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{"actress", "export"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "[]")
	assert.Contains(t, stdout, "Exported 0 actress(es)")
}

func TestActressExport_ToFile(t *testing.T) {
	configPath, _ := setupActressTestDB(t)
	a1 := &models.Actress{DMMID: 90010, FirstName: "Moe", LastName: "Kawasaki", JapaneseName: "川崎萌"}
	seedActresses(t, configPath, a1)

	tmpDir := t.TempDir()
	exportPath := filepath.Join(tmpDir, "actresses.json")

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{"actress", "export", exportPath})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Exported 1 actress(es) to")
	assert.Contains(t, stdout, exportPath)

	fileData, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	assert.Contains(t, string(fileData), `"first_name": "Moe"`)
}

func TestActressImport_WithIDs(t *testing.T) {
	configPath, _ := setupActressTestDB(t)
	a1 := &models.Actress{DMMID: 90100, ID: 1, FirstName: "Original", LastName: "Actress", JapaneseName: "オリジナル"}
	seedActresses(t, configPath, a1)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "actresses.json")
	importData := []byte(`[
		{"id": 1, "first_name": "Updated", "last_name": "Actress", "japanese_name": "オリジナル更新", "dmm_id": 90100},
		{"id": 0, "first_name": "New", "last_name": "Star", "japanese_name": "新星", "dmm_id": 90200}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Imported: 2")

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := database.NewActressRepository(db)

	updated, err := repo.FindByID(1)
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.FirstName)
	assert.Equal(t, "オリジナル更新", updated.JapaneseName)
}

func TestActressImport_WithoutIDs(t *testing.T) {
	configPath, _ := setupActressTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "actresses.json")
	importData := []byte(`[
		{"id": 0, "first_name": "Created", "last_name": "Actress", "japanese_name": "創作物", "dmm_id": 90300}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Imported: 1")
	assert.Contains(t, stdout, "Skipped: 0")
	assert.Contains(t, stdout, "Errors: 0")

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int64
	db.DB.Model(&models.Actress{}).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestActressImport_InvalidJSON(t *testing.T) {
	configPath, _ := setupActressTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`{bad}`), 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse JSON")
	})
}

func TestActressImport_EmptyArray(t *testing.T) {
	configPath, _ := setupActressTestDB(t)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "empty.json")
	require.NoError(t, os.WriteFile(importPath, []byte(`[]`), 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no actresses found")
	})
}

func TestActressImport_SkipsIdentical(t *testing.T) {
	configPath, _ := setupActressTestDB(t)
	a1 := &models.Actress{DMMID: 90400, FirstName: "Aki", LastName: "Toyoda", JapaneseName: "豊田あき"}
	seedActresses(t, configPath, a1)

	tmpDir := t.TempDir()
	importPath := filepath.Join(tmpDir, "same.json")
	importData := []byte(`[
		{"id": ` + strconv.FormatUint(uint64(a1.ID), 10) + `, "first_name": "Aki", "last_name": "Toyoda", "japanese_name": "豊田あき", "dmm_id": 90400}
	]`)
	require.NoError(t, os.WriteFile(importPath, importData, 0644))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	rootCmd.AddCommand(actress.NewCommand())
	rootCmd.SetArgs([]string{"actress", "import", importPath})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "Imported: 0")
	assert.Contains(t, stdout, "Skipped: 1")
}
