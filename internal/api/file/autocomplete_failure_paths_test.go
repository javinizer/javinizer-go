package file

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAutocompleteUnreadableDirectoryReturnsError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0111))
	t.Cleanup(func() { require.NoError(t, os.Chmod(root, 0700)) })
	if _, err := os.ReadDir(root); !os.IsPermission(err) {
		t.Skip("host does not enforce directory read permissions")
	}
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	payload, err := json.Marshal(contracts.PathAutocompleteRequest{Path: root + string(os.PathSeparator)})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/autocomplete", autocompletePath(testkit.GetTestRuntime(newTestDepsFromConfig(cfg))))
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/autocomplete", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), `"suggestions":`, "permission failure must not masquerade as an empty directory")
}

func TestAutocompleteAppliesLimitAfterSorting(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta", "beta", "Alpha"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0700))
	}
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	payload, err := json.Marshal(contracts.PathAutocompleteRequest{Path: root + string(os.PathSeparator), Limit: 2})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/autocomplete", autocompletePath(testkit.GetTestRuntime(newTestDepsFromConfig(cfg))))
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/autocomplete", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusOK, rec.Code)
	var result contracts.PathAutocompleteResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Len(t, result.Suggestions, 2)
	require.Equal(t, "Alpha", result.Suggestions[0].Name)
	require.Equal(t, "beta", result.Suggestions[1].Name)
}

func TestAutocompleteMissingWorkingDirectoryFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not permit removal of the active working directory")
	}
	root := t.TempDir()
	t.Chdir(root)
	require.NoError(t, os.Remove(root))
	path, fragment, err := resolveAutocompleteBasePath("relative", &core.SecurityNarrowConfig{}, "configure")
	require.True(t,
		errors.Is(err, apperrors.ErrPathInvalid) || errors.Is(err, apperrors.ErrPathNotExist),
		"removed working directory must reject a relative path, got %v", err)
	require.Empty(t, path)
	require.Empty(t, fragment)
}

func TestHomeDirectoryComparisonHandlesMissingPaths(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "missing-home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.True(t, isHomeDirectory(home))
	require.False(t, isHomeDirectory(filepath.Join(root, "other-missing-path")))
}
