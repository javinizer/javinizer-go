package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateBatchMoviePosterFromURL_V5_InvalidMovieID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	batchDeps := movieEditDepsFromCore(deps)

	tests := []struct {
		name     string
		resultID string
	}{
		{"empty", ""},
		{"dot", "."},
		{"path traversal", "../etc/passwd"},
		{"subdirectory", "sub/movie"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			c.Params = gin.Params{
				{Key: "id", Value: "test-job"},
				{Key: "resultId", Value: tt.resultID},
			}

			body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "http://example.com/poster.jpg"})
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			// Re-create with body
			c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			handler := updateBatchMoviePosterFromURL(batchDeps)
			handler(c)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestUpdateBatchMoviePosterFromURL_V5_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	batchDeps := movieEditDepsFromCore(deps)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	c.Params = gin.Params{
		{Key: "id", Value: "nonexistent-job"},
		{Key: "resultId", Value: "some-result-id"},
	}

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "http://example.com/poster.jpg"})
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler := updateBatchMoviePosterFromURL(batchDeps)
	handler(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestResolvePosterID_V5_InvalidMovieID(t *testing.T) {
	lookup := worker.NewResultTracker(0, nil)
	_, err := resolvePosterID(lookup, "")
	assert.Error(t, err)
}

func TestResolvePosterID_V5_PathTraversal(t *testing.T) {
	lookup := worker.NewResultTracker(0, nil)
	_, err := resolvePosterID(lookup, "../etc/passwd")
	assert.Error(t, err)
}

func TestResolvePosterID_V5_DotMovieID(t *testing.T) {
	lookup := worker.NewResultTracker(0, nil)
	_, err := resolvePosterID(lookup, ".")
	assert.Error(t, err)
}

func TestResolvePosterID_V5_ValidMovieID(t *testing.T) {
	lookup := worker.NewResultTracker(0, nil)
	id, err := resolvePosterID(lookup, "ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "ABC-123", id)
}

func TestBulkExcludeMaxMovies_V5_Constant(t *testing.T) {
	assert.Equal(t, 100, bulkExcludeMaxMovies)
}
