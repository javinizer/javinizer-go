package system

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import scrapers to trigger init() registration of options
	_ "github.com/javinizer/javinizer-go/internal/scraper/aventertainment"
	_ "github.com/javinizer/javinizer-go/internal/scraper/caribbeancom"
	_ "github.com/javinizer/javinizer-go/internal/scraper/dlgetchu"
	_ "github.com/javinizer/javinizer-go/internal/scraper/dmm"
	_ "github.com/javinizer/javinizer-go/internal/scraper/fc2"
	_ "github.com/javinizer/javinizer-go/internal/scraper/jav321"
	_ "github.com/javinizer/javinizer-go/internal/scraper/javbus"
	_ "github.com/javinizer/javinizer-go/internal/scraper/javdb"
	_ "github.com/javinizer/javinizer-go/internal/scraper/javlibrary"
	_ "github.com/javinizer/javinizer-go/internal/scraper/libredmm"
	_ "github.com/javinizer/javinizer-go/internal/scraper/mgstage"
	_ "github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	_ "github.com/javinizer/javinizer-go/internal/scraper/tokyohot"
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
				"user_agent",
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
				"user_agent",
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
				"user_agent",
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
				"user_agent",
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
				"user_agent",
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
				"user_agent",
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
				"user_agent",
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
			assert.Equal(t, tt.wantLabel, scraper.DisplayTitle)

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
	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

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
		assert.Contains(t, w.Body.String(), "proxy.enabled=true and proxy profile with url are required for direct proxy test")
	})

	t.Run("direct proxy propagates non-success status", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "blocked", http.StatusBadGateway)
		}))
		defer target.Close()

		proxy := startTestForwardProxy(t)
		defer proxy.Close()

		cfg := config.DefaultConfig()
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "main"
		cfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
			"main": {URL: proxy.URL},
		}

		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		body, err := json.Marshal(ProxyTestRequest{
			Mode:      "direct",
			TargetURL: target.URL,
			Proxy: config.ProxyConfig{
				Enabled: true,
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

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(`{"mode":"flaresolverr","target_url":"https://example.com","flaresolverr":{"enabled":false}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "flaresolverr.enabled=true and flaresolverr.url are required")
	})

	t.Run("flaresolverr client creation failure", func(t *testing.T) {
		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		reqBody := `{"mode":"flaresolverr","target_url":"https://example.com","flaresolverr":{"enabled":true,"url":"","timeout":30}}`
		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "flaresolverr.enabled=true and flaresolverr.url are required")
	})

	t.Run("direct proxy client creation failure returns structured response", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "main"
		cfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
			"main": {URL: "http://[::1"}, // Invalid URL to cause client creation failure
		}

		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		reqBody := ProxyTestRequest{
			Mode:      "direct",
			TargetURL: "https://example.com",
			Proxy: config.ProxyConfig{
				Enabled: true,
			},
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response ProxyTestResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.Contains(t, response.Message, "failed to create proxy client")
		assert.GreaterOrEqual(t, response.DurationMS, int64(0))
	})

	t.Run("direct proxy request failure returns structured response", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "main"
		cfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
			"main": {URL: "http://127.0.0.1:1"}, // Invalid port to force connection error
		}

		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		reqBody := ProxyTestRequest{
			Mode:      "direct",
			TargetURL: "https://example.com",
			Proxy: config.ProxyConfig{
				Enabled: true,
			},
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response ProxyTestResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.Contains(t, response.Message, "direct proxy request failed")
		assert.GreaterOrEqual(t, response.DurationMS, int64(0))
	})

	t.Run("direct proxy method not allowed adds endpoint guidance", func(t *testing.T) {
		nonProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}))
		defer nonProxy.Close()

		cfg := config.DefaultConfig()
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "main"
		cfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
			"main": {URL: nonProxy.URL},
		}

		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		reqBody := ProxyTestRequest{
			Mode:      "direct",
			TargetURL: "https://example.com",
			Proxy: config.ProxyConfig{
				Enabled: true,
			},
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response ProxyTestResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.Contains(t, response.Message, "direct proxy request failed")
		assert.Contains(t, response.Message, "not a forward proxy")
	})

	t.Run("flaresolverr request failure returns structured response", func(t *testing.T) {
		fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"error","message":"blocked"}`))
		}))
		defer fs.Close()

		deps := &ServerDependencies{}
		deps.SetConfig(config.DefaultConfig())

		router := gin.New()
		router.POST("/proxy/test", testProxy(deps))

		reqBody := `{"mode":"flaresolverr","target_url":"https://example.com","flaresolverr":{"enabled":true,"url":"` + fs.URL + `","timeout":5}}`
		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response ProxyTestResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.Contains(t, response.Message, "flaresolverr request failed")
		assert.GreaterOrEqual(t, response.DurationMS, int64(0))
	})
}

