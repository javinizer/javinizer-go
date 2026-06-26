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
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import scrapers to trigger init() registration of options
	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
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
				"rate_limit",
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
				"rate_limit",
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
				"rate_limit",
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
				"rate_limit",
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
				"rate_limit",
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
				"rate_limit",
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
				"rate_limit",
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
			registry := newTestRegistry()
			registry.RegisterInstance(&mockScraper{name: tt.scraperName, enabled: true})

			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
				"alpha": {URL: "http://alpha.example:8080"},
				"beta":  {URL: "http://beta.example:8080"},
			}

			deps := newTestDeps(cfg, withRegistry(registry))

			router := gin.New()
			router.GET("/scrapers", getAvailableScrapers(testkit.GetTestRuntime(deps)))

			req := httptest.NewRequest(http.MethodGet, "/scrapers", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)

			var response contracts.AvailableScrapersResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Len(t, response.Scrapers, 1)

			scraper := response.Scrapers[0]
			assert.Equal(t, tt.scraperName, scraper.Name)
			assert.Equal(t, tt.wantLabel, scraper.DisplayTitle)

			keys := make(map[string]contracts.ScraperOption, len(scraper.Options))
			for _, option := range scraper.Options {
				keys[option.Key] = option
			}
			for _, key := range tt.wantKeys {
				_, ok := keys[key]
				assert.Truef(t, ok, "missing option %q", key)
			}

			proxyProfile := keys["proxy.profile"]
			require.Len(t, proxyProfile.Choices, 3)
			assert.Equal(t, []contracts.ScraperChoice{
				{Value: "", Label: "Inherit Default"},
				{Value: "alpha", Label: "alpha"},
				{Value: "beta", Label: "beta"},
			}, proxyProfile.Choices)
		})
	}
}

