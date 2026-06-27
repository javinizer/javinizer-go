package update

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/version"
)

// service handles update checking with caching and background refresh.
type service struct {
	checker   checker
	store     *stateStore
	statePath string
	interval  time.Duration
	enabled   bool
	checkMu   sync.Mutex // prevents concurrent background checks
}

// UpdateConfig carries the narrow set of config fields the update service reads.
type UpdateConfig struct {
	Enabled                   bool
	VersionCheckIntervalHours int
}

// NewService creates a new update service with production defaults (the real
// GitHub checker and the default cache path). Existing callers are unaffected.
func NewService(cfg UpdateConfig) *service {
	return NewServiceWithOptions(cfg, ServiceOptions{})
}

// ServiceOptions carries optional overrides for the update service, enabling
// hermetic tests (a stub Checker and a temp-dir cache path). Zero-value fields
// fall back to production defaults, so NewService(cfg) and
// NewServiceWithOptions(cfg, ServiceOptions{}) are equivalent.
type ServiceOptions struct {
	// Checker, if non-nil, replaces the default GitHub release checker. Inject
	// a stub to avoid real network calls in tests.
	Checker Checker
	// StatePath overrides the on-disk update cache location. Use t.TempDir() in
	// tests to avoid writing to the real data directory.
	StatePath string
}

// NewServiceWithOptions creates an update service with the given options. A
// zero-value opts reproduces NewService behavior exactly, so production callers
// can keep using NewService while tests inject a stub Checker and a temp-dir
// StatePath for hermetic, network-free coverage.
func NewServiceWithOptions(cfg UpdateConfig, opts ServiceOptions) *service {
	interval := time.Duration(cfg.VersionCheckIntervalHours) * time.Hour
	if interval <= 0 {
		interval = defaultCheckInterval
	}

	statePath := opts.StatePath
	if statePath == "" {
		statePath = updateStatePath()
	}

	store := newStateStore(statePath, interval)

	chk := opts.Checker
	if chk == nil {
		chk = newGitHubChecker("javinizer/Javinizer")
	}

	return &service{
		checker:   chk,
		store:     store,
		statePath: statePath,
		interval:  interval,
		enabled:   cfg.Enabled,
	}
}

// Interval returns the normalized interval between background update checks.
// Exposed so callers (e.g. the API bootstrap) can start the background ticker
// with the same interval the service uses for staleness checks, rather than
// re-deriving (and possibly diverging from) the default-interval logic.
func (s *service) Interval() time.Duration {
	return s.interval
}

// GetStatus returns the current update status.
// If the cached state is stale, it performs a background check.
func (s *service) GetStatus(ctx context.Context) (*updateState, error) {
	if !s.enabled {
		return &updateState{
			Source: UpdateSourceDisabled,
		}, nil
	}

	state, err := s.store.LoadState()
	if err != nil {
		logging.Debugf("Failed to load update state: %v", err)
		// Return empty state rather than failing
		return &updateState{
			Source: UpdateSourceError,
			Error:  err.Error(),
		}, nil
	}

	// If no state exists, return empty. (A background refresh is intentionally
	// NOT triggered here: NewService hardcodes the real GitHub checker with no
	// injection seam, so firing a check on this read path would make hermetic
	// CI tests hit the network and write a shared on-disk cache. The stale-state
	// path below still triggers a refresh for non-empty caches.)
	if state == nil {
		return &updateState{
			Source: UpdateSourceNone,
		}, nil
	}

	// If state is stale and we should check, do it in background
	if s.store.ShouldCheck() {
		// Use Background() so the check outlives the request — intentional, not a leak
		go s.BackgroundCheck(context.Background())
	}

	return state, nil
}

// ForceCheck performs an immediate sync check and updates the cache.
func (s *service) ForceCheck(ctx context.Context) (*updateState, error) {
	if !s.enabled {
		return &updateState{
			Source: UpdateSourceDisabled,
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
			state.Source = UpdateSourceCached
			_ = s.store.SaveState(state)
			return state, nil
		}

		return &updateState{
			Source: UpdateSourceError,
			Error:  err.Error(),
		}, nil
	}

	// Check if update is available
	// Use the actual build version from the version package
	currentVersion := version.Short()

	// Compare versions
	isAvailable := CompareVersions(currentVersion, latest.Version) < 0
	isPrerelease := IsPrerelease(latest.Version)

	state := &updateState{
		Version:    latest.Version,
		CheckedAt:  nowISO8601(),
		Available:  isAvailable,
		Prerelease: isPrerelease,
		Source:     UpdateSourceFresh,
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
func (s *service) ShouldCheck(state *updateState) bool {
	if state == nil || state.CheckedAt == "" {
		return true
	}
	return s.store.ShouldCheck()
}

// BackgroundCheck performs a non-blocking update check.
// Uses caller-provided context for proper cancellation/timeout propagation.
func (s *service) BackgroundCheck(ctx context.Context) {
	// Use a new context with timeout for the check itself
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Prevent concurrent background checks across all service instances
	s.checkMu.Lock()
	defer s.checkMu.Unlock()

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
func (s *service) StartBackgroundCheck(ctx context.Context, interval time.Duration) {
	if !s.enabled {
		return
	}

	// Normalize non-positive intervals before creating the ticker, otherwise
	// time.NewTicker panics and the goroutine dies (the recover only logs after
	// the goroutine is already lost).
	if interval <= 0 {
		interval = defaultCheckInterval
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := panicutil.FormatRecover(r)
				logging.Errorf("Update background check goroutine %v", err)
			}
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logging.Info("Background update check stopped")
				return
			case <-ticker.C:
				s.BackgroundCheck(ctx)
			}
		}
	}()
}

// IsUpdateAvailable checks if an update is available without modifying state.
func (s *service) IsUpdateAvailable(ctx context.Context) (bool, error) {
	state, err := s.GetStatus(ctx)
	if err != nil {
		return false, err
	}
	return state.Available, nil
}

// GetLatestVersion returns the latest version without checking availability.
func (s *service) GetLatestVersion(ctx context.Context) (string, error) {
	state, err := s.GetStatus(ctx)
	if err != nil {
		return "", err
	}
	return state.Version, nil
}

// formatUpdateMessage creates a user-friendly message about an update.
func formatUpdateMessage(current, latest string) string {
	if latest == "" {
		return fmt.Sprintf("Current version: %s", current)
	}

	if CompareVersions(current, latest) >= 0 {
		return fmt.Sprintf("You are running the latest version: %s", current)
	}

	return fmt.Sprintf("Update available: %s (current: %s)", latest, current)
}
