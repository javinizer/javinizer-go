package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/system"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVersionStatus_InstallEnvironment verifies the /version response carries
// the bootstrap-injected install environment and environment-specific upgrade
// instructions on every response path, so the Web UI can render the right
// upgrade guidance (docker pull / releases link / javinizer upgrade).
func TestVersionStatus_InstallEnvironment(t *testing.T) {
	cases := []struct {
		name         string
		env          system.Environment
		wantEnv      string
		wantInstrSub string
		wantCmdKeys  []string
	}{
		{
			name:         "docker surfaces docker pull instructions",
			env:          system.EnvironmentDocker,
			wantEnv:      "docker",
			wantInstrSub: "docker pull",
			wantCmdKeys:  []string{"docker_pull", "docker_compose"},
		},
		{
			name:         "desktop surfaces in-app update button",
			env:          system.EnvironmentDesktop,
			wantEnv:      "desktop",
			wantInstrSub: "Update & restart",
			wantCmdKeys:  nil, // in-app button only — no terminal commands
		},
		{
			name:         "cli surfaces javinizer upgrade",
			env:          system.EnvironmentCLI,
			wantEnv:      "cli",
			wantInstrSub: "javinizer upgrade",
			wantCmdKeys:  nil, // host-OS-dependent; derived below
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tempDataDir := t.TempDir()
			t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

			cfg := config.DefaultConfig(nil, nil)
			// Disabled so GetStatus short-circuits to the disabled path — the
			// environment must still be stamped regardless of update state.
			cfg.System.VersionCheckEnabled = false

			deps := newTestVersionDeps(cfg)
			deps.CoreDeps.SetInstallEnvironment(tc.env)

			router := gin.New()
			router.GET("/version", versionStatus(deps.CoreDeps))

			req := httptest.NewRequest(http.MethodGet, "/version", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)

			var resp VersionStatusResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, tc.wantEnv, resp.InstallEnvironment, "install_environment")
			require.NotEmpty(t, resp.UpgradeInstructions, "upgrade_instructions must be populated")
			assert.True(t, strings.Contains(resp.UpgradeInstructions, tc.wantInstrSub),
				"upgrade_instructions=%q must contain %q", resp.UpgradeInstructions, tc.wantInstrSub)

			// Structured per-method upgrade commands ride alongside the prose so
			// the UI can render one copyable row each (never a prose blob).
			wantCmdKeys := tc.wantCmdKeys
			if tc.env == system.EnvironmentCLI {
				// The package-manager row is gated on the host OS — Homebrew on
				// darwin/linux, Scoop on windows, never both.
				wantCmdKeys = []string{"cli_binary"}
				switch runtime.GOOS {
				case "darwin", "linux":
					wantCmdKeys = append(wantCmdKeys, "homebrew")
				case "windows":
					wantCmdKeys = append(wantCmdKeys, "scoop")
				}
			}
			var gotKeys []string
			for _, cmd := range resp.UpgradeCommands {
				gotKeys = append(gotKeys, cmd.Key)
				assert.NotEmpty(t, cmd.Command, "env=%s key=%s must carry a pasteable command", tc.env, cmd.Key)
				assert.NotContains(t, cmd.Command, "\n", "env=%s key=%s must be a single line", tc.env, cmd.Key)
			}
			assert.Equal(t, wantCmdKeys, gotKeys, "env=%s upgrade_commands keys", tc.env)
			if tc.env == system.EnvironmentDocker {
				require.NotEmpty(t, resp.UpgradeCommands)
				assert.Contains(t, resp.UpgradeCommands[0].Command, "ghcr.io/javinizer/javinizer-go",
					"docker commands must embed the published image ref")
			}
		})
	}
}

