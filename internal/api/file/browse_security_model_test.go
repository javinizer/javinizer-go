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

func doBrowse(t *testing.T, router *gin.Engine, path, scope string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(contracts.BrowseRequest{Path: path, Scope: scope})
	req := httptest.NewRequest("POST", "/browse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func doAutocomplete(t *testing.T, router *gin.Engine, path, scope string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(contracts.PathAutocompleteRequest{Path: path, Scope: scope})
	req := httptest.NewRequest("POST", "/browse/autocomplete", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// --- Configure scope (unrestricted) ---

func TestBrowse_ConfigureScope_EmptyAllowlist(t *testing.T) {
	router, dir := newBrowseTestRouter(t, nil)
	canonical, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	w := doBrowse(t, router, dir, "configure")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, canonical, resp.CurrentPath)
	assert.Len(t, resp.Items, 3, "videos, movies, readme.txt")
}

func TestBrowse_ConfigureScope_DotOnlyAllowlist(t *testing.T) {
	router, dir := newBrowseTestRouter(t, []string{"."})
	canonical, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	w := doBrowse(t, router, dir, "configure")
	require.Equal(t, http.StatusOK, w.Code,
		"configure scope must not enforce the allowlist even when it is '.'; body=%s", w.Body.String())
	var resp contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, canonical, resp.CurrentPath)
}

func TestAutocomplete_ConfigureScope(t *testing.T) {
	router, dir := newBrowseTestRouter(t, nil)

	w := doAutocomplete(t, router, dir+string(os.PathSeparator), "configure")
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

func TestAutocomplete_ConfigureScope_FragmentFilter(t *testing.T) {
	router, dir := newBrowseTestRouter(t, nil)

	w := doAutocomplete(t, router, filepath.Join(dir, "vi"), "configure")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp contracts.PathAutocompleteResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Suggestions, 1, "only 'videos' matches fragment 'vi'")
	assert.Equal(t, "videos", resp.Suggestions[0].Name)
}

// --- Operation scope (allowlist-enforcing) ---

func TestBrowse_OperationScope_RejectsWhenAllowlistEmpty(t *testing.T) {
	router, dir := newBrowseTestRouter(t, nil)

	w := doBrowse(t, router, dir, "")
	assert.Equal(t, http.StatusForbidden, w.Code,
		"operation scope must reject when allowlist is empty; body=%s", w.Body.String())
}

func TestBrowse_OperationScope_RejectsPathOutsideAllowlist(t *testing.T) {
	allowedDir := t.TempDir()
	router, _ := newBrowseTestRouter(t, []string{allowedDir})
	outsideDir := t.TempDir()

	w := doBrowse(t, router, outsideDir, "")
	assert.Equal(t, http.StatusForbidden, w.Code,
		"operation scope must reject a path outside the allowlist; body=%s", w.Body.String())
}

func TestBrowse_OperationScope_AllowsPathInsideAllowlist(t *testing.T) {
	_, dir := newBrowseTestRouter(t, []string{})
	allowedDir := filepath.Join(dir, "videos")
	router, _ := newBrowseTestRouter(t, []string{allowedDir})

	w := doBrowse(t, router, allowedDir, "")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.CurrentPath)
}

func TestAutocomplete_OperationScope_RejectsWhenAllowlistEmpty(t *testing.T) {
	router, dir := newBrowseTestRouter(t, nil)

	w := doAutocomplete(t, router, dir+string(os.PathSeparator), "")
	assert.Equal(t, http.StatusForbidden, w.Code,
		"operation autocomplete must reject when allowlist is empty; body=%s", w.Body.String())
}

func TestAutocomplete_OperationScope_RejectsPathOutsideAllowlist(t *testing.T) {
	allowedDir := t.TempDir()
	router, _ := newBrowseTestRouter(t, []string{allowedDir})
	outsideDir := t.TempDir()

	w := doAutocomplete(t, router, filepath.Join(outsideDir, "frag"), "")
	assert.Equal(t, http.StatusForbidden, w.Code,
		"operation autocomplete must reject a path outside the allowlist; body=%s", w.Body.String())
}

// --- Denylist applies to both scopes ---

func TestBrowse_Denylist_ConfigureScope(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not present on this platform")
	}
	router, _ := newBrowseTestRouter(t, nil)

	w := doBrowse(t, router, "/proc", "configure")
	assert.NotEqual(t, http.StatusOK, w.Code,
		"denylist must block /proc in configure scope")
}

func TestBrowse_Denylist_OperationScope(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not present on this platform")
	}
	router, _ := newBrowseTestRouter(t, []string{"/"})

	w := doBrowse(t, router, "/proc", "")
	assert.NotEqual(t, http.StatusOK, w.Code,
		"denylist must block /proc in operation scope")
}

// --- Empty path fallback ---

func TestBrowse_EmptyPath_ConfigureScopeFallsBackToHome(t *testing.T) {
	router, _ := newBrowseTestRouter(t, nil)

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	w := doBrowse(t, router, "", "configure")
	assert.Equal(t, http.StatusOK, w.Code,
		"empty path with no home dir must fall back to Getwd and succeed in configure scope")
}

func TestBrowse_EmptyPath_ConfigureScopeUsesHomeDir(t *testing.T) {
	homeDir := t.TempDir()
	router, _ := newBrowseTestRouter(t, nil)

	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	w := doBrowse(t, router, "", "configure")
	assert.Equal(t, http.StatusOK, w.Code,
		"empty path with a valid home dir must default to home and succeed in configure scope")

	var resp contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	resolvedHome, err := filepath.EvalSymlinks(homeDir)
	require.NoError(t, err)
	resolvedCurrent, err := filepath.EvalSymlinks(resp.CurrentPath)
	require.NoError(t, err)
	assert.Equal(t, resolvedHome, resolvedCurrent)
}

func TestBrowse_EmptyPath_OperationScopeRejectsWhenAllowlistEmpty(t *testing.T) {
	router, _ := newBrowseTestRouter(t, nil)

	w := doBrowse(t, router, "", "")
	assert.Equal(t, http.StatusForbidden, w.Code,
		"operation scope with empty allowlist and empty path must 403")
}

func TestBrowse_EmptyPath_OperationScope_DefaultsToFirstAllowedDir(t *testing.T) {
	allowedDir := t.TempDir()
	router, _ := newBrowseTestRouter(t, []string{allowedDir})

	w := doBrowse(t, router, "", "")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	allowedCanonical, err := filepath.EvalSymlinks(allowedDir)
	require.NoError(t, err)
	respCanonical, err := filepath.EvalSymlinks(resp.CurrentPath)
	require.NoError(t, err)
	assert.Equal(t, allowedCanonical, respCanonical)
}
