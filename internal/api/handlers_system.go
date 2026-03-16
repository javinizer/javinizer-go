package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/aventertainment"
	"github.com/javinizer/javinizer-go/internal/scraper/caribbeancom"
	"github.com/javinizer/javinizer-go/internal/scraper/dlgetchu"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraper/fc2"
	"github.com/javinizer/javinizer-go/internal/scraper/jav321"
	"github.com/javinizer/javinizer-go/internal/scraper/javbus"
	"github.com/javinizer/javinizer-go/internal/scraper/javdb"
	"github.com/javinizer/javinizer-go/internal/scraper/javlibrary"
	"github.com/javinizer/javinizer-go/internal/scraper/libredmm"
	"github.com/javinizer/javinizer-go/internal/scraper/mgstage"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	"github.com/javinizer/javinizer-go/internal/scraper/tokyohot"
	"github.com/javinizer/javinizer-go/internal/version"
)

// Mutex to serialize config updates (prevents concurrent read-modify-write races)
var configMutex sync.Mutex

const defaultProxyTestURL = "https://javdb.com"

// healthCheck godoc
// @Summary Health check
// @Description Check API health and list enabled scrapers
// @Tags system
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func healthCheck(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Use getter to get current registry (respects config reloads)
		registry := deps.GetRegistry()
		scrapers := []string{}
		for _, s := range registry.GetEnabled() {
			scrapers = append(scrapers, s.Name())
		}
		c.JSON(200, HealthResponse{
			Status:    "ok",
			Scrapers:  scrapers,
			Version:   version.Short(),
			Commit:    version.Commit,
			BuildDate: version.BuildDate,
		})
	}
}

// getConfig godoc
// @Summary Get configuration
// @Description Retrieve the current server configuration
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/config [get]
func getConfig(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read current config dynamically (respects config reloads) and include
		// runtime config file location for UI display.
		c.JSON(200, struct {
			*config.Config
			ConfigFilePath string `json:"config_file_path"`
		}{
			Config:         deps.GetConfig(),
			ConfigFilePath: deps.ConfigFile,
		})
	}
}

