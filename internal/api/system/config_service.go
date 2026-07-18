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

// scraperNames returns the names of scrapers registered in the active registry,
// so sparse persistence can recognise (and delete) registered scraper blocks.
// Returns nil when no registry is wired (e.g. minimal test deps), in which case
// scraper blocks are treated as unknown and preserved.
func (s *ConfigUpdateService) scraperNames() []string {
	if s.deps == nil || s.deps.CoreDeps == nil || s.deps.CoreDeps.ScraperRegistry == nil {
		return nil
	}
	return s.deps.CoreDeps.GetRegistry().Names()
}

// ValidateAndApply runs the full config update pipeline:
//  1. Load disk-origin snapshot for secret preservation (pre-env)
//  2. Preserve redacted secrets from disk state, never runtime state
//  3. Prepare for persistence (structural validation, no credential check)
//  4. Clone + apply env overrides -> runtimeCfg
//  5. Prepare runtime (full validation incl. credentials)
//  6. Validate translation + proxy tokens against runtimeCfg
//  7. Save persistedCfg sparsely to disk
//  8. Reload components (publishes runtimeCfg only on success)
//  9. Roll back sparsely to diskCfg if reload fails
//
// The caller (HTTP handler) is responsible for serialising concurrent access
// (via Runtime.ConfigUpdateMu) before calling this method.
func (s *ConfigUpdateService) ValidateAndApply(oldCfg *config.Config, newCfg *config.Config, proxyTokens map[string]string) error {
	diskCfg := s.rt.DiskConfigSnapshot()
	if diskCfg == nil {
		loaded, err := config.Load(s.configFile)
		if err != nil {
			return &persistError{message: "Failed to load disk config for secret preservation"}
		}
		diskCfg = loaded
	}
	preserveRedactedSecrets(diskCfg, newCfg)

	if _, err := config.PrepareForPersistence(newCfg); err != nil {
		return &validationError{message: err.Error()}
	}

	runtimeCfg := newCfg.Clone()
	config.ApplyEnvironmentOverrides(runtimeCfg)
	if _, err := config.PrepareRuntime(runtimeCfg); err != nil {
		return &validationError{message: err.Error()}
	}

	if err := validateTranslationSaveConfig(runtimeCfg); err != nil {
		return &validationError{message: "Invalid configuration: " + err.Error()}
	}
	if err := validateProxySaveConfig(s.deps, runtimeCfg, proxyTokens); err != nil {
		return &validationError{message: "Invalid configuration: " + err.Error()}
	}

	ctx, err := config.BuildSparseSaveContextWithNames(s.scraperNames())
	if err != nil {
		return &validationError{message: err.Error()}
	}
	storage := config.NewConfigStorage(nil, nil)
	if err := storage.SaveSparse(newCfg, s.configFile, ctx); err != nil {
		logging.Errorf("Failed to save config: %v", err)
		return &persistError{message: "Failed to save configuration"}
	}

	if err := reloadComponents(s.rt, s.deps, runtimeCfg); err != nil {
		logging.Errorf("Failed to reload components: %v", err)
		if saveErr := storage.SaveSparse(diskCfg, s.configFile, ctx); saveErr != nil {
			logging.Errorf("CRITICAL: Failed to restore old config: %v", saveErr)
			return &rollbackError{
				rollbackErr: saveErr,
				originalErr: err,
				message:     fmt.Sprintf("Configuration reload failed AND rollback save failed: %v (original: %v)", saveErr, err),
			}
		}
		return &reloadError{
			originalErr: err,
			message:     "Configuration reload failed, reverted: " + err.Error(),
		}
	}

	logging.Info("Configuration updated and reloaded successfully")
	s.rt.SetInitialConfigs(runtimeCfg, newCfg)
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
