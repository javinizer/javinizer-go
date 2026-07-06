package file

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// These tests encode the security model: the allowlist is a safety guard for
// file OPERATIONS (scan/organize), not a restriction on BROWSING. Browse and
// autocomplete never enforce the allowlist — otherwise configuring the
// allowlist is a catch-22 (you can't browse to add the first directory, or to
// add a directory on another drive like D:\). The denylist (/proc, /sys, /dev
// + config) still applies. This holds regardless of install environment.

func newBrowseTestRouter(t *testing.T, allowedDirs []string) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "videos"), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "movies"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0644))

	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = allowedDirs

	deps := newTestDepsFromConfig(cfg)
	router := gin.New()
	router.POST("/browse", browseDirectory(testkit.GetTestRuntime(deps)))
	router.POST("/browse/autocomplete", autocompletePath(testkit.GetTestRuntime(deps)))
	return router, dir
}

// TestBrowse_NeverEnforcesAllowlist_EmptyAllowlist: first-time setup (no
// allowed dirs) — browse must still list the directory.
func TestBrowse_NeverEnforcesAllowlist_EmptyAllowlist(t *testing.T) {
	router, dir := newBrowseTestRouter(t, nil)
	canonical, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	body, _ := json.Marshal(contracts.BrowseRequest{Path: dir})
	req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, canonical, resp.CurrentPath)
	assert.Len(t, resp.Items, 3, "videos, movies, readme.txt")
}

// TestBrowse_NeverEnforcesAllowlist_DotOnlyAllowlist: the default desktop
// config (allowed_directories: ['.']) — browse must list a directory outside
// the CWD (the Windows D:\ catch-22).
func TestBrowse_NeverEnforcesAllowlist_DotOnlyAllowlist(t *testing.T) {
	router, dir := newBrowseTestRouter(t, []string{"."})
	canonical, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	body, _ := json.Marshal(contracts.BrowseRequest{Path: dir})
	req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"browse must not enforce the allowlist even when it is '.'; body=%s", w.Body.String())
	var resp contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, canonical, resp.CurrentPath)
}

// TestAutocomplete_NeverEnforcesAllowlist: autocomplete must return
// suggestions regardless of the allowlist (empty here).
func TestAutocomplete_NeverEnforcesAllowlist(t *testing.T) {
	router, dir := newBrowseTestRouter(t, nil)

	// Trailing separator → list contents of dir, fragment "" → all dirs.
	body, _ := json.Marshal(contracts.PathAutocompleteRequest{Path: dir + string(os.PathSeparator)})
	req := httptest.NewRequest("POST", "/browse/autocomplete", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp contracts.PathAutocompleteResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	names := make([]string, 0, len(resp.Suggestions))
	for _, s := range resp.Suggestions {
		names = append(names, s.Name)
	}
	assert.Contains(t, names, "videos")
	assert.Contains(t, names, "movies")
	assert.NotContains(t, names, "readme.txt", "only directories are suggested")
}

// TestAutocomplete_NeverEnforcesAllowlist_FragmentFilter: typing a partial
// path filters by the fragment even with an empty allowlist.
func TestAutocomplete_NeverEnforcesAllowlist_FragmentFilter(t *testing.T) {
	router, dir := newBrowseTestRouter(t, nil)

	body, _ := json.Marshal(contracts.PathAutocompleteRequest{Path: filepath.Join(dir, "vi")})
	req := httptest.NewRequest("POST", "/browse/autocomplete", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp contracts.PathAutocompleteResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Suggestions, 1, "only 'videos' matches fragment 'vi'")
	assert.Equal(t, "videos", resp.Suggestions[0].Name)
}

// TestBrowse_StillEnforcesDenylist: even without the allowlist, the denylist
// (built-in /proc, /sys, /dev) still blocks sensitive system directories.
func TestBrowse_StillEnforcesDenylist(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not present on this platform")
	}
	router, _ := newBrowseTestRouter(t, nil)

	body, _ := json.Marshal(contracts.BrowseRequest{Path: "/proc"})
	req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code,
		"denylist must still block /proc even without the allowlist")
}

// TestBrowse_EmptyPathFallsBackToGetwd covers the os.Getwd() fallback in the
// empty-path branch: when os.UserHomeDir() is unavailable (HOME unset), the
// handler must fall back to the process working directory rather than fail.
func TestBrowse_EmptyPathFallsBackToGetwd(t *testing.T) {
	router, _ := newBrowseTestRouter(t, nil)

	// Force os.UserHomeDir() to fail so the Getwd fallback is taken. On Unix
	// an empty HOME makes UserHomeDir return ("", error); on Windows the same
	// applies via USERPROFILE. t.Setenv restores them after the test.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	body, _ := json.Marshal(contracts.BrowseRequest{Path: ""})
	req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// The test process's CWD is a real, non-denied directory, so the Getwd
	// fallback must resolve it and return a listing (200), not an error.
	assert.Equal(t, http.StatusOK, w.Code,
		"empty path with no home dir must fall back to Getwd and succeed")
}
