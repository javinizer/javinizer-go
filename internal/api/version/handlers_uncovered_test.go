package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionStatusResponse_Fields_Uncovered(t *testing.T) {
	// Verify the response struct has all expected fields
	resp := VersionStatusResponse{
		Current:         "1.0.0",
		Commit:          "abc123",
		BuildDate:       "2026-01-01",
		Latest:          "1.1.0",
		UpdateAvailable: true,
		Prerelease:      false,
		CheckedAt:       "2026-01-01T00:00:00Z",
		Source:          string(update.UpdateSourceCached),
		Error:           "",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, "1.0.0", decoded["current"])
	assert.Equal(t, "abc123", decoded["commit"])
	assert.Equal(t, "1.1.0", decoded["latest"])
	assert.Equal(t, true, decoded["update_available"])
	assert.Equal(t, false, decoded["prerelease"])
	assert.Equal(t, "cached", decoded["source"])
}

func TestVersionCheck_DisabledConfig_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = false

	deps := newTestVersionDeps(cfg)

	router := gin.New()
	router.GET("/api/v1/version", versionStatus(deps.CoreDeps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp VersionStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, string(update.UpdateSourceDisabled), resp.Source)
	// When disabled, latest should be empty
	assert.Empty(t, resp.Latest)
}
