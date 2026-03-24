package system

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scraper"
)

// getConfig godoc
// @Summary Get configuration
// @Description Retrieve the current server configuration including all settings for scrapers, output, database, and API. Returns the active configuration with runtime file path.
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} ErrorResponse
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
		core.ConfigUpdateMutex.Lock()
		defer core.ConfigUpdateMutex.Unlock()

		// Parse incoming config
		var newConfig config.Config
		if err := c.ShouldBindJSON(&newConfig); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid configuration format"})
			return
		}

		// Run full config preparation pipeline before save/reload.
		if _, err := config.Prepare(&newConfig); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
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
	newRegistry, err := scraper.NewDefaultScraperRegistry(newCfg, deps.DB)
	if err != nil {
		return fmt.Errorf("failed to initialize scraper registry: %w", err)
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
	deps.ReplaceReloadable(newCfg, newRegistry, newAggregator, newMatcher)

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
