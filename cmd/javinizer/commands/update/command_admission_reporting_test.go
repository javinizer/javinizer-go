package update_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateCommandRejectsInvalidMergeStrategiesBeforeBootstrap(t *testing.T) {
	for _, tc := range []struct {
		name       string
		flag       string
		wantError  string
		wantOutput string
	}{
		{"preset", "--preset=invalid", "invalid preset", "Using preset: invalid\n"},
		{"scalar", "--scalar-strategy=invalid", `invalid scalar strategy "invalid"`, ""},
		{"array", "--array-strategy=invalid", `invalid array strategy "invalid"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := t.TempDir()
			video := testutil.CreateTestVideoFile(t, source, "FAIL-001.mp4")
			stateDir := t.TempDir()
			configPath := filepath.Join(stateDir, "config.yaml")
			dbPath := filepath.Join(stateDir, "javinizer.db")
			t.Setenv("JAVINIZER_HOME", t.TempDir())
			t.Setenv("JAVINIZER_TEMP_DIR", t.TempDir())
			t.Setenv("JAVINIZER_DB", dbPath)
			t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")

			// Exercise the installed Cobra RunE, including the inherited config
			// flag. LoadOrCreate would create this missing config if admission
			// were delayed until after the batch scaffold starts.
			var output bytes.Buffer
			root := &cobra.Command{Use: "javinizer", SilenceUsage: true, SilenceErrors: true}
			root.PersistentFlags().String("config", "", "configuration file")
			root.AddCommand(update.NewCommand())
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs([]string{"update", source, "--config", configPath, tc.flag})

			err := root.Execute()
			require.ErrorContains(t, err, tc.wantError)
			assert.Equal(t, tc.wantOutput, output.String(), "rejection must precede scan/processing output")
			entries, err := os.ReadDir(stateDir)
			require.NoError(t, err)
			assert.Empty(t, entries, "invalid flags must not create a config, database, or sidecars")
			assertUpdateVideoUnchanged(t, source, video)
		})
	}
}

func TestUpdateCommandReportsAllFailedBatchWithoutWritingMedia(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_TEMP_DIR", "")
	t.Setenv("JAVINIZER_R18DEV_DUMP_PATH", "")
	t.Setenv("JAVINIZER_HOME", t.TempDir())
	source := t.TempDir()
	video := testutil.CreateTestVideoFile(t, source, "FAIL-001.mp4")
	tempDir := t.TempDir()
	configPath, cfg := testutil.CreateTestConfig(t, func(cfg *config.Config) {
		cfg.Database.LogLevel = "silent"
		cfg.System.TempDir = tempDir
		cfg.Matching.Extensions = []string{".mp4"}
		cfg.Matching.MinSizeMB = 0
		cfg.Metadata.R18DevDump.Enabled = false
		cfg.Metadata.Translation.Enabled = false
		cfg.Scrapers.ScrapeActress = false
		cfg.Output.Download.DownloadCover = false
		cfg.Output.Download.DownloadPoster = false
		cfg.Output.Download.DownloadExtrafanart = false
		cfg.Output.Download.DownloadTrailer = false
		cfg.Output.Download.DownloadActress = false
	})

	// FAIL-* returns a deterministic error before the e2emock scraper can
	// supply artwork URLs. This runs the real nonempty scan, scrape job and
	// installed update summary, not an empty-directory shortcut or closure.
	var output bytes.Buffer
	root := &cobra.Command{Use: "javinizer", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "configuration file")
	root.AddCommand(update.NewCommand())
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"update", source, "--config", configPath, "--download=false"})
	require.NoError(t, root.Execute(), "per-file scrape failures are reported in the batch summary")

	out := output.String()
	assert.Contains(t, out, "e2emock: movie FAIL-001 not found")
	assert.Contains(t, out, "Updated: 0, Failed: 1\n")
	assert.Contains(t, out, "No metadata found\n")
	assert.NotContains(t, out, "No files to process")
	assert.NotContains(t, out, "NFO Merge Summary")
	assert.NotContains(t, out, "Generated ")
	assert.NotContains(t, out, "Update complete!")
	assertUpdateVideoUnchanged(t, source, video)

	// The persisted batch proves that the explicit config was used and that
	// the summary describes one real failed file, not an early scan exit.
	require.FileExists(t, cfg.Database.DSN)
	db, err := database.New(&database.Config{Type: "sqlite", DSN: cfg.Database.DSN, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	jobs, err := db.Repositories().JobRepo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.True(t, jobs[0].Update)
	assert.Equal(t, 1, jobs[0].TotalFiles)
	assert.Equal(t, 0, jobs[0].Completed)
	assert.Equal(t, 1, jobs[0].Failed)
	assert.Contains(t, out, "Batch Job: "+jobs[0].ID)
}

func assertUpdateVideoUnchanged(t *testing.T, source, video string) {
	t.Helper()
	data, err := os.ReadFile(video)
	require.NoError(t, err)
	assert.Equal(t, "dummy video content", string(data))
	entries, err := os.ReadDir(source)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no NFO, artwork, or organized subdirectory may be created")
	assert.Equal(t, filepath.Base(video), entries[0].Name())
}
