package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/system"
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
	}{
		{
			name:         "docker surfaces docker pull instructions",
			env:          system.EnvironmentDocker,
			wantEnv:      "docker",
			wantInstrSub: "docker pull",
		},
		{
			name:         "desktop surfaces in-app update button",
			env:          system.EnvironmentDesktop,
			wantEnv:      "desktop",
			wantInstrSub: "Update & restart",
		},
		{
			name:         "cli surfaces javinizer upgrade",
			env:          system.EnvironmentCLI,
			wantEnv:      "cli",
			wantInstrSub: "javinizer upgrade",
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
		})
	}
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
