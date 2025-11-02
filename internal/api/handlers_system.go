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
func healthCheck(registry *models.ScraperRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
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
func getConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(200, cfg)
	}
}

// getAvailableScrapers godoc
// @Summary Get available scrapers
// @Description Get list of all available scrapers with their display names and enabled status
// @Tags system
// @Produce json
// @Success 200 {object} AvailableScrapersResponse
// @Router /api/v1/scrapers [get]
func getAvailableScrapers(registry *models.ScraperRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		scrapers := []ScraperInfo{}

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
						Key:         "enable_headless",
						Label:       "Enable headless browser",
						Description: "Use headless browser for video.dmm.co.jp (required for some content)",
						Type:        "boolean",
					},
					{
						Key:         "headless_timeout",
						Label:       "Headless timeout",
						Description: "Maximum time to wait for headless browser operations",
						Type:        "number",
						Min:         &minTimeout,
						Max:         &maxTimeout,
						Unit:        "seconds",
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

		// Save to YAML file (empty arrays are preserved, not removed)
		if err := config.Save(&newConfig, deps.ConfigFile); err != nil {
			logging.Errorf("Failed to save config: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to save configuration"})
			return
		}

		// Update the in-memory config
		*deps.Config = newConfig

		// Reload components that depend on config
		if err := reloadComponents(deps); err != nil {
			logging.Errorf("Failed to reload components: %v", err)
			c.JSON(500, ErrorResponse{Error: "Configuration saved but failed to reload: " + err.Error()})
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
func reloadComponents(deps *ServerDependencies) error {
	cfg := deps.Config

	logging.Info("Reloading components with new configuration...")

	// 1. Reload scrapers
	logging.Debug("Reinitializing scraper registry...")
	newRegistry := models.NewScraperRegistry()

	// Get content ID repository for DMM scraper
	contentIDRepo := database.NewContentIDMappingRepository(deps.DB)

	// Register scrapers with new config
	newRegistry.Register(r18dev.New(cfg))
	newRegistry.Register(dmm.New(cfg, contentIDRepo))

	// Replace the old registry with the new one
	deps.Registry = newRegistry
	logging.Infof("Reloaded scraper registry with %d scrapers", len(deps.Registry.GetAll()))

	// 2. Reload aggregator
	logging.Debug("Reinitializing aggregator...")
	deps.Aggregator = aggregator.NewWithDatabase(cfg, deps.DB)
	logging.Debug("Aggregator reloaded with new metadata priorities")

	// 3. Reload matcher
	logging.Debug("Reinitializing matcher...")
	newMatcher, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		return fmt.Errorf("failed to reload matcher: %w", err)
	}
	deps.Matcher = newMatcher
	logging.Debug("Matcher reloaded with new patterns")

	logging.Info("✓ All components reloaded successfully")
	return nil
}
