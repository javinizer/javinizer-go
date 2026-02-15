package api

import (
	"fmt"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraper/mgstage"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
)

// Mutex to serialize config updates (prevents concurrent read-modify-write races)
var configMutex sync.Mutex

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
			Status:   "ok",
			Scrapers: scrapers,
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
		// Read current config dynamically (respects config reloads)
		c.JSON(200, deps.GetConfig())
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

		// Use getter to get current registry (respects config reloads)
		registry := deps.GetRegistry()

		// Get all registered scrapers
		for _, scraper := range registry.GetAll() {
			name := scraper.Name()

			// Map internal names to display names
			displayName := name
			var options []ScraperOption

			switch name {
			case "r18dev":
				displayName = "R18.dev"
				// R18Dev has no additional options
				options = []ScraperOption{}
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
	newRegistry.Register(mgstage.New(newCfg))

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

// ptrInt returns a pointer to an int value
func ptrInt(v int) *int {
	return &v
}
