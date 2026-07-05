package system

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
)

func TestUpdateSecurityConfig_PersistsSecurityBlock(t *testing.T) {
	initial := config.DefaultConfig(nil, nil)
	initial.API.Security.AllowedDirectories = []string{"/old"}
	initial.API.Security.DeniedDirectories = []string{"/tmp"}
	initial.API.Security.AllowUNC = false
	initial.API.Security.AllowedUNCServers = []string{"\\\\oldserver\\share"}
	initial.API.Security.MaxFilesPerScan = 500

	tempConfigFile := t.TempDir() + "/config.yaml"
	coreDeps := createTestDeps(t, initial, tempConfigFile)
	deps := systemDepsFromCore(coreDeps)

	router := gin.New()
	router.PUT("/config/security", updateSecurityConfig(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(SecurityUpdateRequest{
		AllowedDirectories: []string{"/videos", "/media"},
		DeniedDirectories:  []string{"/etc"},
		AllowUNC:           true,
		AllowedUNCServers:  []string{"\\\\nas\\share"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/config/security", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp securityResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"/videos", "/media"}, resp.Security.AllowedDirectories)
	assert.Equal(t, []string{"/etc"}, resp.Security.DeniedDirectories)
	assert.True(t, resp.Security.AllowUNC)
	assert.Equal(t, []string{"\\\\nas\\share"}, resp.Security.AllowedUNCServers)

	saved := deps.CoreDeps.GetConfig()
	assert.Equal(t, []string{"/videos", "/media"}, saved.API.Security.AllowedDirectories)
	assert.Equal(t, 500, saved.API.Security.MaxFilesPerScan, "non-editable security fields preserved")
}

func TestUpdateSecurityConfig_EmptyAllowlistAllowed(t *testing.T) {
	initial := config.DefaultConfig(nil, nil)
	initial.API.Security.AllowedDirectories = []string{"/keep"}

	tempConfigFile := t.TempDir() + "/config.yaml"
	coreDeps := createTestDeps(t, initial, tempConfigFile)
	deps := systemDepsFromCore(coreDeps)

	router := gin.New()
	router.PUT("/config/security", updateSecurityConfig(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(SecurityUpdateRequest{
		AllowedDirectories: []string{},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/config/security", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	saved := deps.CoreDeps.GetConfig()
	assert.Empty(t, saved.API.Security.AllowedDirectories)
}

func TestUpdateSecurityConfig_InvalidJSON(t *testing.T) {
	initial := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	coreDeps := createTestDeps(t, initial, tempConfigFile)
	deps := systemDepsFromCore(coreDeps)

	router := gin.New()
	router.PUT("/config/security", updateSecurityConfig(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPut, "/config/security", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid security configuration format")
}

func TestUpdateSecurityConfig_PreservesOtherSections(t *testing.T) {
	initial := config.DefaultConfig(nil, nil)
	initial.Server.Host = "1.2.3.4"
	initial.Server.Port = 9999
	initial.API.Security.AllowedDirectories = []string{"/old"}

	tempConfigFile := t.TempDir() + "/config.yaml"
	coreDeps := createTestDeps(t, initial, tempConfigFile)
	deps := systemDepsFromCore(coreDeps)

	router := gin.New()
	router.PUT("/config/security", updateSecurityConfig(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(SecurityUpdateRequest{
		AllowedDirectories: []string{"/new"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/config/security", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	saved := deps.CoreDeps.GetConfig()
	assert.Equal(t, "1.2.3.4", saved.Server.Host, "non-security sections preserved")
	assert.Equal(t, 9999, saved.Server.Port)
	assert.Equal(t, []string{"/new"}, saved.API.Security.AllowedDirectories)
}

// TestUpdateSecurityConfig_ValidateAndApplyError exercises the ValidateAndApply
// error branch in updateSecurityConfig — the path that calls
// mapConfigErrorToHTTP and returns the mapped status. A persist failure (config
// file path whose parent directory cannot be created) yields a persistError,
// which mapConfigErrorToHTTP maps to HTTP 500 — the non-400 path that the happy
// and invalid-JSON tests do not cover.
func TestUpdateSecurityConfig_ValidateAndApplyError(t *testing.T) {
	initial := config.DefaultConfig(nil, nil)
	initial.API.Security.AllowedDirectories = []string{"/old"}

	// Block the config file's parent directory by creating a regular file at
	// that path, then point the config file underneath it. config.Save calls
	// MkdirAll on the parent, which fails with "not a directory" — the storage
	// layer wraps this as a persistError (mapped to 500 by mapConfigErrorToHTTP).
	parentBlocker := filepath.Join(t.TempDir(), "not_a_dir")
	require.NoError(t, os.WriteFile(parentBlocker, []byte("blocker"), 0644))
	configFile := filepath.Join(parentBlocker, "config.yaml")

	coreDeps := createTestDeps(t, initial, configFile)
	deps := systemDepsFromCore(coreDeps)

	router := gin.New()
	router.PUT("/config/security", updateSecurityConfig(testkit.GetTestRuntime(deps)))

	body, err := json.Marshal(SecurityUpdateRequest{
		AllowedDirectories: []string{"/videos"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/config/security", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "Failed to save configuration",
		"should surface the persistError message from mapConfigErrorToHTTP")
}
