package system

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockScraper implements the Scraper interface for testing
type mockScraper struct {
	name    string
	enabled bool
}

func (m *mockScraper) Name() string {
	return m.name
}

func (m *mockScraper) Search(id string) (*models.ScraperResult, error) {
	return nil, nil
}

func (m *mockScraper) GetURL(id string) (string, error) {
	return "", nil
}

func (m *mockScraper) IsEnabled() bool {
	return m.enabled
}

func (m *mockScraper) Close() error { return nil }

func (m *mockScraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{Enabled: m.enabled}
}

func TestHealthCheck(t *testing.T) {
	tests := []struct {
		name             string
		scrapers         []models.Scraper
		expectedStatus   int
		expectedScrapers []string
	}{
		{
			name: "health check with enabled scrapers",
			scrapers: []models.Scraper{
				&mockScraper{name: "r18dev", enabled: true},
				&mockScraper{name: "dmm", enabled: true},
			},
			expectedStatus:   200,
			expectedScrapers: []string{"r18dev", "dmm"},
		},
		{
			name: "health check with one scraper",
			scrapers: []models.Scraper{
				&mockScraper{name: "r18dev", enabled: true},
			},
			expectedStatus:   200,
			expectedScrapers: []string{"r18dev"},
		},
		{
			name: "health check with no enabled scrapers",
			scrapers: []models.Scraper{
				&mockScraper{name: "r18dev", enabled: false},
			},
			expectedStatus:   200,
			expectedScrapers: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := models.NewScraperRegistry()
			for _, scraper := range tt.scrapers {
				registry.Register(scraper)
			}

			// Create minimal ServerDependencies for test
			deps := &ServerDependencies{
				Registry: registry,
			}
			deps.SetConfig(config.DefaultConfig())

			router := gin.New()
			router.GET("/health", healthCheck(deps))

			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response HealthResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "ok", response.Status)
			assert.ElementsMatch(t, tt.expectedScrapers, response.Scrapers)
			assert.Equal(t, version.Short(), response.Version)
			assert.Equal(t, version.Commit, response.Commit)
			assert.Equal(t, version.BuildDate, response.BuildDate)
		})
	}
}

func TestGetConfig(t *testing.T) {
	tests := []struct {
		name           string
		config         *config.Config
		expectedStatus int
	}{
		{
			name: "get config successfully",
			config: &config.Config{
				Server: config.ServerConfig{
					Host: "localhost",
					Port: 8080,
				},
			},
			expectedStatus: 200,
		},
		{
			name: "get empty config",
			config: &config.Config{
				Server: config.ServerConfig{
					Host: "",
					Port: 0,
				},
			},
			expectedStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create minimal ServerDependencies for test
			deps := &ServerDependencies{}
			deps.SetConfig(tt.config)

			router := gin.New()
			router.GET("/config", getConfig(deps))

			req := httptest.NewRequest("GET", "/config", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response config.Config
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, tt.config.Server.Host, response.Server.Host)
			assert.Equal(t, tt.config.Server.Port, response.Server.Port)
		})
	}
}