// getAvailableScrapers godoc
// @Summary Get available scrapers
// @Description Get list of all available scrapers with their display names and enabled status
// @Tags system
// @Produce json
// @Success 200 {object} AvailableScrapersResponse
// @Router /api/v1/scrapers [get]
func getAvailableScrapers(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		scrapers := []ScraperInfo{}
		cfg := deps.GetConfig()
		profileChoices := proxyProfileChoices(cfg)

		// Use getter to get current registry (respects config reloads)
		registry := deps.GetRegistry()
		registered := registry.GetAll()
		scraperByName := make(map[string]models.Scraper, len(registered))
		for _, scraper := range registered {
			scraperByName[scraper.Name()] = scraper
		}

		// Build deterministic order:
		// 1) config scrapers.priority order
		// 2) any remaining registered scrapers (sorted by name)
		orderedNames := make([]string, 0, len(scraperByName))
		seen := make(map[string]bool, len(scraperByName))
		if cfg != nil {
			for _, name := range cfg.Scrapers.Priority {
				if _, ok := scraperByName[name]; !ok || seen[name] {
					continue
				}
				orderedNames = append(orderedNames, name)
				seen[name] = true
			}
		}
		remainingNames := make([]string, 0, len(scraperByName))
		for name := range scraperByName {
			if !seen[name] {
				remainingNames = append(remainingNames, name)
			}
		}
		sort.Strings(remainingNames)
		orderedNames = append(orderedNames, remainingNames...)

		for _, name := range orderedNames {
			scraper := scraperByName[name]
			// Map internal names to display names
			displayName := name
			var options []ScraperOption

			switch name {
			case "r18dev":
				displayName = "R18.dev"
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "dmm":
				displayName = "DMM/Fanza"
				// DMM scraper options
				minTimeout := 5
				maxTimeout := 120
				options = []ScraperOption{
					{
						Key:         "scrape_actress",
						Label:       "Scrape Actress Information",
						Description: "Extract actress names and IDs from DMM. Disable for faster scraping if you only need actress data from other sources.",
						Type:        "boolean",
					},
					{
						Key:         "enable_browser",
						Label:       "Enable browser mode",
						Description: "Use browser automation for video.dmm.co.jp (required for JavaScript-rendered content)",
						Type:        "boolean",
					},
					{
						Key:         "browser_timeout",
						Label:       "Browser timeout",
						Description: "Maximum time to wait for browser operations",
						Type:        "number",
						Min:         &minTimeout,
						Max:         &maxTimeout,
						Unit:        "seconds",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "libredmm":
				displayName = "LibreDMM"
				options = []ScraperOption{
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "LibreDMM base URL",
						Type:        "string",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "mgstage":
				displayName = "MGStage"
				// MGStage scraper options
				options = []ScraperOption{
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting (0 = no delay)",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "javlibrary":
				displayName = "JavLibrary"
				options = []ScraperOption{
					{
						Key:         "language",
						Label:       "Language",
						Description: "Language for metadata (affects title, genres, and actress names)",
						Type:        "select",
						Choices: []ScraperChoice{
							{Value: "en", Label: "English"},
							{Value: "ja", Label: "Japanese"},
							{Value: "cn", Label: "Chinese (Simplified)"},
							{Value: "tw", Label: "Chinese (Traditional)"},
						},
					},
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "JavLibrary base URL (leave default unless you need a mirror/domain override)",
						Type:        "string",
					},
					{
						Key:         "use_flaresolverr",
						Label:       "Use FlareSolverr",
						Description: "Route requests through FlareSolverr to bypass Cloudflare protection (requires FlareSolverr to be configured in Proxy settings)",
						Type:        "boolean",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "javdb":
				displayName = "JavDB"
				options = []ScraperOption{
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "JavDB base URL (leave default unless you need a mirror/domain override)",
						Type:        "string",
					},
					{
						Key:         "use_flaresolverr",
						Label:       "Use FlareSolverr",
						Description: "Route requests through FlareSolverr to bypass Cloudflare protection (often needed for JavDB)",
						Type:        "boolean",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "javbus":
				displayName = "JavBus"
				options = []ScraperOption{
					{
						Key:         "language",
						Label:       "Language",
						Description: "Language for metadata output",
						Type:        "select",
						Choices: []ScraperChoice{
							{Value: "ja", Label: "Japanese"},
							{Value: "en", Label: "English"},
							{Value: "zh", Label: "Chinese"},
						},
					},
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "JavBus base URL (leave default unless you need a mirror/domain override)",
						Type:        "string",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "jav321":
				displayName = "Jav321"
				options = []ScraperOption{
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "Jav321 base URL",
						Type:        "string",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "tokyohot":
				displayName = "Tokyo-Hot"
				options = []ScraperOption{
					{
						Key:         "language",
						Label:       "Language",
						Description: "Language for metadata output",
						Type:        "select",
						Choices: []ScraperChoice{
							{Value: "ja", Label: "Japanese"},
							{Value: "en", Label: "English"},
							{Value: "zh", Label: "Chinese"},
						},
					},
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "Tokyo-Hot base URL",
						Type:        "string",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "aventertainment":
				displayName = "AV Entertainment"
				options = []ScraperOption{
					{
						Key:         "language",
						Label:       "Language",
						Description: "Language for metadata output",
						Type:        "select",
						Choices: []ScraperChoice{
							{Value: "en", Label: "English"},
							{Value: "ja", Label: "Japanese"},
						},
					},
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "AV Entertainment base URL",
						Type:        "string",
					},
					{
						Key:         "scrape_bonus_screens",
						Label:       "Scrape bonus screenshots",
						Description: "Append bonus image files (e.g., 特典ファイル) to screenshots",
						Type:        "boolean",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "dlgetchu":
				displayName = "DLGetchu"
				options = []ScraperOption{
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "DLGetchu base URL",
						Type:        "string",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "caribbeancom":
				displayName = "Caribbeancom"
				options = []ScraperOption{
					{
						Key:         "language",
						Label:       "Language",
						Description: "Language for metadata output",
						Type:        "select",
						Choices: []ScraperChoice{
							{Value: "ja", Label: "Japanese"},
							{Value: "en", Label: "English"},
						},
					},
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "Caribbeancom base URL",
						Type:        "string",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			case "fc2":
				displayName = "FC2"
				options = []ScraperOption{
					{
						Key:         "request_delay",
						Label:       "Request delay",
						Description: "Delay between requests to avoid rate limiting",
						Type:        "number",
						Min:         ptrInt(0),
						Max:         ptrInt(5000),
						Unit:        "ms",
					},
					{
						Key:         "base_url",
						Label:       "Base URL",
						Description: "FC2 base URL",
						Type:        "string",
					},
				}
				options = append(options, scraperFakeUserAgentOptions()...)
				options = append(options, scraperProxyOptions(profileChoices)...)
				options = append(options, scraperDownloadProxyOptions(profileChoices)...)
			}

			scrapers = append(scrapers, ScraperInfo{
				Name:        name,
				DisplayName: displayName,
				Enabled:     scraper.IsEnabled(),
				Options:     options,
			})
		}

		c.JSON(200, AvailableScrapersResponse{
			Scrapers: scrapers,
		})
	}
}

// testProxy godoc
// @Summary Test proxy connectivity
// @Description Test direct proxy or FlareSolverr access to a target URL using provided proxy settings
// @Tags system
// @Accept json
// @Produce json
// @Param request body ProxyTestRequest true "Proxy test request"
// @Success 200 {object} ProxyTestResponse
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/proxy/test [post]
func testProxy(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ProxyTestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid proxy test request"})
			return
		}

		targetURL := strings.TrimSpace(req.TargetURL)
		if targetURL == "" {
			targetURL = defaultProxyTestURL
		}
		if !isValidHTTPURL(targetURL) {
			c.JSON(400, ErrorResponse{Error: "target_url must be a valid http(s) URL"})
			return
		}

		start := time.Now()
		resp := ProxyTestResponse{
			Mode:      req.Mode,
			TargetURL: targetURL,
		}

		switch req.Mode {
		case "direct":
			if !req.Proxy.Enabled || strings.TrimSpace(req.Proxy.URL) == "" {
				c.JSON(400, ErrorResponse{Error: "proxy.enabled=true and proxy.url are required for direct proxy test"})
				return
			}
			resp.ProxyURL = httpclient.SanitizeProxyURL(req.Proxy.URL)

			client, err := httpclient.NewRestyClient(&req.Proxy, 30*time.Second, 0)
			if err != nil {
				resp.Success = false
				resp.DurationMS = time.Since(start).Milliseconds()
				resp.Message = fmt.Sprintf("failed to create proxy client: %v", err)
				c.JSON(200, resp)
				return
			}

			userAgent := deps.GetConfig().Scrapers.UserAgent
			if userAgent == "" {
				userAgent = config.DefaultUserAgent
			}

			httpResp, err := client.R().
				SetHeader("User-Agent", userAgent).
				SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8").
				Get(targetURL)

			resp.DurationMS = time.Since(start).Milliseconds()
			if err != nil {
				resp.Success = false
				resp.Message = fmt.Sprintf("direct proxy request failed: %v", err)
				c.JSON(200, resp)
				return
			}

			resp.StatusCode = httpResp.StatusCode()
			resp.Success = httpResp.StatusCode() >= 200 && httpResp.StatusCode() < 400
			if resp.Success {
				resp.Message = fmt.Sprintf("direct proxy request succeeded with status %d", httpResp.StatusCode())
			} else {
				resp.Message = fmt.Sprintf("direct proxy request returned status %d", httpResp.StatusCode())
			}
			c.JSON(200, resp)
		case "flaresolverr":
			if !req.Proxy.FlareSolverr.Enabled || strings.TrimSpace(req.Proxy.FlareSolverr.URL) == "" {
				c.JSON(400, ErrorResponse{Error: "proxy.flaresolverr.enabled=true and proxy.flaresolverr.url are required for flaresolverr test"})
				return
			}

			resp.ProxyURL = httpclient.SanitizeProxyURL(req.Proxy.URL)
			resp.FlareSolverrURL = req.Proxy.FlareSolverr.URL

			_, fs, err := httpclient.NewRestyClientWithFlareSolverr(&req.Proxy, 45*time.Second, 0)
			if err != nil {
				resp.Success = false
				resp.DurationMS = time.Since(start).Milliseconds()
				resp.Message = fmt.Sprintf("failed to create flaresolverr client: %v", err)
				c.JSON(200, resp)
				return
			}
			if fs == nil {
				resp.Success = false
				resp.DurationMS = time.Since(start).Milliseconds()
				resp.Message = "flaresolverr client is not enabled in proxy config"
				c.JSON(200, resp)
				return
			}

			html, cookies, err := fs.ResolveURL(targetURL)
			resp.DurationMS = time.Since(start).Milliseconds()
			if err != nil {
				resp.Success = false
				resp.Message = fmt.Sprintf("flaresolverr request failed: %v", err)
				c.JSON(200, resp)
				return
			}

			resp.Success = true
			resp.Message = fmt.Sprintf("flaresolverr resolved page successfully (%d bytes, %d cookies)", len(html), len(cookies))
			c.JSON(200, resp)
		default:
			c.JSON(400, ErrorResponse{Error: "mode must be 'direct' or 'flaresolverr'"})
		}
	}
}

// getTranslationModels godoc
// @Summary Get translation models
// @Description Fetch available models from an OpenAI-compatible base URL
// @Tags system
// @Accept json
// @Produce json
// @Param request body TranslationModelsRequest true "Translation model lookup request"
// @Success 200 {object} TranslationModelsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Router /api/v1/translation/models [post]
func getTranslationModels(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req TranslationModelsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid request format"})
			return
		}

		provider := strings.ToLower(strings.TrimSpace(req.Provider))
		if provider != "openai" {
			c.JSON(400, ErrorResponse{Error: "Only provider=openai is supported for model discovery"})
			return
		}

		baseURL := strings.TrimSpace(req.BaseURL)
		if !isValidHTTPURL(baseURL) {
			c.JSON(400, ErrorResponse{Error: "base_url must be a valid http(s) URL"})
			return
		}
		if strings.TrimSpace(req.APIKey) == "" {
			c.JSON(400, ErrorResponse{Error: "api_key is required for model discovery"})
			return
		}

		models, err := fetchOpenAICompatibleModels(c.Request.Context(), baseURL, req.APIKey)
		if err != nil {
			c.JSON(http.StatusBadGateway, ErrorResponse{Error: "Failed to fetch models: " + err.Error()})
			return
		}

		c.JSON(200, TranslationModelsResponse{Models: models})
	}
}

func isValidHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// updateConfig godoc
// @Summary Update configuration
// @Description Update and save the server configuration. The server will reload scrapers and aggregator with the new settings.
// @Tags system
// @Accept json
// @Produce json
// @Param config body config.Config true "Full configuration object"
// @Success 200 {object} map[string]interface{} "message: Configuration saved and reloaded successfully"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/config [put]
func updateConfig(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Serialize updates to prevent concurrent read-modify-write races
		configMutex.Lock()
		defer configMutex.Unlock()

		// Parse incoming config
		var newConfig config.Config
		if err := c.ShouldBindJSON(&newConfig); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid configuration format"})
			return
		}

		// Validate configuration on save so invalid settings are rejected in UI
		// before writing to disk or attempting component reload.
		if err := newConfig.Validate(); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid configuration: " + err.Error()})
			return
		}
		if err := validateTranslationSaveConfig(&newConfig); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid configuration: " + err.Error()})
			return
		}

		// Save old config for rollback in case reload fails
		oldConfig := deps.GetConfig()

		// Save new config to YAML file (empty arrays are preserved, not removed)
		if err := config.Save(&newConfig, deps.ConfigFile); err != nil {
			logging.Errorf("Failed to save config: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to save configuration"})
			return
		}

		// Reload components with new config (config not published until components are ready)
		// This prevents split-brain state where handlers see new config but old components
		if err := reloadComponents(deps, &newConfig); err != nil {
			logging.Errorf("Failed to reload components: %v", err)

			// Rollback: restore old config to YAML file to prevent restart failures
			// (in-memory config was never changed, so no need to rollback in memory)
			if saveErr := config.Save(oldConfig, deps.ConfigFile); saveErr != nil {
				logging.Errorf("CRITICAL: Failed to restore old config to file during rollback: %v", saveErr)
				c.JSON(500, ErrorResponse{Error: fmt.Sprintf("Configuration reload failed AND rollback save failed - manual intervention required: %v (original error: %v)", saveErr, err)})
				return
			}

			c.JSON(500, ErrorResponse{Error: "Configuration reload failed, reverted to previous version: " + err.Error()})
			return
		}

		logging.Info("Configuration updated and reloaded successfully")
		c.JSON(200, gin.H{
			"message": "Configuration saved and reloaded successfully",
		})
	}
}

// reloadComponents reinitializes components that depend on configuration
// This is called after config is updated to ensure all components use the new settings
// The new config is passed as a parameter and is NOT published until all components are ready
// This prevents split-brain state where handlers see new config but old components
func reloadComponents(deps *ServerDependencies, newCfg *config.Config) error {
	logging.Info("Reloading components with new configuration...")

	// 1. Build new scrapers (outside lock - can take time)
	logging.Debug("Reinitializing scraper registry...")
	newRegistry := models.NewScraperRegistry()

	// Get content ID repository for DMM scraper
	contentIDRepo := database.NewContentIDMappingRepository(deps.DB)

	// Register scrapers with new config
	newRegistry.Register(r18dev.New(newCfg))
	newRegistry.Register(dmm.New(newCfg, contentIDRepo))
	newRegistry.Register(libredmm.New(newCfg))
	newRegistry.Register(mgstage.New(newCfg))
	newRegistry.Register(javdb.New(newCfg))
	newRegistry.Register(javbus.New(newCfg))
	newRegistry.Register(jav321.New(newCfg))
	newRegistry.Register(tokyohot.New(newCfg))
	newRegistry.Register(aventertainment.New(newCfg))
	newRegistry.Register(dlgetchu.New(newCfg))
	newRegistry.Register(caribbeancom.New(newCfg))
	newRegistry.Register(fc2.New(newCfg))

	// Register JavLibrary scraper (may return error if language is invalid)
	javLibraryProxy := config.ResolveScraperProxy(newCfg.Scrapers.Proxy, newCfg.Scrapers.JavLibrary.Proxy)
	javlib, err := javlibrary.New(&newCfg.Scrapers.JavLibrary, javLibraryProxy, newCfg.Scrapers.UserAgent)
	if err != nil {
		logging.Warnf("Failed to initialize JavLibrary scraper: %v", err)
	} else {
		newRegistry.Register(javlib)
	}

	// 2. Build new aggregator (outside lock)
	logging.Debug("Reinitializing aggregator...")
	newAggregator := aggregator.NewWithDatabase(newCfg, deps.DB)

	// 3. Build new matcher (outside lock)
	logging.Debug("Reinitializing matcher...")
	newMatcher, err := matcher.NewMatcher(&newCfg.Matching)
	if err != nil {
		return fmt.Errorf("failed to reload matcher: %w", err)
	}

	// 4. Atomically swap ALL components AND config together with mutex protection
	// This ensures handlers never see mismatched config+components
	deps.mu.Lock()
	deps.Registry = newRegistry
	deps.Aggregator = newAggregator
	deps.Matcher = newMatcher
	deps.SetConfig(newCfg) // Publish config only after components are ready
	deps.mu.Unlock()

	logging.Infof("Reloaded scraper registry with %d scrapers", len(newRegistry.GetAll()))
	logging.Debug("Aggregator reloaded with new metadata priorities")
	logging.Debug("Matcher reloaded with new patterns")

	// 5. Reload logging configuration (non-fatal - keep current logger if reload fails)
	logging.Debug("Reinitializing logging configuration...")
	loggingCfg := &logging.Config{
		Level:  newCfg.Logging.Level,
		Format: newCfg.Logging.Format,
		Output: newCfg.Logging.Output,
	}
	if err := logging.InitLogger(loggingCfg); err != nil {
		// Log warning but don't fail the entire reload - keep using current logger
		logging.Warnf("Failed to reload logging configuration, keeping current logger: %v", err)
	} else {
		logging.Info("Logging configuration reloaded successfully")
	}

	logging.Info("✓ All components reloaded successfully")
	return nil
}

func fetchOpenAICompatibleModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/models"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	var decoded struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("invalid upstream response payload")
	}

	modelSet := make(map[string]struct{})
	for _, item := range decoded.Data {
		model := strings.TrimSpace(item.ID)
		if model == "" {
			continue
		}
		modelSet[model] = struct{}{}
	}

	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("no models found")
	}

	return models, nil
}

