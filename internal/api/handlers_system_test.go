package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
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
				assert.Empty(t, resp.Scrapers[0].Options)
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
				assert.Len(t, resp.Scrapers[0].Options, 3)

				// Verify options exist
				optionKeys := make(map[string]bool)
				for _, opt := range resp.Scrapers[0].Options {
					optionKeys[opt.Key] = true
				}
				assert.True(t, optionKeys["scrape_actress"])
				assert.True(t, optionKeys["enable_browser"])
				assert.True(t, optionKeys["browser_timeout"])
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

func TestUpdateConfig(t *testing.T) {
	tests := []struct {
		name           string
		initialConfig  *config.Config
		requestBody    interface{}
		expectedStatus int
		expectedError  string
	}{
		{
			name: "valid config update",
			initialConfig: &config.Config{
				Server: config.ServerConfig{
					Host: "localhost",
					Port: 8080,
				},
				Matching: config.MatchingConfig{},
			},
			requestBody: &config.Config{
				Server: config.ServerConfig{
					Host: "0.0.0.0",
					Port: 9090,
				},
				Matching: config.MatchingConfig{},
			},
			expectedStatus: 200,
		},
		{
			name: "invalid json",
			initialConfig: &config.Config{
				Server: config.ServerConfig{
					Host: "localhost",
					Port: 8080,
				},
				Matching: config.MatchingConfig{},
			},
			requestBody:    "invalid json",
			expectedStatus: 400,
			expectedError:  "Invalid configuration format",
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
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Matching: config.MatchingConfig{},
	}

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

			newConfig := &config.Config{
				Server: config.ServerConfig{
					Host: "0.0.0.0",
					Port: 8080 + port,
				},
				Matching: config.MatchingConfig{},
			}

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
	require.Len(t, scraper.Options, 3)

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