func TestProxyProfileChoices(t *testing.T) {
	assert.Equal(t, []contracts.ScraperChoice{
		{Value: "", Label: "Inherit Default"},
	}, proxyProfileChoices(core.APIConfig{}))

	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"zeta":  {URL: "http://zeta.example:8080"},
		"alpha": {URL: "http://alpha.example:8080"},
	}

	assert.Equal(t, []contracts.ScraperChoice{
		{Value: "", Label: "Inherit Default"},
		{Value: "alpha", Label: "alpha"},
		{Value: "zeta", Label: "zeta"},
	}, proxyProfileChoices(core.ConfigFromAppConfig(cfg)))
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
			cfg:  config.DefaultConfig(nil, nil),
		},
		{
			name: "openai missing key",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig(nil, nil)
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "openai"
				return cfg
			}(),
			wantErr: "metadata.translation.openai.api_key is required",
		},
		{
			name: "deepl missing key",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig(nil, nil)
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "deepl"
				return cfg
			}(),
			wantErr: "metadata.translation.deepl.api_key is required",
		},
		{
			name: "google paid missing key",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig(nil, nil)
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
				cfg := config.DefaultConfig(nil, nil)
				cfg.Metadata.Translation.Enabled = true
				cfg.Metadata.Translation.Provider = "google"
				cfg.Metadata.Translation.Google.Mode = "free"
				return cfg
			}(),
		},
		{
			name: "openai with key allowed",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig(nil, nil)
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
	testCfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(testCfg)

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
		testCfg := config.DefaultConfig(nil, nil)
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(`{"mode":`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid proxy test request")
	})

	t.Run("invalid target url", func(t *testing.T) {
		testCfg := config.DefaultConfig(nil, nil)
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(`{"mode":"direct","target_url":"ftp://example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "target_url must be a valid http(s) URL")
	})

	t.Run("direct proxy requires configuration", func(t *testing.T) {
		testCfg := config.DefaultConfig(nil, nil)
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

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

		cfg := config.DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "main"
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
			"main": {URL: proxy.URL},
		}

		testCfg := cfg
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		body, err := json.Marshal(contracts.ProxyTestRequest{
			Mode:      "direct",
			TargetURL: target.URL,
			Proxy: models.ProxyConfig{
				Enabled: true,
			},
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response contracts.ProxyTestResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.Equal(t, http.StatusBadGateway, response.StatusCode)
		assert.Contains(t, response.Message, "returned status 502")
	})

	t.Run("flaresolverr requires configuration", func(t *testing.T) {
		testCfg := config.DefaultConfig(nil, nil)
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(`{"mode":"flaresolverr","target_url":"https://example.com","flaresolverr":{"enabled":false}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "flaresolverr.enabled=true and flaresolverr.url are required")
	})

	t.Run("flaresolverr client creation failure", func(t *testing.T) {
		testCfg := config.DefaultConfig(nil, nil)
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		reqBody := `{"mode":"flaresolverr","target_url":"https://example.com","flaresolverr":{"enabled":true,"url":"","timeout":30}}`
		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "flaresolverr.enabled=true and flaresolverr.url are required")
	})

	t.Run("direct proxy client creation failure returns structured response", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "main"
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
			"main": {URL: "http://[::1"}, // Invalid URL to cause client creation failure
		}

		testCfg := cfg
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		reqBody := contracts.ProxyTestRequest{
			Mode:      "direct",
			TargetURL: "https://example.com",
			Proxy: models.ProxyConfig{
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

		var response contracts.ProxyTestResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.Contains(t, response.Message, "failed to create proxy transport")
		assert.GreaterOrEqual(t, response.DurationMS, int64(0))
	})

	t.Run("direct proxy request failure returns structured response", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "main"
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
			"main": {URL: "http://127.0.0.1:1"}, // Invalid port to force connection error
		}

		testCfg := cfg
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		reqBody := contracts.ProxyTestRequest{
			Mode:      "direct",
			TargetURL: "https://example.com",
			Proxy: models.ProxyConfig{
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

		var response contracts.ProxyTestResponse
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

		cfg := config.DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "main"
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
			"main": {URL: nonProxy.URL},
		}

		testCfg := cfg
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		reqBody := contracts.ProxyTestRequest{
			Mode:      "direct",
			TargetURL: "https://example.com",
			Proxy: models.ProxyConfig{
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

		var response contracts.ProxyTestResponse
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

		testCfg := config.DefaultConfig(nil, nil)
		deps := newTestDeps(testCfg)

		router := gin.New()
		router.POST("/proxy/test", testProxy(testkit.GetTestRuntime(deps)))

		reqBody := `{"mode":"flaresolverr","target_url":"https://example.com","flaresolverr":{"enabled":true,"url":"` + fs.URL + `","timeout":5}}`
		req := httptest.NewRequest(http.MethodPost, "/proxy/test", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response contracts.ProxyTestResponse
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
		coreDeps := createTestDeps(t, config.DefaultConfig(nil, nil), tempConfigFile)
		deps := systemDepsFromCore(coreDeps)

		router := gin.New()
		router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

		cfg := config.DefaultConfig(nil, nil)
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
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not enforce Unix-style file permissions")
		}

		tempDir := t.TempDir()
		coreDeps := createTestDeps(t, config.DefaultConfig(nil, nil), tempDir)
		deps := systemDepsFromCore(coreDeps)

		router := gin.New()
		router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

		cfg := config.DefaultConfig(nil, nil)
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
	coreDeps := createTestDeps(t, config.DefaultConfig(nil, nil), tempConfigFile)
	deps := systemDepsFromCore(coreDeps)

	router := gin.New()
	router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

	cfg := config.DefaultConfig(nil, nil)
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
	updated := deps.CoreDeps.GetConfig()
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
		coreDeps := createTestDeps(t, config.DefaultConfig(nil, nil), tempConfigFile)
		deps := systemDepsFromCore(coreDeps)
		// Initialize token store for this test
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

		// Change proxy settings without providing a token
		cfg := *config.DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "test"
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
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
		coreDeps := createTestDeps(t, config.DefaultConfig(nil, nil), tempConfigFile)
		deps := systemDepsFromCore(coreDeps)
		// Initialize token store
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

		// Create new proxy config
		newProxy := models.ProxyConfig{
			Enabled:        true,
			DefaultProfile: "test",
			Profiles: map[string]models.ProxyProfile{
				"test": {URL: "http://proxy.example:8080"},
			},
		}

		// Create a valid token for the new proxy config
		newHash, _ := core.HashProxyConfig(newProxy)
		vt, _, _ := deps.TokenStore.Create("global", newHash)

		// Build the full config with the new proxy settings
		cfg := *config.DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy = newProxy

		reqBody := UpdateConfigRequest{
			Config: cfg,
			ProxyVerificationTokens: map[string]string{
				"global": vt,
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
		coreDeps := createTestDeps(t, config.DefaultConfig(nil, nil), tempConfigFile)
		deps := systemDepsFromCore(coreDeps)
		// Initialize token store
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

		// Change proxy settings with an invalid token
		cfg := *config.DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy.Enabled = true
		cfg.Scrapers.Proxy.DefaultProfile = "test"
		cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
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
		initialCfg := config.DefaultConfig(nil, nil)
		initialCfg.Scrapers.Proxy.Enabled = true
		initialCfg.Scrapers.Proxy.DefaultProfile = "test"
		initialCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
			"test": {URL: "http://proxy.example:8080"},
		}

		coreDeps := createTestDeps(t, initialCfg, tempConfigFile)
		coreDeps.CoreDeps.SetConfig(initialCfg)
		deps := systemDepsFromCore(coreDeps)
		// Initialize token store
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

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
		coreDeps := createTestDeps(t, config.DefaultConfig(nil, nil), tempConfigFile)
		deps := systemDepsFromCore(coreDeps)
		// Initialize token store
		deps.TokenStore = core.NewTokenStore()

		router := gin.New()
		router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

		// Create new proxy config
		newProxy := models.ProxyConfig{
			Enabled:        true,
			DefaultProfile: "test",
			Profiles: map[string]models.ProxyProfile{
				"test": {URL: "http://proxy.example:8080"},
			},
		}

		// Create a token with wrong config hash (simulates token for different config)
		wrongHashToken, _, _ := deps.TokenStore.Create("global", "wrong_hash")

		// Build the full config with the new proxy settings
		cfg := *config.DefaultConfig(nil, nil)
		cfg.Scrapers.Proxy = newProxy

		reqBody := UpdateConfigRequest{
			Config: cfg,
			ProxyVerificationTokens: map[string]string{
				"global": wrongHashToken, // Token exists but for different config hash
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

func TestScraperDisplayTitleAndOptions(t *testing.T) {
	t.Run("returns fallback for unknown scraper", func(t *testing.T) {
		title, options := scraperDisplayTitleAndOptions(nil, "nonexistent_scraper", nil, nil)
		assert.Equal(t, "nonexistent_scraper", title)
		assert.NotEmpty(t, options)
	})

	t.Run("returns registered options for known scraper", func(t *testing.T) {
		title, options := scraperDisplayTitleAndOptions(nil, "r18dev", nil, nil)
		assert.NotEmpty(t, title)
		assert.NotEmpty(t, options)
	})
}

func TestProxyProfilesEqual(t *testing.T) {
	t.Run("equal maps", func(t *testing.T) {
		a := map[string]models.ProxyProfile{
			"test": {URL: "http://localhost:8080"},
		}
		b := map[string]models.ProxyProfile{
			"test": {URL: "http://localhost:8080"},
		}
		assert.True(t, proxyProfilesEqual(a, b))
	})

	t.Run("different length", func(t *testing.T) {
		a := map[string]models.ProxyProfile{
			"test": {URL: "http://localhost:8080"},
		}
		b := map[string]models.ProxyProfile{}
		assert.False(t, proxyProfilesEqual(a, b))
	})

	t.Run("different values", func(t *testing.T) {
		a := map[string]models.ProxyProfile{
			"test": {URL: "http://localhost:8080"},
		}
		b := map[string]models.ProxyProfile{
			"test": {URL: "http://other:9090"},
		}
		assert.False(t, proxyProfilesEqual(a, b))
	})

	t.Run("missing key", func(t *testing.T) {
		a := map[string]models.ProxyProfile{
			"test": {URL: "http://localhost:8080"},
		}
		b := map[string]models.ProxyProfile{
			"other": {URL: "http://localhost:8080"},
		}
		assert.False(t, proxyProfilesEqual(a, b))
	})
}

func TestPreserveRedactedSecrets(t *testing.T) {
	t.Run("nil configs do nothing", func(t *testing.T) {
		preserveRedactedSecrets(nil, nil)
		preserveRedactedSecrets(nil, &config.Config{})
		preserveRedactedSecrets(&config.Config{}, nil)
	})

	t.Run("redacted DSN preserved", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Database.DSN = "real-dsn-value"
		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Database.DSN = models.RedactedValue
		preserveRedactedSecrets(old, newCfg)
		assert.Equal(t, "real-dsn-value", newCfg.Database.DSN)
	})

	t.Run("redacted scraper APIKey preserved", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {APIKey: "real-scraper-key"},
		}
		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {APIKey: models.RedactedValue},
		}
		preserveRedactedSecrets(old, newCfg)
		assert.Equal(t, "real-scraper-key", newCfg.Scrapers.Overrides["javstash"].APIKey)
	})

	t.Run("explicit scraper APIKey change preserved", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {APIKey: "old-key"},
		}
		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {APIKey: "new-key"},
		}
		preserveRedactedSecrets(old, newCfg)
		assert.Equal(t, "new-key", newCfg.Scrapers.Overrides["javstash"].APIKey)
	})

	t.Run("scraper APIKey with nil override skipped", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {APIKey: "real-key"},
		}
		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": nil,
		}
		preserveRedactedSecrets(old, newCfg)
		// Should not panic; nil newSettings is skipped
	})
}

