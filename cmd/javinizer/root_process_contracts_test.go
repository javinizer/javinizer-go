package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

// The child executes the real root pre-run and os.Exit paths. Its current-version
// config migration is a no-op and does not construct scrapers or make requests.
func TestRootConfigurationProcessContract(t *testing.T) {
	if os.Getenv("JAVINIZER_ROOT_EXIT_HELPER") == "1" {
		rootCmd.SetArgs([]string{"--config", os.Getenv("JAVINIZER_CONFIG"), "config", "migrate", "--dry-run"})
		if err := Execute(); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	for _, name := range []string{"malformed config", "invalid environment", "blocked log parent", "valid initialization", "relocated log"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			cfg := config.DefaultConfig(nil, nil)
			cfg.ConfigVersion = config.CurrentConfigVersion
			cfg.Database.DSN = filepath.Join(dir, "unused.db")
			cfg.System.VersionCheckEnabled = false
			cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.Output = "info", "text", "stdout"
			cfg.Metadata.Translation.Enabled = true
			cfg.Metadata.Translation.Provider = "google"
			cfg.Metadata.Translation.Google.Mode = "free"
			wantCode := 1
			var want, blocker, relocated string
			var overrides []string
			switch name {
			case "malformed config":
				want = "Failed to load config:"
			case "invalid environment":
				// LoadOrCreate applies and validates overrides before the root re-prepares.
				want = "Failed to load config:"
				overrides = append(overrides, "TRANSLATION_PROVIDER=unsupported-fixture-provider")
			case "blocked log parent":
				want = "Failed to initialize logger:"
				blocker = filepath.Join(dir, "not-a-directory")
				require.NoError(t, os.WriteFile(blocker, []byte("keep this file"), 0600))
				cfg.Logging.Output = filepath.Join(blocker, "app.log")
			case "valid initialization":
				wantCode, want = 0, "Config is already at current version"
			case "relocated log":
				wantCode = 0
				cfg.Logging.Output = filepath.Join(dir, "original", "app.log")
				logDir := filepath.Join(dir, "relocated")
				relocated = filepath.Join(logDir, "app.log")
				overrides = append(overrides, "JAVINIZER_LOG_DIR="+logDir)
			}
			// Start with a canonical current config: ordinary load normalization is
			// allowed to rewrite noncanonical fixtures before command execution.
			_, err := config.Prepare(cfg)
			require.NoError(t, err)
			require.NoError(t, config.Save(cfg, path))
			if name == "malformed config" {
				require.NoError(t, os.WriteFile(path, []byte("[malformed yaml"), 0600))
			}
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			executable, err := os.Executable()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, executable, "-test.run=^TestRootConfigurationProcessContract$")
			child.Dir = dir
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				key = strings.ToUpper(key)
				if key == "LOG_LEVEL" || key == "UMASK" || strings.HasPrefix(key, "JAVINIZER_") || strings.HasPrefix(key, "TRANSLATION_") || strings.HasPrefix(key, "METADATA_TRANSLATION_") || strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "DEEPL_") || strings.HasPrefix(key, "GOOGLE_") || strings.HasPrefix(key, "ANTHROPIC_") {
					continue
				}
				child.Env = append(child.Env, entry)
			}
			child.Env = append(child.Env, "JAVINIZER_ROOT_EXIT_HELPER=1", "JAVINIZER_CONFIG="+path)
			child.Env = append(child.Env, overrides...)
			output, err := child.CombinedOutput()
			require.NoError(t, ctx.Err(), "subprocess timed out: %s", output)
			if wantCode == 0 {
				require.NoError(t, err, "%s", output)
			} else {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, "%s", output)
				require.Equal(t, wantCode, exitErr.ExitCode(), "%s", output)
			}
			if want != "" {
				require.Contains(t, string(output), want)
			}
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after, "initialization must not rewrite the current config")
			if blocker != "" {
				kept, err := os.ReadFile(blocker)
				require.NoError(t, err)
				require.Equal(t, "keep this file", string(kept))
			}
			if relocated != "" {
				log, err := os.ReadFile(relocated)
				require.NoError(t, err)
				require.Contains(t, string(log), "Config is already at current version")
				_, err = os.Stat(cfg.Logging.Output)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
			_, err = os.Stat(cfg.Database.DSN)
			require.ErrorIs(t, err, os.ErrNotExist, "config-only command must not open a database")
		})
	}
}
