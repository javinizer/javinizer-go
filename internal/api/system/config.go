package system

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// UpdateConfigRequest represents a configuration update request with proxy verification.
type UpdateConfigRequest struct {
	config.Config
	ProxyVerificationTokens map[string]string `json:"proxy_verification_tokens,omitempty"`
}

// getConfig godoc
// @Summary Get configuration
// @Description Retrieve the current server configuration including all settings for scrapers, output, database, and API. Returns the active configuration with runtime file path.
// @Tags system
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/config [get]
func getConfig(deps *core.APIDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, struct {
			*config.Config
			ConfigFilePath string `json:"config_file_path"`
		}{
			Config:         deps.CoreDeps.GetConfig().Redact(),
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
// @Success 200 {object} map[string]any "message: Configuration saved and reloaded successfully"
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/config [put]
// updateConfig handles PUT /api/v1/config. This is the only API handler that
// reads and writes the full *config.Config directly, bypassing the narrow
// APIConfig pattern used by all other handlers. This exception is necessary
// because the config endpoint must serialize/deserialize the complete config.
// Do not copy this pattern into other handlers.
func updateConfig(rt *core.APIRuntime) gin.HandlerFunc {
	deps := rt.Deps()
	svc := NewConfigUpdateService(rt, deps.ConfigFile)

	return func(c *gin.Context) {
		// Serialize updates to prevent concurrent read-modify-write races
		rt.GetRuntime().ConfigUpdateMu.Lock()
		defer rt.GetRuntime().ConfigUpdateMu.Unlock()

		// Parse incoming config
		var req UpdateConfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Invalid configuration format"})
			return
		}

		oldCfg := deps.CoreDeps.GetConfig()
		err := svc.ValidateAndApply(oldCfg, &req.Config, req.ProxyVerificationTokens)
		if err != nil {
			status, msg := mapConfigErrorToHTTP(err)
			c.JSON(status, contracts.ErrorResponse{Error: msg})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Configuration saved and reloaded successfully",
		})
	}
}

// reloadComponents reinitializes components that depend on configuration
// This is called after config is updated to ensure all components use the new settings
// The new config is passed as a parameter and is NOT published until all components are ready
// This prevents split-brain state where handlers see new config but old components
func reloadComponents(rt *core.APIRuntime, deps *core.APIDeps, newCfg *config.Config) error {
	logging.Info("Reloading components with new configuration...")

	// Rebuild scraper registry and swap config atomically via APIRuntime.ReloadConfig.
	// The workflow factory creates aggregator/matcher from config on each request,
	// so ReloadConfig does not need to construct them.
	//
	// Reuse the existing APIRuntime so that its WebSocket hub and serverCtx
	// are preserved. Only fall back to NewAPIRuntime if the
	// runtime hasn't been initialized yet (should not happen in production).
	if rt == nil {
		logging.Warn("No existing APIRuntime found, creating a new one")
		rt = core.NewAPIRuntime(deps)
	}
	if err := rt.ReloadConfig(newCfg); err != nil {
		return err
	}

	logging.Info("✓ All components reloaded successfully")

	// Reload logging configuration (non-fatal - keep current logger if reload fails)
	logging.Debug("Reinitializing logging configuration...")
	loggingCfg := &logging.Config{
		Level:      newCfg.Logging.Level,
		Format:     newCfg.Logging.Format,
		Output:     newCfg.Logging.Output,
		MaxSizeMB:  newCfg.Logging.MaxSizeMB,
		MaxBackups: newCfg.Logging.MaxBackups,
		MaxAgeDays: newCfg.Logging.MaxAgeDays,
		Compress:   newCfg.Logging.Compress,
	}
	if err := logging.InitLogger(loggingCfg); err != nil {
		// Log warning but don't fail the entire reload - keep using current logger
		logging.Warnf("Failed to reload logging configuration, keeping current logger: %v", err)
	} else {
		logging.Info("Logging configuration reloaded successfully")
	}

	return nil
}

func validateTranslationSaveConfig(cfg *config.Config) error {
	return config.ValidateTranslationProvider(cfg)
}

