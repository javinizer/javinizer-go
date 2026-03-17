package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAvailableScrapers_AdditionalOptionSets(t *testing.T) {
	tests := []struct {
		name        string
		scraperName string
		wantLabel   string
		wantKeys    []string
	}{
		{
			name:        "mgstage options",
			scraperName: "mgstage",
			wantLabel:   "MGStage",
			wantKeys: []string{
				"request_delay",
				"use_fake_user_agent",
				"fake_user_agent",
				"proxy.enabled",
				"proxy.profile",
				"download_proxy.enabled",
				"download_proxy.profile",
			},
		},
		{
			name:        "javlibrary options",
			scraperName: "javlibrary",
			wantLabel:   "JavLibrary",
			wantKeys: []string{
				"language",
				"request_delay",
				"base_url",
				"use_flaresolverr",
				"use_fake_user_agent",
				"fake_user_agent",
				"proxy.enabled",
				"proxy.profile",
				"download_proxy.enabled",
				"download_proxy.profile",
			},
		},
		{
			name:        "javbus options",
			scraperName: "javbus",
			wantLabel:   "JavBus",
			wantKeys: []string{
				"language",
				"request_delay",
				"base_url",
				"use_fake_user_agent",
				"fake_user_agent",
				"proxy.enabled",
				"proxy.profile",
				"download_proxy.enabled",
				"download_proxy.profile",
			},
		},
		{
			name:        "jav321 options",
			scraperName: "jav321",
			wantLabel:   "Jav321",
			wantKeys: []string{
				"request_delay",
				"base_url",
				"use_fake_user_agent",
				"fake_user_agent",
				"proxy.enabled",
				"proxy.profile",
				"download_proxy.enabled",
				"download_proxy.profile",
			},
		},
		{
			name:        "tokyohot options",
			scraperName: "tokyohot",
			wantLabel:   "Tokyo-Hot",
			wantKeys: []string{
				"language",
				"request_delay",
				"base_url",
				"use_fake_user_agent",
				"fake_user_agent",
				"proxy.enabled",
				"proxy.profile",
				"download_proxy.enabled",
				"download_proxy.profile",
			},
		},
		{
			name:        "aventertainment options",
			scraperName: "aventertainment",
			wantLabel:   "AV Entertainment",
			wantKeys: []string{
				"language",
				"request_delay",
				"base_url",
				"scrape_bonus_screens",
				"use_fake_user_agent",
				"fake_user_agent",
				"proxy.enabled",
				"proxy.profile",
				"download_proxy.enabled",
				"download_proxy.profile",
			},
		},
		{
			name:        "dlgetchu options",
			scraperName: "dlgetchu",
			wantLabel:   "DLGetchu",
			wantKeys: []string{
				"request_delay",
				"base_url",
				"use_fake_user_agent",
				"fake_user_agent",
				"proxy.enabled",
				"proxy.profile",
				"download_proxy.enabled",
				"download_proxy.profile",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := models.NewScraperRegistry()
			registry.Register(&mockScraper{name: tt.scraperName, enabled: true})

			cfg := config.DefaultConfig()
			cfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
				"alpha": {URL: "http://alpha.example:8080"},
				"beta":  {URL: "http://beta.example:8080"},
			}

			deps := &ServerDependencies{Registry: registry}
			deps.SetConfig(cfg)

			router := gin.New()
			router.GET("/scrapers", getAvailableScrapers(deps))

			req := httptest.NewRequest(http.MethodGet, "/scrapers", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)

			var response AvailableScrapersResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Len(t, response.Scrapers, 1)

			scraper := response.Scrapers[0]
			assert.Equal(t, tt.scraperName, scraper.Name)
			assert.Equal(t, tt.wantLabel, scraper.DisplayName)

			keys := make(map[string]ScraperOption, len(scraper.Options))
			for _, option := range scraper.Options {
				keys[option.Key] = option
			}
			for _, key := range tt.wantKeys {
				_, ok := keys[key]
				assert.Truef(t, ok, "missing option %q", key)
			}

			proxyProfile := keys["proxy.profile"]
			require.Len(t, proxyProfile.Choices, 3)
			assert.Equal(t, []ScraperChoice{
				{Value: "", Label: "Inherit Default"},
				{Value: "alpha", Label: "alpha"},
				{Value: "beta", Label: "beta"},
			}, proxyProfile.Choices)
		})
	}
}

