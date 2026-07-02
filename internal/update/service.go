package update

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/version"
)

// Service handles update checking with caching and background refresh.
type Service struct {
	checker    checker
	store      *stateStore
	statePath  string
	interval   time.Duration
	enabled    bool
	stableOnly bool       // honor system.version_check_stable_only: when true, never surface a prerelease as an available update
	checkMu    sync.Mutex // prevents concurrent background checks
}

// UpdateConfig carries the narrow set of config fields the update service reads.
type UpdateConfig struct {
	Enabled                   bool
	VersionCheckIntervalHours int
	// StableOnly, when true, restricts update notifications to stable releases
	// only (prereleases are still fetched and cached for transparency but never
	// reported as available). Defaults to false: the Go repo currently ships
	// only prereleases, so suppressing them by default would mean the checker
	// never notifies any user until a stable release exists.
	StableOnly bool
}

// NewService creates a new update service with production defaults (the real
// GitHub checker and the default cache path). Existing callers are unaffected.
func NewService(cfg UpdateConfig) *Service {
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
func NewServiceWithOptions(cfg UpdateConfig, opts ServiceOptions) *Service {
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
		chk = newGitHubChecker(defaultRepo)
	}

	return &Service{
		checker:    chk,
		store:      store,
		statePath:  statePath,
		interval:   interval,
		enabled:    cfg.Enabled,
		stableOnly: cfg.StableOnly,
	}
}

// Interval returns the normalized interval between background update checks.
// Exposed so callers (e.g. the API bootstrap) can start the background ticker
// with the same interval the service uses for staleness checks, rather than
// re-deriving (and possibly diverging from) the default-interval logic.
func (s *Service) Interval() time.Duration {
	return s.interval
}

