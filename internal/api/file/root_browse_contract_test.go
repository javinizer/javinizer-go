package file_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	fileapi "github.com/javinizer/javinizer-go/internal/api/file"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

// Configure browsing intentionally supports filesystem-root navigation without
// granting operational scan access. Only root directory metadata is listed;
// no descendant file contents are opened and no root entries are modified.
func TestConfigureBrowseRootHasNoParentWithoutBroadeningScanAccess(t *testing.T) {
	owned := t.TempDir()
	denied := filepath.Join(owned, "denied")
	require.NoError(t, os.Mkdir(denied, 0700))
	sentinel := filepath.Join(owned, "sentinel")
	require.NoError(t, os.WriteFile(sentinel, []byte("preserved"), 0600))
	beforeFile, err := os.Stat(sentinel)
	require.NoError(t, err)
	root := filepath.VolumeName(owned) + string(os.PathSeparator)
	rootInfo, err := os.Stat(root)
	require.NoError(t, err)
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join(owned, "work")
	cfg.API.Security.AllowedDirectories = []string{owned}
	cfg.API.Security.DeniedDirectories = []string{denied}
	deps := testkit.CreateTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	beforeConfig, err := json.Marshal(rt.Deps().CoreDeps.GetConfig())
	require.NoError(t, err)
	router := gin.New()
	fileapi.RegisterRoutes(router.Group("/api/v1"), rt)
	browse := func(path, scope string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(contracts.BrowseRequest{Path: path, Scope: scope})
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/browse", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	operational := browse(root, "")
	require.Equal(t, http.StatusForbidden, operational.Code)
	configured := browse(root, "configure")
	// Do not log or make assumptions about native root item names/count/order.
	require.Equal(t, http.StatusOK, configured.Code)
	var result contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(configured.Body.Bytes(), &result))
	require.Empty(t, result.ParentPath)
	require.True(t, filepath.IsAbs(result.CurrentPath))
	require.Equal(t, result.CurrentPath, filepath.Dir(result.CurrentPath))
	actualRoot, err := os.Stat(result.CurrentPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(rootInfo, actualRoot), "canonical response identifies the same native root")
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(configured.Body.Bytes(), &envelope))
	require.NotContains(t, envelope, "parent_path")
	require.Equal(t, http.StatusForbidden, browse(denied, "configure").Code, "configure scope retains configured deny rules")
	if os.PathSeparator == '/' {
		// Built-in Unix system deny rules reject this before any directory listing.
		require.Equal(t, http.StatusForbidden, browse("/dev", "configure").Code)
	}
	afterConfig, err := json.Marshal(rt.Deps().CoreDeps.GetConfig())
	require.NoError(t, err)
	require.Equal(t, beforeConfig, afterConfig)
	afterFile, err := os.Stat(sentinel)
	require.NoError(t, err)
	require.True(t, os.SameFile(beforeFile, afterFile))
	data, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, "preserved", string(data))
}
