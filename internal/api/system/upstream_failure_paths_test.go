package system

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestTranslationFetchRejectsTruncatedBodies(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic", "deepl"} {
		t.Run(kind, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "100")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"data":`)
			}))
			defer upstream.Close()
			var err error
			switch kind {
			case "openai":
				result, e := fetchOpenAICompatibleModels(t.Context(), upstream.URL, "test-key")
				err = e
				require.Nil(t, result)
			case "anthropic":
				result, e := fetchAnthropicModels(t.Context(), upstream.URL, "test-key")
				err = e
				require.Nil(t, result)
			case "deepl":
				result, e := fetchDeepLUsage(t.Context(), upstream.URL, "test-key")
				err = e
				require.Nil(t, result)
			}
			require.ErrorIs(t, err, io.ErrUnexpectedEOF, "a partial body must fail at transport reading, not silently decode")
		})
	}
}
func TestTranslationFetchRejectsMalformedEndpoint(t *testing.T) {
	result, err := fetchAnthropicModels(context.Background(), "http://%invalid", "test-key")
	require.Error(t, err)
	require.Nil(t, result)
	usage, err := fetchDeepLUsage(context.Background(), "http://%invalid", "test-key")
	require.Error(t, err)
	require.Nil(t, usage)
}
func TestAnthropicModelsIgnoreBlankAndDuplicateIDs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":" "},{"id":"model-b"},{"id":"model-a"},{"id":"model-a"},{"id":""}]}`)
	}))
	defer upstream.Close()
	result, err := fetchAnthropicModels(t.Context(), upstream.URL, "test-key")
	require.NoError(t, err)
	require.Equal(t, []string{"model-a", "model-b"}, result)
}
func TestAnthropicDiscoveryReturnsBadGatewayForUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	payload, err := json.Marshal(map[string]string{"provider": "anthropic", "base_url": upstream.URL, "api_key": "test-key"})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/translation/models", strings.NewReader(string(payload)))
	c.Request.Header.Set("Content-Type", "application/json")
	getTranslationModels(nil)(c)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "upstream returned status 503")
}
func TestDeepLUsageRejectsMalformedBaseURL(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/translation/deepl/usage", strings.NewReader(`{"mode":"free","base_url":"file:///tmp/usage","api_key":"test-key"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	getDeepLUsage(nil)(c)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "base_url must be a valid http(s) URL")
}
func TestFlareSolverrReportsConstructionAndDisabledFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     models.FlareSolverrConfig
		message string
	}{
		{"missing endpoint", models.FlareSolverrConfig{Enabled: true}, "failed to create flaresolverr client"},
		{"disabled client", models.FlareSolverrConfig{Enabled: false}, "not enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := TestFlareSolverr("https://example.invalid", tc.cfg, &models.ProxyProfile{})
			require.False(t, result.Success)
			require.Zero(t, result.StatusCode)
			require.Contains(t, result.Message, tc.message)
			require.Empty(t, result.VerificationToken)
		})
	}
}
