package main

import (
	"context"
	"flag"
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
		return
	}
	for _, name := range []string{"malformed config", "invalid environment", "blocked log parent", "valid initialization", "relocated log", "normalized default log relocated", "stdout log directory"} {
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
			var want, blocker, relocated, originalLogPath string
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
			case "stdout log directory":
				wantCode, want = 0, "set, but logging.output has no file target; output remains: stdout"
				overrides = append(overrides, "LOG_LEVEL=debug", "JAVINIZER_LOG_DIR="+filepath.Join(dir, "unused-logs"))
			case "relocated log":
				wantCode = 0
				cfg.Logging.Output = filepath.Join(dir, "original", "app.log")
				originalLogPath = cfg.Logging.Output
				logDir := filepath.Join(dir, "relocated")
				relocated = filepath.Join(logDir, "app.log")
				overrides = append(overrides, "JAVINIZER_LOG_DIR="+logDir)
			case "normalized default log relocated":
				wantCode = 0
				logDir := filepath.Join(dir, "relocated")
				relocated = filepath.Join(logDir, "javinizer.log")
				originalLogPath = filepath.Join(dir, "data", "logs", "javinizer.log")
				want = "Log file outputs relocated by JAVINIZER_LOG_DIR="
				overrides = append(overrides, "LOG_LEVEL=debug", "JAVINIZER_LOG_DIR="+logDir)
			}
			// Start with a canonical current config: ordinary load normalization is
			// allowed to rewrite noncanonical fixtures before command execution.
			_, err := config.Prepare(cfg)
			require.NoError(t, err)
			if name == "normalized default log relocated" {
				// The loader's first environment pass sees no file target. Its
				// normalization supplies the default that root must relocate.
				cfg.Logging.Output = ""
			}
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
			args := []string{"-test.run=^TestRootConfigurationProcessContract$"}
			if coverDir := flag.Lookup("test.gocoverdir"); coverDir != nil && coverDir.Value.String() != "" {
				args = append(args, "-test.gocoverdir="+coverDir.Value.String())
			}
			child := exec.CommandContext(ctx, executable, args...)
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
			if name == "normalized default log relocated" {
				onDisk, loadErr := config.Load(path)
				require.NoError(t, loadErr)
				require.Equal(t, config.DefaultConfig(nil, nil).Logging.Output, onDisk.Logging.Output)
				require.NotContains(t, string(after), filepath.Dir(relocated), "runtime log override must not be persisted")
			} else {
				require.Equal(t, before, after, "initialization must not rewrite the current config")
			}
			if blocker != "" {
				kept, err := os.ReadFile(blocker)
				require.NoError(t, err)
				require.Equal(t, "keep this file", string(kept))
			}
			if name == "stdout log directory" {
				_, err := os.Stat(filepath.Join(dir, "unused-logs"))
				require.ErrorIs(t, err, os.ErrNotExist, "stdout-only logging must not create the override directory")
			}
			if relocated != "" {
				log, err := os.ReadFile(relocated)
				require.NoError(t, err)
				require.Contains(t, string(log), "Config is already at current version")
				_, err = os.Stat(originalLogPath)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
			_, err = os.Stat(cfg.Database.DSN)
			require.ErrorIs(t, err, os.ErrNotExist, "config-only command must not open a database")
		})
	}
}
