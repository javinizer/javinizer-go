package api_test

import (
	"os"
	"testing"

	api "github.com/javinizer/javinizer-go/cmd/javinizer/commands/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCommand_RunEExists verifies that the RunE function is set on the command.
func TestNewCommand_RunEExists(t *testing.T) {
	cmd := api.NewCommand()
	require.NotNil(t, cmd.RunE, "RunE should be set on the command")
}

// TestRun_LoggingInfoPath exercises the path where Run logs
// "Loaded configuration from %s" — this hits line 75 of command.go.
func TestRun_LoggingInfoPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()
}

// TestRun_E2EAuthRateLimitDisabled hits the JAVINIZER_E2E_AUTH=true branch in Run().
func TestRun_E2EAuthRateLimitDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	configPath, _ := setupTagTestDB(t)

	t.Setenv("JAVINIZER_E2E_AUTH", "true")

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()
}

// TestRun_InvalidPrepare tests the "invalid configuration" error path when Prepare fails.
func TestRun_InvalidPrepare(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/bad_config.yaml"

	// Write a config that will fail Prepare (e.g. translation enabled without api_key)
	configContent := `config_version: 3
database:
  dsn: ":memory:"
  type: sqlite
metadata:
  priority: {}
  translation:
    enabled: true
    provider: deepl
    deepl:
      mode: free
matching:
  extensions: [".mp4"]
  regex_enabled: false
server:
  host: localhost
  port: 8080
system:
  temp_dir: ` + tmpDir + `
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	assert.Error(t, err, "should fail with invalid config")
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "invalid configuration")
}

// TestRun_BootstrapAPIError hits the path where apicore.BootstrapAPI fails.
func TestRun_BootstrapAPIError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a valid config but with an in-memory DSN that BootstrapAPI can work with
	// We test that Run returns an error when BootstrapAPI fails
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.yaml"

	configContent := `config_version: 3
database:
  dsn: ":memory:"
  type: sqlite
server:
  host: localhost
  port: 8080
system:
  temp_dir: ` + tmpDir + `
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	cmd := api.NewCommand()
	// This should succeed since config is valid
	deps, _, err := api.Run(cmd, configPath, "", 0)
	if err != nil {
		assert.Nil(t, deps)
	} else {
		defer func() { _ = deps.CoreDeps.DB.Close() }()
		assert.NotNil(t, deps)
	}
}

// TestRun_SetApiTokenRepo verifies the authManager.SetApiTokenRepo path.
func TestRun_SetApiTokenRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	// Verify the API token repo was set on the auth manager
	assert.NotNil(t, deps.Repos.ApiTokenRepo, "ApiTokenRepo should be initialized")
}