func TestGetAvailableScrapers(t *testing.T) {
	tests := []struct {
		name           string
		scrapers       []models.Scraper
		expectedStatus int
		validateFn     func(*testing.T, AvailableScrapersResponse)
	}{
		{
			name: "r18dev scraper",
			scrapers: []models.Scraper{
				&mockScraper{name: "r18dev", enabled: true},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp AvailableScrapersResponse) {
				require.Len(t, resp.Scrapers, 1)
				assert.Equal(t, "r18dev", resp.Scrapers[0].Name)
				assert.Equal(t, "R18.dev", resp.Scrapers[0].DisplayName)
				assert.True(t, resp.Scrapers[0].Enabled)
				assert.Len(t, resp.Scrapers[0].Options, 7)
				optionKeys := make(map[string]bool)
				for _, opt := range resp.Scrapers[0].Options {
					optionKeys[opt.Key] = true
				}
				assert.True(t, optionKeys["language"])
				assert.True(t, optionKeys["use_fake_user_agent"])
				assert.True(t, optionKeys["fake_user_agent"])
				assert.True(t, optionKeys["proxy.enabled"])
				assert.True(t, optionKeys["proxy.profile"])
				assert.True(t, optionKeys["download_proxy.enabled"])
				assert.True(t, optionKeys["download_proxy.profile"])
			},
		},
		{
			name: "dmm scraper with options",
			scrapers: []models.Scraper{
				&mockScraper{name: "dmm", enabled: true},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp AvailableScrapersResponse) {
				require.Len(t, resp.Scrapers, 1)
				assert.Equal(t, "dmm", resp.Scrapers[0].Name)
				assert.Equal(t, "DMM/Fanza", resp.Scrapers[0].DisplayName)
				assert.True(t, resp.Scrapers[0].Enabled)
				assert.Len(t, resp.Scrapers[0].Options, 9)

				// Verify options exist
				optionKeys := make(map[string]bool)
				for _, opt := range resp.Scrapers[0].Options {
					optionKeys[opt.Key] = true
				}
				assert.True(t, optionKeys["scrape_actress"])
				assert.True(t, optionKeys["enable_browser"])
				assert.True(t, optionKeys["browser_timeout"])
				assert.True(t, optionKeys["use_fake_user_agent"])
				assert.True(t, optionKeys["fake_user_agent"])
				assert.True(t, optionKeys["proxy.enabled"])
				assert.True(t, optionKeys["proxy.profile"])
				assert.True(t, optionKeys["download_proxy.enabled"])
				assert.True(t, optionKeys["download_proxy.profile"])
			},
		},
		{
			name: "multiple scrapers",
			scrapers: []models.Scraper{
				&mockScraper{name: "r18dev", enabled: true},
				&mockScraper{name: "dmm", enabled: false},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp AvailableScrapersResponse) {
				require.Len(t, resp.Scrapers, 2)
			},
		},
		{
			name: "javdb scraper with options",
			scrapers: []models.Scraper{
				&mockScraper{name: "javdb", enabled: true},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp AvailableScrapersResponse) {
				require.Len(t, resp.Scrapers, 1)
				assert.Equal(t, "javdb", resp.Scrapers[0].Name)
				assert.Equal(t, "JavDB", resp.Scrapers[0].DisplayName)
				assert.True(t, resp.Scrapers[0].Enabled)
				assert.Len(t, resp.Scrapers[0].Options, 9)

				optionKeys := make(map[string]bool)
				for _, opt := range resp.Scrapers[0].Options {
					optionKeys[opt.Key] = true
				}
				assert.True(t, optionKeys["request_delay"])
				assert.True(t, optionKeys["base_url"])
				assert.True(t, optionKeys["use_flaresolverr"])
				assert.True(t, optionKeys["use_fake_user_agent"])
				assert.True(t, optionKeys["fake_user_agent"])
				assert.True(t, optionKeys["proxy.enabled"])
				assert.True(t, optionKeys["proxy.profile"])
				assert.True(t, optionKeys["download_proxy.enabled"])
				assert.True(t, optionKeys["download_proxy.profile"])
			},
		},
		{
			name: "libredmm scraper with options",
			scrapers: []models.Scraper{
				&mockScraper{name: "libredmm", enabled: true},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp AvailableScrapersResponse) {
				require.Len(t, resp.Scrapers, 1)
				assert.Equal(t, "libredmm", resp.Scrapers[0].Name)
				assert.Equal(t, "LibreDMM", resp.Scrapers[0].DisplayName)
				assert.True(t, resp.Scrapers[0].Enabled)
				assert.Len(t, resp.Scrapers[0].Options, 8)

				optionKeys := make(map[string]bool)
				for _, opt := range resp.Scrapers[0].Options {
					optionKeys[opt.Key] = true
				}
				assert.True(t, optionKeys["request_delay"])
				assert.True(t, optionKeys["base_url"])
				assert.True(t, optionKeys["use_fake_user_agent"])
				assert.True(t, optionKeys["fake_user_agent"])
				assert.True(t, optionKeys["proxy.enabled"])
				assert.True(t, optionKeys["proxy.profile"])
				assert.True(t, optionKeys["download_proxy.enabled"])
				assert.True(t, optionKeys["download_proxy.profile"])
			},
		},
		{
			name: "caribbeancom scraper with options",
			scrapers: []models.Scraper{
				&mockScraper{name: "caribbeancom", enabled: true},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp AvailableScrapersResponse) {
				require.Len(t, resp.Scrapers, 1)
				assert.Equal(t, "caribbeancom", resp.Scrapers[0].Name)
				assert.Equal(t, "Caribbeancom", resp.Scrapers[0].DisplayName)
				assert.True(t, resp.Scrapers[0].Enabled)
				assert.Len(t, resp.Scrapers[0].Options, 9)

				optionKeys := make(map[string]bool)
				for _, opt := range resp.Scrapers[0].Options {
					optionKeys[opt.Key] = true
				}
				assert.True(t, optionKeys["language"])
				assert.True(t, optionKeys["request_delay"])
				assert.True(t, optionKeys["base_url"])
				assert.True(t, optionKeys["use_fake_user_agent"])
				assert.True(t, optionKeys["fake_user_agent"])
				assert.True(t, optionKeys["proxy.enabled"])
				assert.True(t, optionKeys["proxy.profile"])
				assert.True(t, optionKeys["download_proxy.enabled"])
				assert.True(t, optionKeys["download_proxy.profile"])
			},
		},
		{
			name: "fc2 scraper with options",
			scrapers: []models.Scraper{
				&mockScraper{name: "fc2", enabled: true},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp AvailableScrapersResponse) {
				require.Len(t, resp.Scrapers, 1)
				assert.Equal(t, "fc2", resp.Scrapers[0].Name)
				assert.Equal(t, "FC2", resp.Scrapers[0].DisplayName)
				assert.True(t, resp.Scrapers[0].Enabled)
				assert.Len(t, resp.Scrapers[0].Options, 8)

				optionKeys := make(map[string]bool)
				for _, opt := range resp.Scrapers[0].Options {
					optionKeys[opt.Key] = true
				}
				assert.True(t, optionKeys["request_delay"])
				assert.True(t, optionKeys["base_url"])
				assert.True(t, optionKeys["use_fake_user_agent"])
				assert.True(t, optionKeys["fake_user_agent"])
				assert.True(t, optionKeys["proxy.enabled"])
				assert.True(t, optionKeys["proxy.profile"])
				assert.True(t, optionKeys["download_proxy.enabled"])
				assert.True(t, optionKeys["download_proxy.profile"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := models.NewScraperRegistry()
			for _, scraper := range tt.scrapers {
				registry.Register(scraper)
			}

			// Create minimal ServerDependencies for test
			deps := &ServerDependencies{
				Registry: registry,
			}
			deps.SetConfig(config.DefaultConfig())

			router := gin.New()
			router.GET("/scrapers", getAvailableScrapers(deps))

			req := httptest.NewRequest("GET", "/scrapers", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response AvailableScrapersResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			if tt.validateFn != nil {
				tt.validateFn(t, response)
			}
		})
	}
}

func TestGetAvailableScrapers_RespectsConfigPriorityOrder(t *testing.T) {
	registry := models.NewScraperRegistry()
	registry.Register(&mockScraper{name: "mgstage", enabled: true})
	registry.Register(&mockScraper{name: "javdb", enabled: true})
	registry.Register(&mockScraper{name: "dmm", enabled: true})

	cfg := config.DefaultConfig()
	cfg.Scrapers.Priority = []string{"javdb", "dmm"}

	deps := &ServerDependencies{
		Registry: registry,
	}
	deps.SetConfig(cfg)

	router := gin.New()
	router.GET("/scrapers", getAvailableScrapers(deps))

	req := httptest.NewRequest("GET", "/scrapers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response AvailableScrapersResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Scrapers, 3)

	assert.Equal(t, "javdb", response.Scrapers[0].Name)
	assert.Equal(t, "dmm", response.Scrapers[1].Name)
	assert.Equal(t, "mgstage", response.Scrapers[2].Name)
}

func startTestForwardProxy(t *testing.T) *httptest.Server {
	t.Helper()

	client := &http.Client{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetURL := r.RequestURI
		if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
			targetURL = r.URL.String()
		}

		req, err := http.NewRequest(r.Method, targetURL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Header = r.Header.Clone()

		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
}

func TestTestProxy(t *testing.T) {
	t.Run("direct proxy success", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer target.Close()

		proxy := startTestForwardProxy(t)
		defer proxy.Close()

		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		reqBody := ProxyTestRequest{
			Mode:      "direct",
			TargetURL: target.URL,
			Proxy: config.ProxyConfig{
				Enabled: true,
				URL:     proxy.URL,
			},
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ProxyTestResponse
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, "direct", resp.Mode)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Message, "succeeded")
	})

	t.Run("flaresolverr success", func(t *testing.T) {
		fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","solution":{"response":"<html>ok</html>","cookies":[{"name":"cf_clearance","value":"abc"}],"userAgent":"ua"}}`))
		}))
		defer fs.Close()

		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		reqBody := ProxyTestRequest{
			Mode:      "flaresolverr",
			TargetURL: "https://javdb.com",
			Proxy: config.ProxyConfig{
				FlareSolverr: config.FlareSolverrConfig{
					Enabled:    true,
					URL:        fs.URL,
					Timeout:    30,
					MaxRetries: 0,
					SessionTTL: 300,
				},
			},
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ProxyTestResponse
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, "flaresolverr", resp.Mode)
		assert.Equal(t, fs.URL, resp.FlareSolverrURL)
	})

	t.Run("invalid mode", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		body := []byte(`{"mode":"invalid","proxy":{"enabled":true,"url":"http://proxy.example.com:8080"}}`)
		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestGetTranslationModels(t *testing.T) {
	t.Run("success openai-compatible models", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/models", r.URL.Path)
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4.1"},{"id":"gpt-4o-mini"}]}`))
		}))
		defer upstream.Close()

		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/translation/models", getTranslationModels(deps))

		reqBody := TranslationModelsRequest{
			Provider: "openai",
			BaseURL:  upstream.URL,
			APIKey:   "test-key",
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp TranslationModelsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, []string{"gpt-4.1", "gpt-4o-mini"}, resp.Models)
	})

	t.Run("invalid provider", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/translation/models", getTranslationModels(deps))

		body := []byte(`{"provider":"deepl","base_url":"https://example.com","api_key":"k"}`)
		req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing api key", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/translation/models", getTranslationModels(deps))

		body := []byte(`{"provider":"openai","base_url":"https://api.openai.com/v1"}`)
		req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestUpdateConfig(t *testing.T) {
	tests := []struct {
		name           string
		initialConfig  *config.Config
		requestBody    interface{}
		expectedStatus int
		expectedError  string
	}{
		{
			name:          "valid config update",
			initialConfig: config.DefaultConfig(),
			requestBody: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Server.Host = "0.0.0.0"
				cfg.Server.Port = 9090
				return cfg
			}(),
			expectedStatus: 200,
		},
		{
			name:           "invalid json",
			initialConfig:  config.DefaultConfig(),
			requestBody:    "invalid json",
			expectedStatus: 400,
			expectedError:  "Invalid configuration format",
		},
		{
			name:          "reject newer config version",
			initialConfig: config.DefaultConfig(),
			requestBody: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.ConfigVersion = config.CurrentConfigVersion + 1
				return cfg
			}(),
			expectedStatus: 400,
			expectedError:  "newer than supported version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary config file for testing
			tempConfigFile := t.TempDir() + "/config.yaml"

			// Create minimal dependencies for testing
			deps := createTestDeps(t, tt.initialConfig, tempConfigFile)

			router := gin.New()
			router.PUT("/config", updateConfig(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("PUT", "/config", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedError != "" {
				var response ErrorResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response.Error, tt.expectedError)
			}
		})
	}
}