func TestProxyProfileChoices(t *testing.T) {
	assert.Equal(t, []ScraperChoice{
		{Value: "", Label: "Inherit Default"},
	}, proxyProfileChoices(nil))

	cfg := config.DefaultConfig()
	cfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
		"zeta":  {URL: "http://zeta.example:8080"},
		"alpha": {URL: "http://alpha.example:8080"},
	}

	assert.Equal(t, []ScraperChoice{
		{Value: "", Label: "Inherit Default"},
		{Value: "alpha", Label: "alpha"},
		{Value: "zeta", Label: "zeta"},
	}, proxyProfileChoices(cfg))
}

func TestValidateTranslationSaveConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr string
	}{
		{
			name: "nil config allowed",
			cfg:  nil,
		},
		{
			name: "disabled translation allowed",
			cfg:  config.DefaultConfig(),
		},
		{
			name: "openai missing key",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "openai"
				return cfg
			}(),
			wantErr: "metadata.translation.openai.api_key is required",
		},
		{
			name: "deepl missing key",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "deepl"
				return cfg
			}(),
			wantErr: "metadata.translation.deepl.api_key is required",
		},
		{
			name: "google paid missing key",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "google"
				cfg.Metadata.Translation.Google.Mode = "paid"
				return cfg
			}(),
			wantErr: "metadata.translation.google.api_key is required",
		},
		{
			name: "google free without key allowed",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "google"
				cfg.Metadata.Translation.Google.Mode = "free"
				return cfg
			}(),
		},
		{
			name: "openai with key allowed",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "openai"
				cfg.Metadata.Translation.OpenAI.APIKey = "test-key"
				return cfg
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTranslationSaveConfig(tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestFetchOpenAICompatibleModels_ErrorPaths(t *testing.T) {
	t.Run("invalid base url", func(t *testing.T) {
		_, err := fetchOpenAICompatibleModels(context.Background(), "http://[::1", "key")
		require.Error(t, err)
	})

	t.Run("upstream status error", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer upstream.Close()

		_, err := fetchOpenAICompatibleModels(context.Background(), upstream.URL, "key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upstream returned status 502")
	})

	t.Run("invalid payload", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":`))
		}))
		defer upstream.Close()

		_, err := fetchOpenAICompatibleModels(context.Background(), upstream.URL, "key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid upstream response payload")
	})

	t.Run("empty models", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"id":"   "}]}`))
		}))
		defer upstream.Close()

		_, err := fetchOpenAICompatibleModels(context.Background(), upstream.URL, "key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no models found")
	})
}

func TestGetTranslationModels_AdditionalErrors(t *testing.T) {
	deps := &ServerDependencies{}
	deps.SetConfig(config.DefaultConfig())

	router := gin.New()
	router.POST("/translation/models", getTranslationModels(deps))

	tests := []struct {
		name         string
		body         string
		expectedCode int
		expectedBody string
	}{
		{
			name:         "invalid request body",
			body:         `{"provider":`,
			expectedCode: http.StatusBadRequest,
			expectedBody: "Invalid request format",
		},
		{
			name:         "invalid base url",
			body:         `{"provider":"openai","base_url":"ftp://example.com","api_key":"k"}`,
			expectedCode: http.StatusBadRequest,
			expectedBody: "base_url must be a valid http(s) URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			require.Equal(t, tt.expectedCode, w.Code)
			assert.Contains(t, w.Body.String(), tt.expectedBody)
		})
	}

	t.Run("upstream failure becomes bad gateway", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer upstream.Close()

		body := `{"provider":"openai","base_url":"` + upstream.URL + `","api_key":"k"}`
		req := httptest.NewRequest(http.MethodPost, "/translation/models", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadGateway, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to fetch models")
	})
}

func TestTestProxy_AdditionalBranches(t *testing.T) {
	t.Run("invalid request body", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(`{"mode":`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid proxy test request")
	})

	t.Run("invalid target url", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(`{"mode":"direct","target_url":"ftp://example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "target_url must be a valid http(s) URL")
	})

	t.Run("direct proxy requires configuration", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(`{"mode":"direct","target_url":"https://example.com","proxy":{"enabled":false}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "proxy.enabled=true and proxy.url are required")
	})

	t.Run("direct proxy propagates non-success status", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "blocked", http.StatusBadGateway)
		}))
		defer target.Close()

		proxy := startTestForwardProxy(t)
		defer proxy.Close()

		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		body, err := json.Marshal(ProxyTestRequest{
			Mode:      "direct",
			TargetURL: target.URL,
			Proxy: config.ProxyConfig{
				Enabled: true,
				URL:     proxy.URL,
			},
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response ProxyTestResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.Equal(t, http.StatusBadGateway, response.StatusCode)
		assert.Contains(t, response.Message, "returned status 502")
	})

	t.Run("flaresolverr requires configuration", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(`{"mode":"flaresolverr","target_url":"https://example.com","proxy":{"flaresolverr":{"enabled":false}}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "proxy.flaresolverr.enabled=true and proxy.flaresolverr.url are required")
	})

	t.Run("flaresolverr client creation failure", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		reqBody := `{"mode":"flaresolverr","target_url":"https://example.com","proxy":{"flaresolverr":{"enabled":true,"url":"","timeout":30}}}`
		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "proxy.flaresolverr.enabled=true and proxy.flaresolverr.url are required")
	})
}

