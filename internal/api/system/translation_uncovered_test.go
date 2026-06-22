package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDeepLUsage_InvalidMode_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/deepl/usage", getDeepLUsage(deps))

	body := `{"mode": "invalid", "api_key": "test-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/deepl/usage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetDeepLUsage_MissingAPIKey_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/deepl/usage", getDeepLUsage(deps))

	body := `{"mode": "free"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/deepl/usage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetDeepLUsage_InvalidRequestBody_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/deepl/usage", getDeepLUsage(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/deepl/usage", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetTranslationModels_InvalidProvider_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/models", getTranslationModels(deps))

	body := `{"provider": "unknown_provider"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/models", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetTranslationModels_InvalidRequestBody_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/models", getTranslationModels(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/models", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetTranslationModels_OpenAI_MissingAPIKey_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/models", getTranslationModels(deps))

	body := `{"provider": "openai", "base_url": "https://api.openai.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/models", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetTranslationModels_OpenAI_InvalidBaseURL_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/models", getTranslationModels(deps))

	body := `{"provider": "openai", "base_url": "not-a-url", "api_key": "sk-test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/models", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetTranslationModels_Anthropic_MissingAPIKey_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/models", getTranslationModels(deps))

	body := `{"provider": "anthropic", "base_url": "https://api.anthropic.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/models", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeepLUsageResponse_JSONRoundTrip_Uncovered(t *testing.T) {
	resp := DeepLUsageResponse{
		CharacterCount: 1000,
		CharacterLimit: 50000,
		StartTime:      "2026-01-01",
		EndTime:        "2026-12-31",
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded DeepLUsageResponse
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, int64(1000), decoded.CharacterCount)
	assert.Equal(t, int64(50000), decoded.CharacterLimit)
}

func TestGetDeepLUsage_DefaultMode_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/api/v1/translation/deepl/usage", getDeepLUsage(deps))

	// Empty mode defaults to "free" - still needs API key
	body := `{"mode": "", "api_key": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/translation/deepl/usage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
