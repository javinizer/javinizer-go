package system

import (
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// ConfigUpdateService encapsulates the business logic for validating, persisting,
// and applying a configuration update. The HTTP handler becomes a thin wrapper:
// parse JSON → call ValidateAndApply → map error to HTTP status.
//
// Extracting this service decouples validation/preservation/reload/rollback logic
// from Gin, making it testable without an HTTP stack.
type ConfigUpdateService struct {
	rt         *core.APIRuntime
	deps       *core.APIDeps
	configFile string
}

// NewConfigUpdateService creates a service bound to the given runtime and config file path.
func NewConfigUpdateService(rt *core.APIRuntime, configFile string) *ConfigUpdateService {
	return &ConfigUpdateService{rt: rt, deps: rt.Deps(), configFile: configFile}
}

// ValidateAndApply runs the full config update pipeline:
//  1. Prepare (defaults, validation)
//  2. Validate translation provider config
//  3. Validate proxy verification tokens
//  4. Preserve redacted secrets from the old config
//  5. Save the new config to YAML
//  6. Reload components (scraper registry, logging)
//  7. Roll back YAML file if reload fails
//
// The caller (HTTP handler) is responsible for serialising concurrent access
// (via Runtime.ConfigUpdateMu) before calling this method.
func (s *ConfigUpdateService) ValidateAndApply(oldCfg *config.Config, newCfg *config.Config, proxyTokens map[string]string) error {
	// Preserve secrets that were redacted in the GET response
	preserveRedactedSecrets(oldCfg, newCfg)

	// Run full config preparation pipeline before save/reload.
	if _, err := config.Prepare(newCfg); err != nil {
		return &validationError{message: err.Error()}
	}
	if err := validateTranslationSaveConfig(newCfg); err != nil {
		return &validationError{message: "Invalid configuration: " + err.Error()}
	}
	if err := validateProxySaveConfig(s.deps, newCfg, proxyTokens); err != nil {
		return &validationError{message: "Invalid configuration: " + err.Error()}
	}

	// Save new config to YAML file (empty arrays are preserved, not removed)
	if err := config.Save(newCfg, s.configFile); err != nil {
		logging.Errorf("Failed to save config: %v", err)
		return &persistError{message: "Failed to save configuration"}
	}

	// Reload components with new config (config not published until components are ready)
	if err := reloadComponents(s.rt, s.deps, newCfg); err != nil {
		logging.Errorf("Failed to reload components: %v", err)

		// Rollback: restore old config to YAML file to prevent restart failures
		// (in-memory config was never changed, so no need to rollback in memory)
		if saveErr := config.Save(oldCfg, s.configFile); saveErr != nil {
			logging.Errorf("CRITICAL: Failed to restore old config to file during rollback: %v", saveErr)
			return &rollbackError{
				rollbackErr: saveErr,
				originalErr: err,
				message:     fmt.Sprintf("Configuration reload failed AND rollback save failed - manual intervention required: %v (original error: %v)", saveErr, err),
			}
		}

		return &reloadError{
			originalErr: err,
			message:     "Configuration reload failed, reverted to previous version: " + err.Error(),
		}
	}

	logging.Info("Configuration updated and reloaded successfully")
	return nil
}

// --- Typed errors for HTTP status mapping ---

// validationError indicates the new config failed validation (maps to 400).
type validationError struct {
	message string
}

func (e *validationError) Error() string { return e.message }

// persistError indicates the config could not be saved to disk (maps to 500).
type persistError struct {
	message string
}

func (e *persistError) Error() string { return e.message }

// reloadError indicates component reload failed but rollback succeeded (maps to 500).
type reloadError struct {
	originalErr error
	message     string
}

func (e *reloadError) Error() string { return e.message }

// rollbackError indicates both reload and rollback failed (maps to 500, critical).
type rollbackError struct {
	rollbackErr error
	originalErr error
	message     string
}

func (e *rollbackError) Error() string { return e.message }

// mapConfigErrorToHTTP returns the appropriate HTTP status code for a ConfigUpdateService error.
// Returns 0 for nil errors (caller should send 200).
// Uses errors.As instead of type switch to correctly handle wrapped errors.
func mapConfigErrorToHTTP(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var valErr *validationError
	if errors.As(err, &valErr) {
		return 400, err.Error()
	}
	var persistErr *persistError
	if errors.As(err, &persistErr) {
		return 500, err.Error()
	}
	var reloadErr *reloadError
	if errors.As(err, &reloadErr) {
		return 500, err.Error()
	}
	var rollbackErr *rollbackError
	if errors.As(err, &rollbackErr) {
		return 500, err.Error()
	}
	return 500, err.Error()
}