func TestUpdateConfig_SaveAndTranslationFailures(t *testing.T) {
	t.Run("translation save validation failure", func(t *testing.T) {
		tempConfigFile := filepath.Join(t.TempDir(), "config.yaml")
		deps := createTestDeps(t, config.DefaultConfig(), tempConfigFile)

		router := gin.New()
		router.PUT("/config", updateConfig(deps))

		cfg := config.DefaultConfig()
		cfg.Metadata.Translation.Enabled = true
		cfg.Metadata.Translation.Provider = "openai"
		cfg.Metadata.Translation.OpenAI.APIKey = ""

		body, err := json.Marshal(cfg)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "metadata.translation.openai.api_key is required")
	})

	t.Run("save failure returns internal error", func(t *testing.T) {
		tempDir := t.TempDir()
		deps := createTestDeps(t, config.DefaultConfig(), tempDir)

		router := gin.New()
		router.PUT("/config", updateConfig(deps))

		cfg := config.DefaultConfig()
		cfg.Server.Host = "0.0.0.0"

		body, err := json.Marshal(cfg)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to save configuration")
	})
}

func TestUpdateConfig_PersistsSuccessfulReload(t *testing.T) {
	tempConfigFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, config.DefaultConfig(), tempConfigFile)

	router := gin.New()
	router.PUT("/config", updateConfig(deps))

	cfg := config.DefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 9191
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "google"
	cfg.Metadata.Translation.Google.Mode = "free"

	body, err := json.Marshal(cfg)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	updated := deps.GetConfig()
	assert.Equal(t, "127.0.0.1", updated.Server.Host)
	assert.Equal(t, 9191, updated.Server.Port)

	savedBytes, err := os.ReadFile(tempConfigFile)
	require.NoError(t, err)
	assert.Contains(t, string(savedBytes), "127.0.0.1")
}