// validateProxySaveConfig validates that proxy settings were tested before saving
// Returns error if proxy config changed but no valid verification token provided
func validateProxySaveConfig(deps *core.APIDeps, newCfg *config.Config, tokens map[string]string) error {
	if deps.TokenStore == nil {
		// Token store not initialized, skip verification (for testing or legacy mode)
		return nil
	}

	oldCfg := deps.CoreDeps.GetConfig()
	if oldCfg == nil {
		return nil
	}

	// Check if global proxy settings changed
	// Normalize Profile → DefaultProfile to match the normalization applied by
	// the proxy test endpoint (proxy.go). Without this, the hash computed here
	// would differ from the hash embedded in the test verification token when the
	// frontend sends the default profile name in the Profile field.
	normalizedProxy := newCfg.Scrapers.Proxy
	oldProxy := oldCfg.Scrapers.Proxy
	normalizeDefaultProfile := func(proxy *models.ProxyConfig) {
		if proxy.DefaultProfile == "" && proxy.Profile != "" {
			proxy.DefaultProfile = proxy.Profile
			proxy.Profile = ""
		}
	}
	normalizeDefaultProfile(&normalizedProxy)
	normalizeDefaultProfile(&oldProxy)

	// Check if global proxy enabled status or URL changed (meaningful changes).
	// Compare normalized-to-normalized so a save that only shifts the effective
	// default via Profile (normalized to DefaultProfile) is still detected as a
	// change and triggers token re-validation — otherwise the change could skip
	// verification entirely.
	globalChanged := oldProxy.Enabled != normalizedProxy.Enabled ||
		oldProxy.DefaultProfile != normalizedProxy.DefaultProfile ||
		!proxyProfilesEqual(oldProxy.Profiles, normalizedProxy.Profiles)

	if globalChanged {
		// Hash the proxy config only when a change is detected. A hashing
		// failure means we cannot safely verify the token against the new
		// settings, so fail closed immediately instead of risking validation
		// against an empty/incorrect hash.
		newGlobalHash, err := core.HashProxyConfig(normalizedProxy)
		if err != nil {
			return fmt.Errorf("failed to hash proxy settings: %w", err)
		}

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
	flareSolverrChanged := oldCfg.Scrapers.FlareSolverr.Enabled != newCfg.Scrapers.FlareSolverr.Enabled ||
		oldCfg.Scrapers.FlareSolverr.URL != newCfg.Scrapers.FlareSolverr.URL ||
		oldCfg.Scrapers.FlareSolverr.Timeout != newCfg.Scrapers.FlareSolverr.Timeout

	if flareSolverrChanged {
		// Hash the FlareSolverr config only when a change is detected; fail
		// closed on hashing errors so no token is validated against an
		// incorrect hash.
		newFlareSolverrHash, err := core.HashProxyConfig(newCfg.Scrapers.FlareSolverr)
		if err != nil {
			return fmt.Errorf("failed to hash flaresolverr settings: %w", err)
		}

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
func proxyProfilesEqual(a, b map[string]models.ProxyProfile) bool {
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

	if new.Database.DSN == models.RedactedValue {
		new.Database.DSN = old.Database.DSN
	}

	if new.Metadata.Translation.OpenAI.APIKey == models.RedactedValue {
		new.Metadata.Translation.OpenAI.APIKey = old.Metadata.Translation.OpenAI.APIKey
	}
	if new.Metadata.Translation.DeepL.APIKey == models.RedactedValue {
		new.Metadata.Translation.DeepL.APIKey = old.Metadata.Translation.DeepL.APIKey
	}
	if new.Metadata.Translation.Google.APIKey == models.RedactedValue {
		new.Metadata.Translation.Google.APIKey = old.Metadata.Translation.Google.APIKey
	}
	if new.Metadata.Translation.OpenAICompatible.APIKey == models.RedactedValue {
		new.Metadata.Translation.OpenAICompatible.APIKey = old.Metadata.Translation.OpenAICompatible.APIKey
	}
	if new.Metadata.Translation.Anthropic.APIKey == models.RedactedValue {
		new.Metadata.Translation.Anthropic.APIKey = old.Metadata.Translation.Anthropic.APIKey
	}

	preserveRedactedProxyProfiles(old.Scrapers.Proxy.Profiles, new.Scrapers.Proxy.Profiles)
	preserveRedactedProxyProfiles(old.Output.Download.DownloadProxy.Profiles, new.Output.Download.DownloadProxy.Profiles)

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
			if newSettings.APIKey == models.RedactedValue {
				newSettings.APIKey = oldSettings.APIKey
			}
		}
	}
}

func preserveRedactedProxyProfiles(old, new map[string]models.ProxyProfile) {
	if old == nil || new == nil {
		return
	}
	for k, newProfile := range new {
		oldProfile, ok := old[k]
		if !ok {
			continue
		}
		if newProfile.Username == models.RedactedValue {
			newProfile.Username = oldProfile.Username
		}
		if newProfile.Password == models.RedactedValue {
			newProfile.Password = oldProfile.Password
		}
		new[k] = newProfile
	}
}