// GetStatus returns the current update status.
// If the cached state is stale, it performs a background check.
func (s *Service) GetStatus(ctx context.Context) (*updateState, error) {
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
//
// Serialized via s.checkMu so the checker's conditional-request hints (ETag /
// skipLatest) cannot be mutated by a concurrent check — the API version
// endpoint and BackgroundCheck both flow through here. BackgroundCheck already
// holds checkMu and calls forceCheckLocked directly to avoid a non-reentrant
// deadlock.
func (s *Service) ForceCheck(ctx context.Context) (*updateState, error) {
	if !s.enabled {
		return &updateState{
			Source: UpdateSourceDisabled,
		}, nil
	}
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	return s.forceCheckLocked(ctx), nil
}

// forceCheckLocked is the ForceCheck body; the caller MUST hold s.checkMu.
// It returns only the state — every error is translated into the state's
// Error/Source fields rather than a Go error, so callers always get a usable
// state (never a nil + error pair). ForceCheck wraps it to add the disabled
// short-circuit and the (always-nil here) error return for API compatibility.
func (s *Service) forceCheckLocked(ctx context.Context) *updateState {
	logging.Info("Checking for updates...")

	// Thread the cached ETag + "no-stable-latest" flag into the checker so this
	// check is rate-limit-friendly: the ETag becomes If-None-Match (GitHub 304s
	// don't count against quota) and skipLatest avoids the 404-throwing
	// /releases/latest call for prerelease-only repos. Stub checkers don't
	// implement ConditionalChecker — the assert fails silently and a full fetch
	// runs, which is correct for hermetic tests.
	//
	// ALWAYS (re)set both hints, even when the cache is empty: the githubChecker
	// is shared across checks, so leftover ifNoneMatch/skipLatest from a prior
	// check (e.g. after the cache file is deleted between checks) would otherwise
	// leak forward as a stale If-None-Match or an unintended /releases/latest
	// skip. Empty/false yields a full fetch — the correct behavior with no cache.
	cached, _ := s.store.LoadState()
	if cc, ok := s.checker.(ConditionalChecker); ok {
		etag, skipLatest := "", false
		if cached != nil {
			etag, skipLatest = cached.ETag, cached.NoStableLatest
		}
		cc.SetIfNoneMatch(etag)
		cc.SetSkipLatest(skipLatest)
	}

	latest, err := s.checker.CheckLatestVersion(ctx)
	if err != nil {
		// 304 Not Modified: releases unchanged since the last check. Keep the
		// cached version, but re-evaluate availability under the CURRENT
		// stable-only policy — the user may have toggled version_check_stable_only
		// since this state was cached, so a previously-surfaced prerelease must
		// now be suppressed (and vice versa). The build version is constant for
		// the process, so only the stableOnly term can change. Only CheckedAt is
		// refreshed; this path burns no rate-limit quota, so it's the steady
		// state for a long-running or frequently restarted instance.
		if errors.Is(err, ErrNotModified) {
			if cached != nil && cached.Version != "" {
				cached.CheckedAt = nowISO8601()
				cached.Source = UpdateSourceCached
				cached.Error = ""
				cached.Available = CompareVersions(version.Short(), cached.Version) < 0 &&
					(!cached.Prerelease || !s.stableOnly)
				_ = s.store.SaveState(cached)
				logging.Debugf("Update check: not modified (304), keeping cached state")
				return cached
			}
			// No cached state to reuse — fall through to the error path below.
			return &updateState{
				Source: UpdateSourceError,
				Error:  "not modified but no cached state available",
			}
		}

		logging.Debugf("Update check failed: %v", err)

		// Try to load existing state
		state, loadErr := s.store.LoadState()
		if loadErr == nil && state != nil {
			// Update error in existing state
			state.Error = err.Error()
			state.Source = UpdateSourceCached
			_ = s.store.SaveState(state)
			return state
		}

		return &updateState{
			Source: UpdateSourceError,
			Error:  err.Error(),
		}
	}

	// Check if update is available
	// Use the actual build version from the version package
	currentVersion := version.Short()

	// Compare versions
	isAvailable := CompareVersions(currentVersion, latest.Version) < 0
	// Treat a release as a prerelease if GitHub flagged it as one OR its tag
	// looks like a prerelease. The GitHub flag is authoritative for the
	// /releases/latest path; the tag heuristic covers the list-fallback path
	// (which sets latest.Prerelease from the tag) and catches releases marked
	// prerelease but tagged like a stable version.
	isPrerelease := latest.Prerelease || IsPrerelease(latest.Version)

	// Honor the user's stable-only preference: when restricting to stable
	// releases, never surface a prerelease as an available update (the Go repo
	// currently ships only prereleases, so this means "no update" until a
	// stable release lands — by design, not a bug). The version is still cached
	// so the UI can show what was found; only the Available flag is suppressed.
	if isAvailable && isPrerelease && s.stableOnly {
		isAvailable = false
	}

	state := &updateState{
		Version:        latest.Version,
		CheckedAt:      nowISO8601(),
		Available:      isAvailable,
		Prerelease:     isPrerelease,
		Source:         UpdateSourceFresh,
		ETag:           latest.ETag,
		NoStableLatest: latest.NoStableLatest,
	}

	// Only save if we have a valid state
	if state.Version != "" {
		if saveErr := s.store.SaveState(state); saveErr != nil {
			logging.Debugf("Failed to save update state: %v", saveErr)
		}
	}

	return state
}

// ShouldCheck determines if a check should be performed.
func (s *Service) ShouldCheck(state *updateState) bool {
	if state == nil || state.CheckedAt == "" {
		return true
	}
	return s.store.ShouldCheck()
}

// BackgroundCheck performs a non-blocking update check.
// Uses caller-provided context for proper cancellation/timeout propagation.
func (s *Service) BackgroundCheck(ctx context.Context) {
	// Use a new context with timeout for the check itself
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Prevent concurrent background checks across all service instances.
	// ForceCheck acquires this lock itself; call forceCheckLocked here to avoid
	// a non-reentrant deadlock.
	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	state := s.forceCheckLocked(ctx)

	if state.Source == UpdateSourceError {
		logging.Debugf("Update check failed: %s", state.Error)
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