func TestIsValidHTTPURL(t *testing.T) {
	assert.True(t, isValidHTTPURL("https://example.com"))
	assert.True(t, isValidHTTPURL("http://localhost:8080/path"))
	assert.False(t, isValidHTTPURL("ftp://example.com"))
	assert.False(t, isValidHTTPURL("https:///missing-host"))
	assert.False(t, isValidHTTPURL("://bad-url"))
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

// Proxy verification token tests
func TestUpdateConfig_ProxyVerification(t *testing.T) {
	t.Run("save without token fails when proxy changed", func(t *testing.T) {
		tempConfigFile := filepath.Join(t.TempDir(), "config.yaml")
		deps := createTestDeps(t, config.DefaultConfig(), tempConfigFile)
		// Initialize token store for this test
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(deps))

		// Change proxy settings without providing a token
		cfg := *config.DefaultConfig()
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "test"
		cfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
			"test": {URL: "http://proxy.example:8080"},
		}

		body, err := json.Marshal(UpdateConfigRequest{Config: cfg})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "proxy settings changed but no test verification token provided")
	})

	t.Run("save with valid token succeeds when proxy changed", func(t *testing.T) {
		tempConfigFile := filepath.Join(t.TempDir(), "config.yaml")
		deps := createTestDeps(t, config.DefaultConfig(), tempConfigFile)
		// Initialize token store
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(deps))

		// Create new proxy config
		newProxy := config.ProxyConfig{
			Enabled:        true,
			DefaultProfile: "test",
			Profiles: map[string]config.ProxyProfile{
				"test": {URL: "http://proxy.example:8080"},
			},
		}

		// Create a valid token for the new proxy config
		vt := deps.TokenStore.Create("global", core.HashProxyConfig(newProxy))

		// Build the full config with the new proxy settings
		cfg := *config.DefaultConfig()
		cfg.Scrapers.Proxy = newProxy

		reqBody := UpdateConfigRequest{
			Config: cfg,
			ProxyVerificationTokens: map[string]string{
				"global": vt.Token,
			},
		}

		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration saved and reloaded successfully")
	})

	t.Run("save with invalid token fails", func(t *testing.T) {
		tempConfigFile := filepath.Join(t.TempDir(), "config.yaml")
		deps := createTestDeps(t, config.DefaultConfig(), tempConfigFile)
		// Initialize token store
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(deps))

		// Change proxy settings with an invalid token
		cfg := *config.DefaultConfig()
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "test"
		cfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
			"test": {URL: "http://proxy.example:8080"},
		}

		reqBody := UpdateConfigRequest{
			Config: cfg,
			ProxyVerificationTokens: map[string]string{
				"global": "invalid_token",
			},
		}

		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "proxy verification token is invalid or expired")
	})

	t.Run("save without token succeeds when proxy unchanged", func(t *testing.T) {
		tempConfigFile := filepath.Join(t.TempDir(), "config.yaml")
		// Start with proxy already configured
		initialCfg := config.DefaultConfig()
		initialCfg.Scrapers.Proxy.Enabled = true
		initialCfg.Scrapers.Proxy.DefaultProfile = "test"
		initialCfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
			"test": {URL: "http://proxy.example:8080"},
		}

		deps := createTestDeps(t, initialCfg, tempConfigFile)
		deps.SetConfig(initialCfg)
		// Initialize token store
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(deps))

		// Change only server settings, keep proxy the same
		cfg := *initialCfg
		cfg.Server.Host = "192.168.1.1"

		body, err := json.Marshal(UpdateConfigRequest{Config: cfg})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration saved and reloaded successfully")
	})

	t.Run("save with expired token fails", func(t *testing.T) {
		tempConfigFile := filepath.Join(t.TempDir(), "config.yaml")
		deps := createTestDeps(t, config.DefaultConfig(), tempConfigFile)
		// Initialize token store
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(deps))

		// Create new proxy config
		newProxy := config.ProxyConfig{
			Enabled:        true,
			DefaultProfile: "test",
			Profiles: map[string]config.ProxyProfile{
				"test": {URL: "http://proxy.example:8080"},
			},
		}

		// Create a token with wrong config hash (simulates token for different config)
		wrongHashToken := deps.TokenStore.Create("global", "wrong_hash")

		// Build the full config with the new proxy settings
		cfg := *config.DefaultConfig()
		cfg.Scrapers.Proxy = newProxy

		reqBody := UpdateConfigRequest{
			Config: cfg,
			ProxyVerificationTokens: map[string]string{
				"global": wrongHashToken.Token, // Token exists but for different config hash
			},
		}

		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "proxy verification token is invalid or expired")
	})
}
