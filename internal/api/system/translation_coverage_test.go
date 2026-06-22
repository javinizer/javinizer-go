package system

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// --- fetchOpenAICompatibleModels: success path ---

func TestTransCov_FetchOpenAIModels_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "/models", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4"},{"id":"gpt-3.5-turbo"},{"id":"gpt-4"}]}`))
	}))
	defer upstream.Close()

	models, err := fetchOpenAICompatibleModels(context.Background(), upstream.URL, "test-key")
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-3.5-turbo", "gpt-4"}, models, "should be deduplicated and sorted")
}

// --- fetchOpenAICompatibleModels: context cancellation ---

func TestTransCov_FetchOpenAIModels_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchOpenAICompatibleModels(ctx, "https://example.com", "key")
	require.Error(t, err)
}

// --- fetchOpenAICompatibleModels: non-200 status ---

func TestTransCov_FetchOpenAIModels_Non200Status(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer upstream.Close()

	_, err := fetchOpenAICompatibleModels(context.Background(), upstream.URL, "bad-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upstream returned status 401")
}

// --- fetchAnthropicModels: success path ---

func TestTransCov_FetchAnthropicModels_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-anthropic-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "/v1/models", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-3-opus"},{"id":"claude-3-sonnet"}]}`))
	}))
	defer upstream.Close()

	models, err := fetchAnthropicModels(context.Background(), upstream.URL, "test-anthropic-key")
	require.NoError(t, err)
	assert.Equal(t, []string{"claude-3-opus", "claude-3-sonnet"}, models)
}

// --- fetchAnthropicModels: context cancellation ---

func TestTransCov_FetchAnthropicModels_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchAnthropicModels(ctx, "https://example.com", "key")
	require.Error(t, err)
}

// --- fetchAnthropicModels: non-200 status ---

func TestTransCov_FetchAnthropicModels_Non200Status(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer upstream.Close()

	_, err := fetchAnthropicModels(context.Background(), upstream.URL, "bad-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upstream returned status 403")
}

// --- fetchAnthropicModels: invalid payload ---

func TestTransCov_FetchAnthropicModels_InvalidPayload(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer upstream.Close()

	_, err := fetchAnthropicModels(context.Background(), upstream.URL, "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid upstream response payload")
}

// --- fetchAnthropicModels: empty models ---

func TestTransCov_FetchAnthropicModels_EmptyModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	_, err := fetchAnthropicModels(context.Background(), upstream.URL, "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}

// --- fetchDeepLUsage: success path ---

func TestTransCov_FetchDeepLUsage_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DeepL-Auth-Key test-deepl-key", r.Header.Get("Authorization"))
		assert.Equal(t, "/v2/usage", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"character_count":5000,"character_limit":50000}`))
	}))
	defer upstream.Close()

	usage, err := fetchDeepLUsage(context.Background(), upstream.URL, "test-deepl-key")
	require.NoError(t, err)
	assert.Equal(t, int64(5000), usage.CharacterCount)
	assert.Equal(t, int64(50000), usage.CharacterLimit)
}

// --- fetchDeepLUsage: non-200 status ---

func TestTransCov_FetchDeepLUsage_Non200Status(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
	}))
	defer upstream.Close()

	_, err := fetchDeepLUsage(context.Background(), upstream.URL, "bad-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deepl returned status 403")
}

// --- fetchDeepLUsage: context cancellation ---

func TestTransCov_FetchDeepLUsage_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchDeepLUsage(ctx, "https://api-free.deepl.com", "key")
	require.Error(t, err)
}

// --- fetchDeepLUsage: invalid response body ---

func TestTransCov_FetchDeepLUsage_InvalidBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer upstream.Close()

	_, err := fetchDeepLUsage(context.Background(), upstream.URL, "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid deepl usage response")
}

// --- getTranslationModels: openai-compatible provider (no API key required) ---

func TestTransCov_GetTranslationModels_OpenAICompatible(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"local-model-1"}]}`))
	}))
	defer upstream.Close()

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/translation/models", getTranslationModels(deps))

	body := `{"provider":"openai-compatible","base_url":"` + upstream.URL + `","api_key":"key"}`
	req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response contracts.TranslationModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Contains(t, response.Models, "local-model-1")
}