func TestUpdateConfig_ConcurrentAccess(t *testing.T) {
	// Test that concurrent config updates are properly serialized
	cfg := config.DefaultConfig()

	tempConfigFile := t.TempDir() + "/config.yaml"

	// Create minimal dependencies for testing
	deps := createTestDeps(t, cfg, tempConfigFile)

	router := gin.New()
	router.PUT("/config", updateConfig(deps))

	// Launch multiple concurrent requests
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(port int) {
			defer func() { done <- true }()

			newConfig := config.DefaultConfig()
			newConfig.Server.Host = "0.0.0.0"
			newConfig.Server.Port = 8080 + port

			body, err := json.Marshal(newConfig)
			require.NoError(t, err)

			req := httptest.NewRequest("PUT", "/config", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// All requests should succeed or return valid errors
			assert.True(t, w.Code == 200 || w.Code == 400 || w.Code == 500)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestGetAvailableScrapers_OptionsValidation(t *testing.T) {
	// Specifically test DMM options structure
	registry := models.NewScraperRegistry()
	registry.Register(&mockScraper{name: "dmm", enabled: true})

	// Create minimal ServerDependencies for test
	deps := &ServerDependencies{
		Registry: registry,
	}
	deps.SetConfig(config.DefaultConfig())

	router := gin.New()
	router.GET("/scrapers", getAvailableScrapers(deps))

	req := httptest.NewRequest("GET", "/scrapers", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response AvailableScrapersResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	require.Len(t, response.Scrapers, 1)
	scraper := response.Scrapers[0]
	require.Len(t, scraper.Options, 9)

	// Test scrape_actress option
	var scrapeActressOpt *ScraperOption
	for i := range scraper.Options {
		if scraper.Options[i].Key == "scrape_actress" {
			scrapeActressOpt = &scraper.Options[i]
			break
		}
	}
	require.NotNil(t, scrapeActressOpt)
	assert.Equal(t, "boolean", scrapeActressOpt.Type)
	assert.Contains(t, scrapeActressOpt.Description, "actress")

	// Test browser_timeout option
	var timeoutOpt *ScraperOption
	for i := range scraper.Options {
		if scraper.Options[i].Key == "browser_timeout" {
			timeoutOpt = &scraper.Options[i]
			break
		}
	}
	require.NotNil(t, timeoutOpt)
	assert.Equal(t, "number", timeoutOpt.Type)
	assert.NotNil(t, timeoutOpt.Min)
	assert.Equal(t, 5, *timeoutOpt.Min)
	assert.NotNil(t, timeoutOpt.Max)
	assert.Equal(t, 120, *timeoutOpt.Max)
	assert.Equal(t, "seconds", timeoutOpt.Unit)
}

// TestHealthCheck_WithDisabledScrapers tests health check with disabled scrapers
func TestHealthCheck_WithDisabledScrapers(t *testing.T) {
	registry := models.NewScraperRegistry()
	registry.Register(&mockScraper{name: "r18dev", enabled: false})
	registry.Register(&mockScraper{name: "dmm", enabled: false})

	deps := &ServerDependencies{
		Registry: registry,
	}
	deps.SetConfig(config.DefaultConfig())

	router := gin.New()
	router.GET("/health", healthCheck(deps))

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response HealthResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "ok", response.Status)
	assert.Empty(t, response.Scrapers, "Disabled scrapers should not appear in health check")
	assert.Equal(t, version.Short(), response.Version)
	assert.Equal(t, version.Commit, response.Commit)
	assert.Equal(t, version.BuildDate, response.BuildDate)
}

// TestGetConfig_EmptyConfig tests getting an empty config
func TestGetConfig_EmptyConfig(t *testing.T) {
	deps := &ServerDependencies{}
	deps.SetConfig(&config.Config{})

	router := gin.New()
	router.GET("/config", getConfig(deps))

	req := httptest.NewRequest("GET", "/config", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response config.Config
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
}

// TestUpdateConfig_InvalidConfig tests config update with invalid data
func TestUpdateConfig_InvalidConfig(t *testing.T) {
	tempConfigFile := t.TempDir() + "/config.yaml"

	initialConfig := config.DefaultConfig()
	deps := createTestDeps(t, initialConfig, tempConfigFile)

	router := gin.New()
	router.PUT("/config", updateConfig(deps))

	tests := []struct {
		name          string
		requestBody   string
		expectedCode  int
		errorContains string
	}{
		{
			name:          "malformed JSON",
			requestBody:   "{invalid-json",
			expectedCode:  400,
			errorContains: "Invalid configuration format",
		},
		{
			name:         "empty JSON object",
			requestBody:  "{}",
			expectedCode: 400, // Missing required settings after strict save validation
		},
		{
			name: "translation enabled but deepl key missing",
			requestBody: func() string {
				cfg := config.DefaultConfig()
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "deepl"
				cfg.Metadata.Translation.DeepL.APIKey = ""

				payload, err := json.Marshal(cfg)
				require.NoError(t, err)
				return string(payload)
			}(),
			expectedCode:  400,
			errorContains: "metadata.translation.deepl.api_key is required when provider=deepl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/config", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedCode, w.Code)
			if tt.errorContains != "" {
				assert.Contains(t, w.Body.String(), tt.errorContains)
			}
		})
	}
}

// TestGetAvailableScrapers_NoScrapers tests when no scrapers are registered
func TestGetAvailableScrapers_NoScrapers(t *testing.T) {
	registry := models.NewScraperRegistry()

	deps := &ServerDependencies{
		Registry: registry,
	}
	deps.SetConfig(config.DefaultConfig())

	router := gin.New()
	router.GET("/scrapers", getAvailableScrapers(deps))

	req := httptest.NewRequest("GET", "/scrapers", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response AvailableScrapersResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Empty(t, response.Scrapers)
}

// TestConfigReloadRaceConditions tests that config reload doesn't cause race conditions
func TestConfigReloadRaceConditions(t *testing.T) {
	tempConfigFile := t.TempDir() + "/config.yaml"

	initialConfig := config.DefaultConfig()
	deps := createTestDeps(t, initialConfig, tempConfigFile)

	router := gin.New()
	router.PUT("/config", updateConfig(deps))
	router.GET("/config", getConfig(deps))

	// Launch concurrent reads and writes
	done := make(chan bool, 20)

	// 10 readers
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()

			req := httptest.NewRequest("GET", "/config", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Should always succeed
			assert.Equal(t, 200, w.Code)
		}()
	}

	// 10 writers
	for i := 0; i < 10; i++ {
		go func(port int) {
			defer func() { done <- true }()

			newConfig := config.DefaultConfig()
			newConfig.Server.Port = 8080 + port

			body, err := json.Marshal(newConfig)
			require.NoError(t, err)

			req := httptest.NewRequest("PUT", "/config", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Should succeed or fail gracefully (not panic)
			assert.True(t, w.Code == 200 || w.Code == 400 || w.Code == 500)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 20; i++ {
		<-done
	}
}

// TestHealthCheck_MultipleCalls tests that health check is idempotent
func TestHealthCheck_MultipleCalls(t *testing.T) {
	registry := models.NewScraperRegistry()
	registry.Register(&mockScraper{name: "r18dev", enabled: true})

	deps := &ServerDependencies{
		Registry: registry,
	}
	deps.SetConfig(config.DefaultConfig())

	router := gin.New()
	router.GET("/health", healthCheck(deps))

	// Call health check multiple times
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)

		var response HealthResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "ok", response.Status)
		assert.Contains(t, response.Scrapers, "r18dev")
		assert.Equal(t, version.Short(), response.Version)
		assert.Equal(t, version.Commit, response.Commit)
		assert.Equal(t, version.BuildDate, response.BuildDate)
	}
}

// TestUpdateConfig_Rollback tests that config rollback works on reload failure
func TestUpdateConfig_Rollback(t *testing.T) {
	t.Skip("Skipping rollback test - requires special conditions to trigger reload failure")
	// Note: This test would require special mocking to trigger a reload failure
	// which is difficult without modifying production code. Rollback logic is
	// tested manually during integration testing.
}