func scraperProxyOptions(profileChoices []ScraperChoice) []ScraperOption {
	return []ScraperOption{
		{
			Key:         "proxy.enabled",
			Label:       "Enable proxy for this scraper",
			Description: "Use proxy for this scraper (inherits global proxy profile when no scraper profile is selected)",
			Type:        "boolean",
		},
		{
			Key:         "proxy.profile",
			Label:       "Proxy profile",
			Description: "Optional scraper-specific proxy profile (leave empty to inherit global default profile)",
			Type:        "select",
			Choices:     profileChoices,
		},
	}
}

func scraperFakeUserAgentOptions() []ScraperOption {
	return []ScraperOption{
		{
			Key:         "use_fake_user_agent",
			Label:       "Use fake User-Agent",
			Description: "Use a browser-like User-Agent string for this scraper",
			Type:        "boolean",
		},
		{
			Key:         "fake_user_agent",
			Label:       "Fake User-Agent",
			Description: "Optional custom fake User-Agent (leave empty to use default browser User-Agent)",
			Type:        "string",
		},
	}
}

func scraperDownloadProxyOptions(profileChoices []ScraperChoice) []ScraperOption {
	return []ScraperOption{
		{
			Key:         "download_proxy.enabled",
			Label:       "Download proxy enabled",
			Description: "Enable scraper-specific download proxy override",
			Type:        "boolean",
		},
		{
			Key:         "download_proxy.profile",
			Label:       "Download proxy profile",
			Description: "Optional scraper-specific download proxy profile (leave empty to inherit scraper/global proxy profile)",
			Type:        "select",
			Choices:     profileChoices,
		},
	}
}