// TestUpdateConfig_EmptyLogOutputDefaultsAndReloadsNonFatally verifies that an API
// config update with an empty logging.output does not break the reload: Normalize
// (via Prepare) defaults it to the standard dual-output target, so InitLogger gets
// a valid output and the reload succeeds (200). This guards the round-2 review
// concern that the InitLogger zero-output error could surface via the API path.
func TestUpdateConfig_EmptyLogOutputDefaultsAndReloadsNonFatally(t *testing.T) {
	tempConfigFile := filepath.Join(t.TempDir(), "config.yaml")
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), tempConfigFile)

	router := gin.New()
	router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))

	cfg := config.DefaultConfig(nil, nil)
	cfg.Logging.Output = "" // empty — previously could reach InitLogger with no valid targets

	body, err := json.Marshal(cfg)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"reload returns 200 regardless: InitLogger failure is non-fatal in reloadComponents")
	// The 200 above would pass even without the Normalize fix (reloadComponents
	// swallows InitLogger errors). The assertion below is the real pin for the
	// Normalize change: without it, updated.Logging.Output would remain "".
	updated := deps.CoreDeps.GetConfig()
	assert.Equal(t, config.DefaultConfig(nil, nil).Logging.Output, updated.Logging.Output,
		"empty logging.output should be defaulted by Normalize before reload")
}
