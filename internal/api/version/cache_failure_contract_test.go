package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionStatus_BlockedCacheParentPreservesEmptyStateResponse(t *testing.T) {
	// A real filesystem obstruction makes the concrete service return its
	// error state without a Go error or any attempted update check.
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	original := []byte("keep this file")
	require.NoError(t, os.WriteFile(parent, original, 0o600))
	t.Setenv("JAVINIZER_DATA_DIR", parent)

	var requests atomic.Int32
	previousTransport := http.DefaultTransport
	http.DefaultTransport = versionReleaseTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, os.ErrPermission
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	state, err := update.NewService(update.UpdateConfig{Enabled: true}).GetStatus(req.Context())
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, update.UpdateSourceError, state.Source)
	require.NotEmpty(t, state.Error)
	require.Empty(t, state.CheckedAt)

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true
	deps := newTestVersionDeps(cfg)
	router := gin.New()
	router.GET("/version", versionStatus(deps.CoreDeps))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var response VersionStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assertVersionBuildMetadata(t, response)
	// Preserve the existing endpoint mapping: an empty CheckedAt takes the
	// none/empty response path, even when the service state records an error.
	assert.Equal(t, string(update.UpdateSourceNone), response.Source)
	assert.Empty(t, response.Error)
	assert.Empty(t, response.Latest)
	assert.Empty(t, response.CheckedAt)
	assert.False(t, response.UpdateAvailable)
	assert.False(t, response.Prerelease)
	assert.Zero(t, requests.Load())
	contents, err := os.ReadFile(parent)
	require.NoError(t, err)
	assert.Equal(t, original, contents)
}
