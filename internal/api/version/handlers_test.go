package version

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func writeTestState(t *testing.T, path, ver, checkedAt string, available, prerelease bool, source update.UpdateSource, errMsg string) {
	t.Helper()
	state := map[string]any{
		"version":    ver,
		"checked_at": checkedAt,
		"available":  available,
		"prerelease": prerelease,
		"source":     string(source),
	}
	if errMsg != "" {
		state["error"] = errMsg
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func assertVersionBuildMetadata(t *testing.T, resp VersionStatusResponse) {
	t.Helper()
	assert.Equal(t, appversion.Short(), resp.Current)
	assert.Equal(t, appversion.Commit, resp.Commit)
	assert.Equal(t, appversion.BuildDate, resp.BuildDate)
}

// newTestVersionDeps creates a VersionDeps for testing with the given config.
func newTestVersionDeps(cfg *config.Config) *core.APIDeps {
	return newTestDeps(cfg)
}

func newTestDeps(cfg *config.Config) *core.APIDeps {
	deps := &core.APIDeps{}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)
	return deps
}

func TestVersionStatus(t *testing.T) {
	t.Run("disabled state", func(t *testing.T) {
		tempDataDir := t.TempDir()
		t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

		cfg := config.DefaultConfig(nil, nil)
		cfg.System.VersionCheckEnabled = false

		deps := newTestVersionDeps(cfg)

		router := gin.New()
		router.GET("/version", versionStatus(deps.CoreDeps))

		req := httptest.NewRequest(http.MethodGet, "/version", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var resp VersionStatusResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, string(update.UpdateSourceDisabled), resp.Source)
		assert.False(t, resp.UpdateAvailable)
		assert.Equal(t, "", resp.CheckedAt)
		assert.Equal(t, "", resp.Latest)
		assertVersionBuildMetadata(t, resp)
	})

	t.Run("none state when cache does not exist", func(t *testing.T) {
		tempDataDir := t.TempDir()
		t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

		cfg := config.DefaultConfig(nil, nil)
		cfg.System.VersionCheckEnabled = true

		deps := newTestVersionDeps(cfg)

		router := gin.New()
		router.GET("/version", versionStatus(deps.CoreDeps))

		req := httptest.NewRequest(http.MethodGet, "/version", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var resp VersionStatusResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, string(update.UpdateSourceNone), resp.Source)
		assert.False(t, resp.UpdateAvailable)
		assert.Equal(t, "", resp.CheckedAt)
		assert.Equal(t, "", resp.Latest)
		assertVersionBuildMetadata(t, resp)
	})

	t.Run("cached state from update cache", func(t *testing.T) {
		tempDataDir := t.TempDir()
		t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

		checkedAt := time.Now().UTC().Format(time.RFC3339)
		statePath := filepath.Join(tempDataDir, "update_cache.json")
		writeTestState(t, statePath, "v9.9.9", checkedAt, true, true, update.UpdateSourceCached, "cached error")

		cfg := config.DefaultConfig(nil, nil)
		cfg.System.VersionCheckEnabled = true

		deps := newTestVersionDeps(cfg)

		router := gin.New()
		router.GET("/version", versionStatus(deps.CoreDeps))

		req := httptest.NewRequest(http.MethodGet, "/version", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var resp VersionStatusResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, string(update.UpdateSourceCached), resp.Source)
		assert.Equal(t, "v9.9.9", resp.Latest)
		assert.Equal(t, checkedAt, resp.CheckedAt)
		assert.True(t, resp.UpdateAvailable)
		assert.True(t, resp.Prerelease)
		assert.Equal(t, "cached error", resp.Error)
		assertVersionBuildMetadata(t, resp)
	})
}

func TestVersionCheck_EnabledNoCache(t *testing.T) {
	mockVersionRelease(t, http.StatusServiceUnavailable, "")
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true

	deps := newTestVersionDeps(cfg)

	router := gin.New()
	router.POST("/version/check", versionCheck(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodPost, "/version/check", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assertVersionBuildMetadata(t, resp)
	assert.Equal(t, string(update.UpdateSourceError), resp.Source)
	assert.Equal(t, "GitHub API returned status 503", resp.Error)
	assert.Empty(t, resp.Latest)
	assert.Empty(t, resp.CheckedAt)
	assert.False(t, resp.UpdateAvailable)
	assert.False(t, resp.Prerelease)
	assert.NoFileExists(t, filepath.Join(tempDataDir, "update_cache.json"))
}

func TestVersionCheck_CachedState(t *testing.T) {
	mockVersionRelease(t, http.StatusServiceUnavailable, "")
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	checkedAt := time.Now().UTC().Format(time.RFC3339)
	statePath := filepath.Join(tempDataDir, "update_cache.json")
	writeTestState(t, statePath, "v9.9.9", checkedAt, true, false, update.UpdateSourceFresh, "")

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true

	deps := newTestVersionDeps(cfg)

	router := gin.New()
	router.POST("/version/check", versionCheck(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodPost, "/version/check", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assertVersionBuildMetadata(t, resp)
	assert.Equal(t, string(update.UpdateSourceCached), resp.Source)
	assert.Equal(t, "GitHub API returned status 503", resp.Error)
	assert.Equal(t, "v9.9.9", resp.Latest)
	assert.Equal(t, checkedAt, resp.CheckedAt)
	assert.True(t, resp.UpdateAvailable)
	assert.False(t, resp.Prerelease)

	// The failed refresh retains the last successful release and records the
	// synthetic error in the persisted state for subsequent status requests.
	state, err := update.NewService(update.UpdateConfig{Enabled: true}).GetStatus(req.Context())
	require.NoError(t, err)
	assert.Equal(t, update.UpdateSourceCached, state.Source)
	assert.Equal(t, resp.Error, state.Error)
	assert.Equal(t, resp.Latest, state.Version)
	assert.Equal(t, resp.CheckedAt, state.CheckedAt)
	assert.Equal(t, resp.UpdateAvailable, state.Available)
	assert.Equal(t, resp.Prerelease, state.Prerelease)
}

func TestVersionStatus_ErrorState(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true

	deps := newTestVersionDeps(cfg)

	router := gin.New()
	router.GET("/version", versionStatus(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, string(update.UpdateSourceNone), resp.Source)
	assert.False(t, resp.UpdateAvailable)
	assertVersionBuildMetadata(t, resp)
}

func TestVersionCheck_Disabled(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = false

	deps := newTestVersionDeps(cfg)

	router := gin.New()
	router.POST("/version/check", versionCheck(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodPost, "/version/check", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assertVersionBuildMetadata(t, resp)
	assert.Equal(t, string(update.UpdateSourceDisabled), resp.Source)
	assert.Equal(t, "", resp.Latest)
	assert.False(t, resp.Prerelease)
	assert.False(t, resp.UpdateAvailable)
	assert.Equal(t, "", resp.CheckedAt)
	assert.Equal(t, "", resp.Error)
}

// versionReleaseTransport satisfies the real GitHub checker's request without
// ever opening a socket. Tests replacing http.DefaultTransport must not run in
// parallel; t.Setenv in each caller also enforces that requirement.
type versionReleaseTransport func(*http.Request) (*http.Response, error)

func (f versionReleaseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func mockVersionRelease(t *testing.T, status int, body string) {
	t.Helper()
	var calls atomic.Int32
	original := http.DefaultTransport
	http.DefaultTransport = versionReleaseTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, "https://api.github.com/repos/javinizer/javinizer-go/releases/latest", req.URL.String())
		assert.Equal(t, "application/vnd.github.v3+json", req.Header.Get("Accept"))
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() {
		http.DefaultTransport = original
		assert.Equal(t, int32(1), calls.Load(), "force-check must reach the release fixture exactly once")
	})
}
