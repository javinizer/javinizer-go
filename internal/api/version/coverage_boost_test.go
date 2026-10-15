package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- versionCheck: enabled with a deterministic successful release response ---

func TestVersionCheck_EnabledReleaseFixture(t *testing.T) {
	mockVersionRelease(t, http.StatusOK, `{"tag_name":"v9.9.9","prerelease":false}`)
	gin.SetMode(gin.TestMode)

	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true

	deps := &core.APIDeps{}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	testkit.SetTestRuntime(deps, rt)

	router := gin.New()
	router.POST("/version/check", versionCheck(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodPost, "/version/check", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assertVersionBuildMetadata(t, resp)
	assert.Equal(t, string(update.UpdateSourceFresh), resp.Source)
	assert.Equal(t, "v9.9.9", resp.Latest)
	assert.True(t, resp.UpdateAvailable)
	assert.False(t, resp.Prerelease)
	assert.NotEmpty(t, resp.CheckedAt)
	assert.Empty(t, resp.Error)

	state, err := update.NewService(update.UpdateConfig{Enabled: true}).GetStatus(req.Context())
	require.NoError(t, err)
	assert.Equal(t, update.UpdateSourceFresh, state.Source)
	assert.Equal(t, resp.Latest, state.Version)
	assert.Equal(t, resp.CheckedAt, state.CheckedAt)
	assert.Equal(t, resp.UpdateAvailable, state.Available)
	assert.Equal(t, resp.Prerelease, state.Prerelease)
	assert.Empty(t, state.Error)
}
