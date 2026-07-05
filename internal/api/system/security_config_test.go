package system

import (
	"bytes"
	"encoding/json"
	"errors"
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

// TestUpdateSecurityConfig_MissingFieldRejected verifies that a partial payload
// missing one of the four required keys is rejected with 400 and does NOT wipe
// the existing security config on disk. Without the raw-JSON key check, a body
// like {"allow_unc": true} would unmarshal with allowed_directories=nil and
// silently clear the allowlist.
func TestUpdateSecurityConfig_MissingFieldRejected(t *testing.T) {
	initial := config.DefaultConfig(nil, nil)
	initial.API.Security.AllowedDirectories = []string{"/keep"}
	initial.API.Security.DeniedDirectories = []string{"/deny"}
	initial.API.Security.AllowUNC = false
	initial.API.Security.AllowedUNCServers = []string{"\\\\srv\\share"}

	tempConfigFile := t.TempDir() + "/config.yaml"
	coreDeps := createTestDeps(t, initial, tempConfigFile)
	deps := systemDepsFromCore(coreDeps)

	for _, tc := range []struct {
		name    string
		payload string
	}{
		{
			name:    "missing allowed_directories",
			payload: `{"denied_directories":[],"allow_unc":false,"allowed_unc_servers":[]}`,
		},
		{
			name:    "missing denied_directories",
			payload: `{"allowed_directories":[],"allow_unc":false,"allowed_unc_servers":[]}`,
		},
		{
			name:    "missing allow_unc",
			payload: `{"allowed_directories":[],"denied_directories":[],"allowed_unc_servers":[]}`,
		},
		{
			name:    "missing allowed_unc_servers",
			payload: `{"allowed_directories":[],"denied_directories":[],"allow_unc":false}`,
		},
		{
			name:    "only allow_unc present",
			payload: `{"allow_unc":true}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.PUT("/config/security", updateSecurityConfig(testkit.GetTestRuntime(deps)))

			req := httptest.NewRequest(http.MethodPut, "/config/security", bytes.NewBufferString(tc.payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
			assert.Contains(t, w.Body.String(), "Missing required field")
		})
	}

	saved := deps.CoreDeps.GetConfig()
	assert.Equal(t, []string{"/keep"}, saved.API.Security.AllowedDirectories, "allowlist must not be wiped by a rejected payload")
	assert.Equal(t, []string{"/deny"}, saved.API.Security.DeniedDirectories)
	assert.False(t, saved.API.Security.AllowUNC)
	assert.Equal(t, []string{"\\\\srv\\share"}, saved.API.Security.AllowedUNCServers)
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

// TestUpdateSecurityConfig_BodyReadError covers the io.ReadAll error branch in
// updateSecurityConfig. A request body whose Read returns an error drives the
// handler into the "Invalid security configuration format" 400 path before the
// raw-JSON key check runs. The default httptest request body never fails Read,
// so we inject a failing io.ReadCloser directly.
func TestUpdateSecurityConfig_BodyReadError(t *testing.T) {
	initial := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	coreDeps := createTestDeps(t, initial, tempConfigFile)
	deps := systemDepsFromCore(coreDeps)

	router := gin.New()
	router.PUT("/config/security", updateSecurityConfig(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPut, "/config/security", failingBody{})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "Invalid security configuration format")
}

// TestUpdateSecurityConfig_TypeMismatch covers the second json.Unmarshal error
// branch — the one that decodes into SecurityUpdateRequest after the raw-map
// key-presence check already passed. A body that is a valid JSON object
// containing all four required keys but with a value of the wrong type (a
// string where []string is expected) clears the key check, then fails the
// typed unmarshal, exercising the 400 "Invalid security configuration format"
// path that the InvalidJSON and MissingField tests do not reach.
func TestUpdateSecurityConfig_TypeMismatch(t *testing.T) {
	initial := config.DefaultConfig(nil, nil)
	tempConfigFile := t.TempDir() + "/config.yaml"
	coreDeps := createTestDeps(t, initial, tempConfigFile)
	deps := systemDepsFromCore(coreDeps)

	router := gin.New()
	router.PUT("/config/security", updateSecurityConfig(testkit.GetTestRuntime(deps)))

	payload := `{"allowed_directories":"not-an-array","denied_directories":[],"allow_unc":false,"allowed_unc_servers":[]}`
	req := httptest.NewRequest(http.MethodPut, "/config/security", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "Invalid security configuration format")
}

// failingBody is an io.ReadCloser whose Read always returns an error, used to
// exercise the io.ReadAll failure branch of updateSecurityConfig.
type failingBody struct{}

func (failingBody) Read(p []byte) (int, error) { return 0, errors.New("synthetic read failure") }
func (failingBody) Close() error               { return nil }
