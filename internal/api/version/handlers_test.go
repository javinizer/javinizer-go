package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func assertVersionBuildMetadata(t *testing.T, resp VersionStatusResponse) {
	t.Helper()
	assert.Equal(t, appversion.Short(), resp.Current)
	assert.Equal(t, appversion.Commit, resp.Commit)
	assert.Equal(t, appversion.BuildDate, resp.BuildDate)
}

func TestVersionStatus(t *testing.T) {
	t.Run("disabled state", func(t *testing.T) {
		tempDataDir := t.TempDir()
		t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

		cfg := config.DefaultConfig()
		cfg.System.VersionCheckEnabled = false

		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.GET("/version", versionStatus(deps))

		req := httptest.NewRequest(http.MethodGet, "/version", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var resp VersionStatusResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "disabled", resp.Source)
		assert.False(t, resp.UpdateAvailable)
		assert.Equal(t, "", resp.CheckedAt)
		assert.Equal(t, "", resp.Latest)
		assertVersionBuildMetadata(t, resp)
	})

	t.Run("none state when cache does not exist", func(t *testing.T) {
		tempDataDir := t.TempDir()
		t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

		cfg := config.DefaultConfig()
		cfg.System.VersionCheckEnabled = true

		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.GET("/version", versionStatus(deps))

		req := httptest.NewRequest(http.MethodGet, "/version", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var resp VersionStatusResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "none", resp.Source)
		assert.False(t, resp.UpdateAvailable)
		assert.Equal(t, "", resp.CheckedAt)
		assert.Equal(t, "", resp.Latest)
		assertVersionBuildMetadata(t, resp)
	})

	t.Run("cached state from update cache", func(t *testing.T) {
		tempDataDir := t.TempDir()
		t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

		checkedAt := time.Now().UTC().Format(time.RFC3339)
		state := &update.UpdateState{
			Version:    "v9.9.9",
			CheckedAt:  checkedAt,
			Available:  true,
			Prerelease: true,
			Source:     "cached",
			Error:      "cached error",
		}
		statePath := filepath.Join(tempDataDir, "update_cache.json")
		require.NoError(t, update.SaveStateToFile(statePath, state))

		cfg := config.DefaultConfig()
		cfg.System.VersionCheckEnabled = true

		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.GET("/version", versionStatus(deps))

		req := httptest.NewRequest(http.MethodGet, "/version", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var resp VersionStatusResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "cached", resp.Source)
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

	cfg := config.DefaultConfig()
	cfg.System.VersionCheckEnabled = true

	deps := &ServerDependencies{}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/version/check", versionCheck(deps))

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
	state := &update.UpdateState{
		Version:    "v9.9.9",
		CheckedAt:  checkedAt,
		Available:  true,
		Prerelease: false,
		Source:     "fresh",
	}
	statePath := filepath.Join(tempDataDir, "update_cache.json")
	require.NoError(t, update.SaveStateToFile(statePath, state))

	cfg := config.DefaultConfig()
	cfg.System.VersionCheckEnabled = true

	deps := &ServerDependencies{}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/version/check", versionCheck(deps))

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

	cfg := config.DefaultConfig()
	cfg.System.VersionCheckEnabled = true

	deps := &ServerDependencies{}
	deps.SetConfig(cfg)

	router := gin.New()
	router.GET("/version", versionStatus(deps))

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "none", resp.Source)
	assert.False(t, resp.UpdateAvailable)
	assertVersionBuildMetadata(t, resp)
}

func TestVersionCheck_Disabled(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	cfg := config.DefaultConfig()
	cfg.System.VersionCheckEnabled = false

	deps := &ServerDependencies{}
	deps.SetConfig(cfg)

	router := gin.New()
	router.POST("/version/check", versionCheck(deps))

	req := httptest.NewRequest(http.MethodPost, "/version/check", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assertVersionBuildMetadata(t, resp)
	assert.Equal(t, "disabled", resp.Source)
	assert.Equal(t, "", resp.Latest)
	assert.False(t, resp.Prerelease)
	assert.False(t, resp.UpdateAvailable)
	assert.Equal(t, "", resp.CheckedAt)
	assert.Equal(t, "", resp.Error)
}