func proxyProfileChoices(cfg *config.Config) []ScraperChoice {
	choices := []ScraperChoice{
		{Value: "", Label: "Inherit Default"},
	}
	if cfg == nil || len(cfg.Scrapers.Proxy.Profiles) == 0 {
		return choices
	}

	names := make([]string, 0, len(cfg.Scrapers.Proxy.Profiles))
	for name := range cfg.Scrapers.Proxy.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		choices = append(choices, ScraperChoice{
			Value: name,
			Label: name,
		})
	}

	return choices
}

// ptrInt returns a pointer to an int value
func ptrInt(v int) *int {
	return &v
}

func validateTranslationSaveConfig(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}

	translationCfg := cfg.Metadata.Translation
	if !translationCfg.Enabled {
		return nil
	}

	provider := strings.ToLower(strings.TrimSpace(translationCfg.Provider))
	switch provider {
	case "openai":
		if strings.TrimSpace(translationCfg.OpenAI.APIKey) == "" {
			return fmt.Errorf("metadata.translation.openai.api_key is required when provider=openai")
		}
	case "deepl":
		if strings.TrimSpace(translationCfg.DeepL.APIKey) == "" {
			return fmt.Errorf("metadata.translation.deepl.api_key is required when provider=deepl")
		}
	case "google":
		if strings.ToLower(strings.TrimSpace(translationCfg.Google.Mode)) == "paid" &&
			strings.TrimSpace(translationCfg.Google.APIKey) == "" {
			return fmt.Errorf("metadata.translation.google.api_key is required when provider=google and mode=paid")
		}
	}

	return nil
}
