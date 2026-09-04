package movie

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// registerFailingTranslationServer returns an httptest server that always 429s,
// so the real translation pipeline fails with a typed HTTP-status error.
func registerRateLimitedTranslationServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func translationTestConfig(translationURL string, translationEnabled bool) *config.Config {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}
	cfg.Metadata.Translation = config.TranslationConfig{
		Enabled:        translationEnabled,
		Provider:       "openai",
		SourceLanguage: "ja",
		TargetLanguage: "en",
		Fields:         config.TranslationFieldsConfig{Title: true},
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: translationURL,
			APIKey:  "test-key",
			Model:   "gpt-4o-mini",
		},
	}
	return cfg
}

func registerR18devMock(registry *scraperutil.ScraperRegistry, id, title string) {
	registry.RegisterInstance(&mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result:  &models.ScraperResult{Source: "r18dev", ID: id, Title: title},
	})
}

// TestScrapeMovie_TranslationWarningExposed proves the single-scrape handler
// surfaces translation_warning + translation_warning_code from
// OrchestrationMeta instead of discarding it.
func TestScrapeMovie_TranslationWarningExposed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rlSrv := registerRateLimitedTranslationServer(t)

	deps := createTestDeps(t, translationTestConfig(rlSrv.URL, true), "")
	registerR18devMock(deps.CoreDeps.GetRegistry(), "IPX-900", "テストタイトル")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	router := gin.New()
	router.POST("/scrape", scrapeMovie(movieDeps))

	body, err := json.Marshal(contracts.ScrapeRequest{ID: "IPX-900"})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/scrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp contracts.ScrapeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Movie)
	assert.Equal(t, "rate_limited", resp.TranslationWarningCode)
	assert.Contains(t, resp.TranslationWarning, "rate limited")
}

// TestRescrapeMovie_TranslationWarningExposed proves the rescrape handler
// surfaces both fields on its MovieResponse.
func TestRescrapeMovie_TranslationWarningExposed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rlSrv := registerRateLimitedTranslationServer(t)

	deps := createTestDeps(t, translationTestConfig(rlSrv.URL, true), "")
	registerR18devMock(deps.CoreDeps.GetRegistry(), "IPX-901", "テストタイトル")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	router := gin.New()
	router.POST("/movies/:id/rescrape", rescrapeMovie(movieDeps))

	body, err := json.Marshal(contracts.RescrapeRequest{SelectedScrapers: []string{"r18dev"}, Force: true})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/movies/IPX-901/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp contracts.MovieResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Movie)
	assert.Equal(t, "rate_limited", resp.TranslationWarningCode)
	assert.Contains(t, resp.TranslationWarning, "rate limited")
}

// TestScrapeMovie_NoTranslationWarningOmitsFields proves responses stay
// unchanged when no warning exists (omitempty on both fields).
func TestScrapeMovie_NoTranslationWarningOmitsFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps := createTestDeps(t, translationTestConfig("", false), "")
	registerR18devMock(deps.CoreDeps.GetRegistry(), "IPX-902", "テストタイトル")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	router := gin.New()
	router.POST("/scrape", scrapeMovie(movieDeps))

	body, err := json.Marshal(contracts.ScrapeRequest{ID: "IPX-902"})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/scrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, hasWarning := raw["translation_warning"]
	_, hasCode := raw["translation_warning_code"]
	assert.False(t, hasWarning, "no warning -> translation_warning absent")
	assert.False(t, hasCode, "no warning -> translation_warning_code absent")
}
