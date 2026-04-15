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
		c.JSON(200, struct {
			*config.Config
			ConfigFilePath string `json:"config_file_path"`
		}{
			Config:         deps.GetConfig().Redact(),
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
// @Param config body UpdateConfigRequest true "Full configuration object with optional proxy verification tokens"
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
		var req UpdateConfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid configuration format"})
			return
		}

		// Extract config from request
		newConfig := req.Config

		// Preserve secrets that were redacted in the GET response
		preserveRedactedSecrets(deps.GetConfig(), &newConfig)

		// Run full config preparation pipeline before save/reload.
		if _, err := config.Prepare(&newConfig); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}
		if err := validateTranslationSaveConfig(&newConfig); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid configuration: " + err.Error()})
			return
		}
		if err := validateProxySaveConfig(deps, &newConfig, req.ProxyVerificationTokens); err != nil {
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

// validateProxySaveConfig validates that proxy settings were tested before saving
// Returns error if proxy config changed but no valid verification token provided
func validateProxySaveConfig(deps *ServerDependencies, newCfg *config.Config, tokens map[string]string) error {
	if deps.TokenStore == nil {
		// Token store not initialized, skip verification (for testing or legacy mode)
		return nil
	}

	oldCfg := deps.GetConfig()
	if oldCfg == nil {
		return nil
	}

	// Check if global proxy settings changed
	newGlobalHash := core.HashProxyConfig(newCfg.Scrapers.Proxy)

	// Check if global proxy enabled status or URL changed (meaningful changes)
	globalChanged := oldCfg.Scrapers.Proxy.Enabled != newCfg.Scrapers.Proxy.Enabled ||
		oldCfg.Scrapers.Proxy.DefaultProfile != newCfg.Scrapers.Proxy.DefaultProfile ||
		!proxyProfilesEqual(oldCfg.Scrapers.Proxy.Profiles, newCfg.Scrapers.Proxy.Profiles)

	if globalChanged {
		// If no token provided for global scope, reject
		token, ok := tokens["global"]
		if !ok || token == "" {
			return fmt.Errorf("proxy settings changed but no test verification token provided - please test proxy before saving")
		}

		// Validate token
		if !deps.TokenStore.Validate(token, "global", newGlobalHash) {
			return fmt.Errorf("proxy verification token is invalid or expired - please test proxy again")
		}
	}

	// Check if FlareSolverr settings changed
	newFlareSolverrHash := core.HashProxyConfig(newCfg.Scrapers.FlareSolverr)

	flareSolverrChanged := oldCfg.Scrapers.FlareSolverr.Enabled != newCfg.Scrapers.FlareSolverr.Enabled ||
		oldCfg.Scrapers.FlareSolverr.URL != newCfg.Scrapers.FlareSolverr.URL ||
		oldCfg.Scrapers.FlareSolverr.Timeout != newCfg.Scrapers.FlareSolverr.Timeout

	if flareSolverrChanged {
		// If no token provided for flaresolverr scope, reject
		token, ok := tokens["flaresolverr"]
		if !ok || token == "" {
			return fmt.Errorf("flaresolverr settings changed but no test verification token provided - please test flaresolverr before saving")
		}

		// Validate token
		if !deps.TokenStore.Validate(token, "flaresolverr", newFlareSolverrHash) {
			return fmt.Errorf("flaresolverr verification token is invalid or expired - please test flaresolverr again")
		}
	}

	return nil
}

// proxyProfilesEqual compares two proxy profile maps for equality
func proxyProfilesEqual(a, b map[string]config.ProxyProfile) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if other, ok := b[k]; !ok || other != v {
			return false
		}
	}
	return true
}

func preserveRedactedSecrets(old, new *config.Config) {
	if old == nil || new == nil {
		return
	}

	if new.Database.DSN == config.RedactedValue {
		new.Database.DSN = old.Database.DSN
	}

	if new.Metadata.Translation.OpenAI.APIKey == config.RedactedValue {
		new.Metadata.Translation.OpenAI.APIKey = old.Metadata.Translation.OpenAI.APIKey
	}
	if new.Metadata.Translation.DeepL.APIKey == config.RedactedValue {
		new.Metadata.Translation.DeepL.APIKey = old.Metadata.Translation.DeepL.APIKey
	}
	if new.Metadata.Translation.Google.APIKey == config.RedactedValue {
		new.Metadata.Translation.Google.APIKey = old.Metadata.Translation.Google.APIKey
	}
	if new.Metadata.Translation.OpenAICompatible.APIKey == config.RedactedValue {
		new.Metadata.Translation.OpenAICompatible.APIKey = old.Metadata.Translation.OpenAICompatible.APIKey
	}
	if new.Metadata.Translation.Anthropic.APIKey == config.RedactedValue {
		new.Metadata.Translation.Anthropic.APIKey = old.Metadata.Translation.Anthropic.APIKey
	}

	preserveRedactedProxyProfiles(old.Scrapers.Proxy.Profiles, new.Scrapers.Proxy.Profiles)
	preserveRedactedProxyProfiles(old.Output.DownloadProxy.Profiles, new.Output.DownloadProxy.Profiles)

	if old.Scrapers.Overrides != nil && new.Scrapers.Overrides != nil {
		for name, oldSettings := range old.Scrapers.Overrides {
			newSettings, ok := new.Scrapers.Overrides[name]
			if !ok || oldSettings == nil || newSettings == nil {
				continue
			}
			if oldSettings.Proxy != nil && newSettings.Proxy != nil {
				preserveRedactedProxyProfiles(oldSettings.Proxy.Profiles, newSettings.Proxy.Profiles)
			}
			if oldSettings.DownloadProxy != nil && newSettings.DownloadProxy != nil {
				preserveRedactedProxyProfiles(oldSettings.DownloadProxy.Profiles, newSettings.DownloadProxy.Profiles)
			}
		}
	}
}

func preserveRedactedProxyProfiles(old, new map[string]config.ProxyProfile) {
	if old == nil || new == nil {
		return
	}
	for k, newProfile := range new {
		oldProfile, ok := old[k]
		if !ok {
			continue
		}
		if newProfile.Username == config.RedactedValue {
			newProfile.Username = oldProfile.Username
		}
		if newProfile.Password == config.RedactedValue {
			newProfile.Password = oldProfile.Password
		}
		new[k] = newProfile
	}
}
