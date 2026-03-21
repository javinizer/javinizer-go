package update

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/runtime"
	"github.com/javinizer/javinizer-go/internal/version"
)

// Service handles update checking with caching and background refresh.
type Service struct {
	checker   Checker
	store     *StateStore
	statePath string
	interval  time.Duration
	enabled   bool
}

// globalCheckMutex prevents concurrent background checks across all Service instances.
// This is needed because each request creates a new Service with its own StateStore.
var globalCheckMutex sync.Mutex

// NewService creates a new update service.
func NewService(cfg *config.Config) *Service {
	interval := time.Duration(cfg.System.UpdateCheckIntervalHours) * time.Hour
	if interval <= 0 {
		interval = DefaultCheckInterval
	}

	store := NewStateStore(runtime.UpdateStatePath(), interval)

	return &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: runtime.UpdateStatePath(),
		interval:  interval,
		enabled:   cfg.System.UpdateEnabled,
	}
}

// GetStatus returns the current update status.
// If the cached state is stale, it performs a background check.
func (s *Service) GetStatus(ctx context.Context) (*UpdateState, error) {
	if !s.enabled {
		return &UpdateState{
			Source: "disabled",
		}, nil
	}

	state, err := s.store.LoadState()
	if err != nil {
		logging.Debugf("Failed to load update state: %v", err)
		// Return empty state rather than failing
		return &UpdateState{
			Source: "error",
			Error:  err.Error(),
		}, nil
	}

	// If no state exists, return empty
	if state == nil {
		return &UpdateState{
			Source: "none",
		}, nil
	}

	// If state is stale and we should check, do it in background
	if s.store.ShouldCheck() {
		go s.BackgroundCheck()
	}

	return state, nil
}

// ForceCheck performs an immediate sync check and updates the cache.
func (s *Service) ForceCheck(ctx context.Context) (*UpdateState, error) {
	if !s.enabled {
		return &UpdateState{
			Source: "disabled",
		}, nil
	}

	logging.Info("Checking for updates...")

	latest, err := s.checker.CheckLatestVersion(ctx)
	if err != nil {
		logging.Debugf("Update check failed: %v", err)

		// Try to load existing state
		state, loadErr := s.store.LoadState()
		if loadErr == nil && state != nil {
			// Update error in existing state
			state.Error = err.Error()
			state.Source = "cached"
			_ = s.store.SaveState(state)
			return state, nil
		}

		return &UpdateState{
			Source: "error",
			Error:  err.Error(),
		}, nil
	}

	// Check if update is available
	// Use the actual build version from the version package
	currentVersion := version.Short()

	// Compare versions
	isAvailable := CompareVersions(currentVersion, latest.Version) < 0
	isPrerelease := IsPrerelease(latest.Version)

	state := &UpdateState{
		Version:    latest.Version,
		CheckedAt:  NowISO8601(),
		Available:  isAvailable,
		Prerelease: isPrerelease,
		Source:     "fresh",
	}

	// Only save if we have a valid state
	if state.Version != "" {
		if saveErr := s.store.SaveState(state); saveErr != nil {
			logging.Debugf("Failed to save update state: %v", saveErr)
		}
	}

	return state, nil
}

// ShouldCheck determines if a check should be performed.
func (s *Service) ShouldCheck(state *UpdateState) bool {
	if state == nil || state.CheckedAt == "" {
		return true
	}
	return s.store.ShouldCheck()
}

// BackgroundCheck performs a non-blocking update check.
// Uses context.Background() to avoid cancellation when the HTTP request ends.
func (s *Service) BackgroundCheck() {
	// Use a new context with timeout for the check itself
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Prevent concurrent background checks across all Service instances
	globalCheckMutex.Lock()
	defer globalCheckMutex.Unlock()

	state, err := s.ForceCheck(ctx)
	if err != nil {
		logging.Debugf("Background update check failed: %v", err)
		return
	}

	if state.Available {
		logging.Infof("Update available: %s", state.Version)
	} else {
		logging.Debugf("No update available (latest: %s)", state.Version)
	}
}

// StartBackgroundCheck starts a background goroutine for periodic checks.
func (s *Service) StartBackgroundCheck(ctx context.Context, interval time.Duration) {
	if !s.enabled {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logging.Info("Background update check stopped")
				return
			case <-ticker.C:
				s.BackgroundCheck()
			}
		}
	}()
}

// IsUpdateAvailable checks if an update is available without modifying state.
func (s *Service) IsUpdateAvailable(ctx context.Context) (bool, error) {
	state, err := s.GetStatus(ctx)
	if err != nil {
		return false, err
	}
	return state.Available, nil
}

// GetLatestVersion returns the latest version without checking availability.
func (s *Service) GetLatestVersion(ctx context.Context) (string, error) {
	state, err := s.GetStatus(ctx)
	if err != nil {
		return "", err
	}
	return state.Version, nil
}

// FormatUpdateMessage creates a user-friendly message about an update.
func FormatUpdateMessage(current, latest string) string {
	if latest == "" {
		return fmt.Sprintf("Current version: %s", current)
	}

	if CompareVersions(current, latest) >= 0 {
		return fmt.Sprintf("You are running the latest version: %s", current)
	}

	return fmt.Sprintf("Update available: %s (current: %s)", latest, current)
}

// UpdateConfigSection represents the update configuration section.
type UpdateConfigSection struct {
	Enabled                  bool `yaml:"enabled" json:"enabled"`
	UpdateCheckIntervalHours int  `yaml:"check_interval_hours" json:"check_interval_hours"`
}

// DefaultUpdateConfig returns the default update configuration.
func DefaultUpdateConfig() UpdateConfigSection {
	return UpdateConfigSection{
		Enabled:                  true,
		UpdateCheckIntervalHours: 24,
	}
}
