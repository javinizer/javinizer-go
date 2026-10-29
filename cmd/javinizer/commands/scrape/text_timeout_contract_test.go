package scrape_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	scrapecmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestTextScrapeRejectsBrowserTimeoutBeforeBootstrap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Browser.Enabled = true
	cfg.Scrapers.Browser.Timeout = 30
	cfg.System.VersionCheckEnabled = false
	cfg.System.TempDir = filepath.Join(dir, "temp")
	cfg.Database.DSN = filepath.Join(dir, "catalog.db")
	cfg.Logging.Output = filepath.Join(dir, "application.log")
	require.NoError(t, config.Save(cfg, path))
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	t.Setenv("JAVINIZER_TEMP_DIR", cfg.System.TempDir)
	t.Setenv("JAVINIZER_LOG_DIR", dir)
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("UMASK", "022")
	// LoadOrCreate performs supported persisted normalization before the CLI
	// override contract begins. The later boundary loads and command must leave
	// this canonical configuration byte-for-byte unchanged.
	_, err := config.LoadOrCreate(path)
	require.NoError(t, err)
	original, err := os.ReadFile(path)
	require.NoError(t, err)

	// Both inclusive limits remain valid through the same public flag override
	// and preparation APIs, without bootstrapping a browser or provider.
	for _, value := range []string{"1", "300"} {
		t.Run("valid_boundary_"+value, func(t *testing.T) {
			loaded, err := config.LoadOrCreate(path)
			require.NoError(t, err)
			require.True(t, loaded.Scrapers.Browser.Enabled)
			require.Equal(t, 30, loaded.Scrapers.Browser.Timeout)
			config.ApplyEnvironmentOverrides(loaded)
			_, err = config.Prepare(loaded)
			require.NoError(t, err)
			cmd := scrapecmd.NewCommand()
			require.NoError(t, cmd.ParseFlags([]string{"--browser-timeout", value}))
			scrapecmd.ApplyFlagOverrides(cmd, loaded)
			want, err := strconv.Atoi(value)
			require.NoError(t, err)
			require.Equal(t, want, loaded.Scrapers.Browser.Timeout)
			_, err = config.Prepare(loaded)
			require.NoError(t, err)
		})
	}

	root := &cobra.Command{Use: "javinizer", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "configuration file")
	root.AddCommand(scrapecmd.NewCommand())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"scrape", "FIX-001", "--config", path, "--output", "text", "--browser-timeout", "301"})
	err = root.Execute()
	require.ErrorContains(t, err, "invalid configuration after CLI overrides")
	require.ErrorContains(t, err, "browser.timeout must be between 1 and 300 seconds")
	require.Empty(t, stdout.String(), "invalid input must not produce a movie or success output")
	require.Empty(t, stderr.String())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after, "CLI overrides must not rewrite persisted configuration")
	for _, artifact := range []string{cfg.Database.DSN, cfg.Database.DSN + "-wal", cfg.Database.DSN + "-shm", cfg.Logging.Output, cfg.System.TempDir} {
		_, err := os.Stat(artifact)
		require.ErrorIs(t, err, os.ErrNotExist, "bootstrap artifact: %s", artifact)
	}
}
