package system

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- mapConfigErrorToHTTP: covers all error type branches ---

func TestMapConfigErrorToHTTP_Nil(t *testing.T) {
	status, msg := mapConfigErrorToHTTP(nil)
	assert.Equal(t, 0, status)
	assert.Empty(t, msg)
}

func TestMapConfigErrorToHTTP_ValidationError(t *testing.T) {
	err := &validationError{message: "bad config"}
	status, msg := mapConfigErrorToHTTP(err)
	assert.Equal(t, 400, status)
	assert.Equal(t, "bad config", msg)
}

func TestMapConfigErrorToHTTP_PersistError(t *testing.T) {
	err := &persistError{message: "disk full"}
	status, msg := mapConfigErrorToHTTP(err)
	assert.Equal(t, 500, status)
	assert.Equal(t, "disk full", msg)
}

func TestMapConfigErrorToHTTP_ReloadError(t *testing.T) {
	err := &reloadError{originalErr: errors.New("boom"), message: "reload failed: boom"}
	status, msg := mapConfigErrorToHTTP(err)
	assert.Equal(t, 500, status)
	assert.Contains(t, msg, "reload failed")
}

func TestMapConfigErrorToHTTP_RollbackError(t *testing.T) {
	err := &rollbackError{
		rollbackErr: errors.New("rollback failed"),
		originalErr: errors.New("reload failed"),
		message:     "critical: rollback failed",
	}
	status, msg := mapConfigErrorToHTTP(err)
	assert.Equal(t, 500, status)
	assert.Contains(t, msg, "critical")
}

func TestMapConfigErrorToHTTP_UnknownError(t *testing.T) {
	err := errors.New("unknown")
	status, msg := mapConfigErrorToHTTP(err)
	assert.Equal(t, 500, status)
	assert.Equal(t, "unknown", msg)
}

// --- ValidateAndApply: validation error branch (Prepare fails) ---

func TestValidateAndApply_ValidationError(t *testing.T) {
	deps := &core.APIDeps{}
	rt := core.NewAPIRuntime(deps)
	oldCfg := defaultTestConfig()
	rt.SetConfig(oldCfg)
	testkit.SetTestRuntime(deps, rt)
	deps.TokenStore = core.NewTokenStore()

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), t.TempDir()+"/config.yaml")

	newCfg := defaultTestConfig()
	newCfg.ConfigVersion = 9999 // Triggers Prepare error: version too new

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)
	var valErr *validationError
	assert.True(t, errors.As(err, &valErr), "Expected validationError, got %T: %v", err, err)
}

// --- ValidateAndApply: persist error branch (configFile is unwritable) ---

func TestValidateAndApply_PersistError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Rename over a directory may succeed on Windows via replaceFileOnWindows, so the persist error path is not reliably triggered")
	}

	tmpDir := t.TempDir()
	// Create a directory at the config path so config.Save fails. config.Save
	// writes a temp file then os.Renames it into place — os.Rename fails on
	// Unix when the destination is a directory, but may succeed on Windows.
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.MkdirAll(configPath, 0755)) // config.yaml is a directory, not a file

	deps := &core.APIDeps{}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(defaultTestConfig())
	testkit.SetTestRuntime(deps, rt)
	deps.TokenStore = core.NewTokenStore()

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), configPath)
	oldCfg := defaultTestConfig()
	newCfg := defaultTestConfig()

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)
	var persistErr *persistError
	assert.True(t, errors.As(err, &persistErr), "Expected persistError, got %T: %v", err, err)
}

// --- ValidateAndApply: success path ---

func TestValidateAndApply_Success(t *testing.T) {
	tempDir := t.TempDir()
	configFile := tempDir + "/config.yaml"

	deps := &core.APIDeps{}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(defaultTestConfig())
	testkit.SetTestRuntime(deps, rt)
	deps.TokenStore = core.NewTokenStore()

	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), configFile)
	oldCfg := defaultTestConfig()
	newCfg := defaultTestConfig()

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)
}

func defaultTestConfig() *config.Config {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Logging = config.LoggingConfig{Level: "error"}
	return cfg
}
