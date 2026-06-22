package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	assert.NotEmpty(t, resp.Source)
}

func TestVersionCheck_CachedState(t *testing.T) {
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
	assert.NotEmpty(t, resp.Source)
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
