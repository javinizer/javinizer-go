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
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- versionCheck: enabled with check reaching GitHub (fresh or error state) ---

func TestVersionCheck_EnabledReachesGitHub(t *testing.T) {
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
	assert.Equal(t, appversion.Short(), resp.Current)
	assert.NotEmpty(t, resp.Source)
	// Source is either "fresh" (GitHub reachable) or "error" (GitHub unreachable)
	assert.Contains(t, []string{string(update.UpdateSourceFresh), string(update.UpdateSourceError), string(update.UpdateSourceCached)}, resp.Source)
}
