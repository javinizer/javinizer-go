package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDisplayTitlePreviewConfig(tmpl string) *config.Config {
	return &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{DisplayTitle: tmpl},
			},
		},
	}
}

func TestPreviewDisplayTitle_RendersTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	deps := createTestDeps(t, newDisplayTitlePreviewConfig("[<ID>] <TITLE>"), "")
	batchDeps := testkit.GetTestRuntime(deps)

	router := gin.New()
	router.POST("/api/v1/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(batchDeps))

	body, _ := json.Marshal(contracts.DisplayTitlePreviewRequest{
		Movie: &contracts.MovieView{ID: "MKMP-094", Title: "Ayaka Tomoda"},
	})
	req := httptest.NewRequest("POST", "/api/v1/batch/test-id/results/ABC-001/display-title-preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp contracts.DisplayTitlePreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "[MKMP-094] Ayaka Tomoda", resp.DisplayTitle)
}

func TestPreviewDisplayTitle_EmptyTemplateFallsBackToTitle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	deps := createTestDeps(t, newDisplayTitlePreviewConfig(""), "")
	batchDeps := testkit.GetTestRuntime(deps)

	router := gin.New()
	router.POST("/api/v1/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(batchDeps))

	body, _ := json.Marshal(contracts.DisplayTitlePreviewRequest{
		Movie: &contracts.MovieView{ID: "MKMP-094", Title: "Ayaka Tomoda"},
	})
	req := httptest.NewRequest("POST", "/api/v1/batch/test-id/results/ABC-001/display-title-preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp contracts.DisplayTitlePreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Ayaka Tomoda", resp.DisplayTitle)
}

func TestPreviewDisplayTitle_MissingMovie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	deps := createTestDeps(t, newDisplayTitlePreviewConfig("[<ID>] <TITLE>"), "")
	batchDeps := testkit.GetTestRuntime(deps)

	router := gin.New()
	router.POST("/api/v1/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/test-id/results/ABC-001/display-title-preview", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