// --- getTranslationModels: anthropic provider with valid response ---

func TestTransCov_GetTranslationModels_AnthropicSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-3-opus"}]}`))
	}))
	defer upstream.Close()

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/translation/models", getTranslationModels(deps))

	body := `{"provider":"anthropic","base_url":"` + upstream.URL + `","api_key":"sk-ant-key"}`
	req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response contracts.TranslationModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Contains(t, response.Models, "claude-3-opus")
}

// --- getTranslationModels: anthropic with missing API key ---

func TestTransCov_GetTranslationModels_AnthropicMissingKey(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/translation/models", getTranslationModels(deps))

	body := `{"provider":"anthropic","base_url":"https://api.anthropic.com","api_key":""}`
	req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "api_key is required for model discovery")
}

// --- getTranslationModels: anthropic with invalid URL ---

func TestTransCov_GetTranslationModels_AnthropicInvalidURL(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/translation/models", getTranslationModels(deps))

	body := `{"provider":"anthropic","base_url":"not-a-url","api_key":"key"}`
	req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "base_url must be a valid http(s) URL")
}

// --- getDeepLUsage: pro mode with default base URL ---

func TestTransCov_GetDeepLUsage_ProDefaultURL(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/deepl/usage", getDeepLUsage(deps))

	// Pro mode with empty base_url should use api.deepl.com
	body := `{"mode":"pro","base_url":"","api_key":"test-key"}`
	req := httptest.NewRequest(http.MethodPost, "/deepl/usage", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Will fail because we can't actually reach DeepL, but validates the mode was accepted
	// (not a 400 "invalid mode" error)
	assert.NotContains(t, w.Body.String(), "mode must be either")
}

// --- getDeepLUsage: free mode with default base URL ---

func TestTransCov_GetDeepLUsage_FreeDefaultURL(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"character_count":100,"character_limit":500000}`))
	}))
	defer upstream.Close()

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)

	router := gin.New()
	router.POST("/deepl/usage", getDeepLUsage(deps))

	// Free mode with explicit base_url pointing to our test server
	body := `{"mode":"free","base_url":"` + upstream.URL + `","api_key":"test-key"}`
	req := httptest.NewRequest(http.MethodPost, "/deepl/usage", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response DeepLUsageResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, int64(100), response.CharacterCount)
	assert.Equal(t, int64(500000), response.CharacterLimit)
}

// --- fetchOpenAICompatibleModels: whitespace trimming in model IDs ---

func TestTransCov_FetchOpenAIModels_WhitespaceTrimming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"  model-a  "},{"id":"model-b"}]}`))
	}))
	defer upstream.Close()

	models, err := fetchOpenAICompatibleModels(context.Background(), upstream.URL, "key")
	require.NoError(t, err)
	assert.Contains(t, models, "model-a")
	assert.Contains(t, models, "model-b")
}

// --- fetchOpenAICompatibleModels: empty model IDs are skipped ---

func TestTransCov_FetchOpenAIModels_EmptyIDSkipped(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":""},{"id":"   "},{"id":"valid-model"}]}`))
	}))
	defer upstream.Close()

	models, err := fetchOpenAICompatibleModels(context.Background(), upstream.URL, "key")
	require.NoError(t, err)
	assert.Equal(t, []string{"valid-model"}, models)
}

// --- fetchDeepLUsage: with optional fields ---

func TestTransCov_FetchDeepLUsage_WithOptionalFields(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"character_count": 5000,
			"character_limit": 50000,
			"start_time": "2026-01-01",
			"end_time": "2026-12-31",
			"api_key_character_count": 3000,
			"api_key_character_limit": 50000
		}`))
	}))
	defer upstream.Close()

	usage, err := fetchDeepLUsage(context.Background(), upstream.URL, "key")
	require.NoError(t, err)
	assert.Equal(t, int64(5000), usage.CharacterCount)
	assert.Equal(t, int64(50000), usage.CharacterLimit)
	assert.Equal(t, "2026-01-01", usage.StartTime)
	assert.Equal(t, "2026-12-31", usage.EndTime)
	assert.Equal(t, int64(3000), usage.APIKeyCount)
	assert.Equal(t, int64(50000), usage.APIKeyLimit)
}