func TestVersionStatus_PrereleaseCommands(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	// Seed a cached state announcing a PRERELEASE update: the generated
	// commands must match what actually installs that version.
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	statePath := filepath.Join(tempDataDir, "update_cache.json")
	writeTestState(t, statePath, "v9.9.9-rc1", checkedAt, true, true, update.UpdateSourceCached, "")

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true

	deps := newTestVersionDeps(cfg)
	deps.CoreDeps.SetInstallEnvironment(system.EnvironmentCLI)

	router := gin.New()
	router.GET("/version", versionStatus(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Prerelease, "seeded state should announce a prerelease")

	require.Len(t, resp.UpgradeCommands, 1, "prerelease announcements must drop stable-only package-manager rows")
	assert.Equal(t, "cli_binary", resp.UpgradeCommands[0].Key)
	assert.Equal(t, "javinizer upgrade --prerelease", resp.UpgradeCommands[0].Command)
}

func TestVersionStatus_PrereleaseCommandsDocker(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	checkedAt := time.Now().UTC().Format(time.RFC3339)
	statePath := filepath.Join(tempDataDir, "update_cache.json")
	writeTestState(t, statePath, "v9.9.9-rc1", checkedAt, true, true, update.UpdateSourceCached, "")

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true

	deps := newTestVersionDeps(cfg)
	deps.CoreDeps.SetInstallEnvironment(system.EnvironmentDocker)

	router := gin.New()
	router.GET("/version", versionStatus(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Prerelease, "seeded state should announce a prerelease")

	// Docker prerelease: the pull row must carry the announced tag — plain
	// `:latest` is stable-only — and the compose row is omitted (its
	// pull is file-driven and would fetch the latest STABLE).
	require.Len(t, resp.UpgradeCommands, 1)
	assert.Equal(t, "docker_pull", resp.UpgradeCommands[0].Key)
	assert.Equal(t, "docker pull ghcr.io/javinizer/javinizer-go:v9.9.9-rc1", resp.UpgradeCommands[0].Command)
}

func TestDetectInstallMethod(t *testing.T) {
	t.Cleanup(func() {
		osExecutable = os.Executable
		evalSymlinks = filepath.EvalSymlinks
	})

	t.Run("executable resolution failure falls back to manual", func(t *testing.T) {
		osExecutable = func() (string, error) { return "", assert.AnError }
		assert.Equal(t, "manual", detectInstallMethod())
	})

	t.Run("symlink resolution failure uses the raw path", func(t *testing.T) {
		osExecutable = func() (string, error) { return "/usr/local/bin/javinizer", nil }
		evalSymlinks = func(string) (string, error) { return "", assert.AnError }
		assert.Equal(t, "manual", detectInstallMethod())
	})

	t.Run("resolved Cellar path reports homebrew", func(t *testing.T) {
		osExecutable = func() (string, error) { return "/usr/local/bin/javinizer", nil }
		evalSymlinks = func(string) (string, error) { return "/opt/homebrew/Cellar/javinizer/1.4.0/bin/javinizer", nil }
		assert.Equal(t, "homebrew", detectInstallMethod())
	})

	t.Run("resolved scoop path reports scoop", func(t *testing.T) {
		// DetectInstallMethod applies filepath.ToSlash — use the already-slashed
		// form; separator conversion depends on the test host OS.
		osExecutable = func() (string, error) { return "C:/Users/x/bin/javinizer.exe", nil }
		evalSymlinks = func(string) (string, error) {
			return "C:/Users/x/scoop/apps/javinizer/current/javinizer.exe", nil
		}
		assert.Equal(t, "scoop", detectInstallMethod())
	})
}

// TestVersionStatus_InstallEnvironmentDefaultCLI confirms an uninitialized
// CoreDeps (no SetInstallEnvironment call) defaults to "cli" so a miswired
// bootstrap never produces an empty install_environment field in the API.
func TestVersionStatus_InstallEnvironmentDefaultCLI(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = false

	deps := newTestVersionDeps(cfg)
	// Deliberately NOT calling SetInstallEnvironment — defaults must be CLI.

	router := gin.New()
	router.GET("/version", versionStatus(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "cli", resp.InstallEnvironment)
}
