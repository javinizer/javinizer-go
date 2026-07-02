package update

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockChecker struct {
	version *versionInfo
	err     error

	// ConditionalChecker probes — record what the service threaded in.
	gotIfNoneMatch string
	gotSkipLatest  bool
}

func (m *mockChecker) CheckLatestVersion(_ context.Context) (*versionInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.version, nil
}

// SetIfNoneMatch implements ConditionalChecker.
func (m *mockChecker) SetIfNoneMatch(etag string) { m.gotIfNoneMatch = etag }

// SetSkipLatest implements ConditionalChecker.
func (m *mockChecker) SetSkipLatest(skip bool) { m.gotSkipLatest = skip }

func TestNewService(t *testing.T) {
	service := NewService(UpdateConfig{
		Enabled:                   true,
		VersionCheckIntervalHours: 24,
	})

	assert.NotNil(t, service)
	assert.True(t, service.enabled)
	assert.Equal(t, 24*time.Hour, service.interval)
	assert.Contains(t, service.statePath, "update_cache.json")
}

func TestService_GetStatus_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	// Override the state path for testing
	store := newStateStore(statePath, defaultCheckInterval)

	service := &Service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   false,
	}

	status, err := service.GetStatus(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, UpdateSourceDisabled, status.Source)
	assert.False(t, status.Available)
}

func TestService_ForceCheck(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	service := &Service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     newStateStore(statePath, defaultCheckInterval),
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	// Force check - may succeed or fail depending on network
	status, err := service.ForceCheck(context.Background())

	// Should not return an error, even if network fails
	assert.NoError(t, err)

	// Verify state was saved
	assert.NotEmpty(t, status.Source)

	// If we have a version, it should be populated
	if status.Version != "" {
		assert.NotEmpty(t, status.CheckedAt)
	}
}

func TestService_BackgroundCheck(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	store := newStateStore(statePath, defaultCheckInterval)
	service := &Service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	// Background check should not block (no context argument now)
	done := make(chan struct{})
	go func() {
		service.BackgroundCheck(context.Background())
		close(done)
	}()

	select {
	case <-done:
		// Success - check completed
	case <-time.After(10 * time.Second):
		t.Fatal("Background check did not complete within timeout")
	}
}

func TestFormatUpdateMessage(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    string
	}{
		{"v1.5.0", "v1.6.0", "Update available: v1.6.0 (current: v1.5.0)"},
		{"v1.6.0", "v1.6.0", "You are running the latest version: v1.6.0"},
		{"v1.7.0", "v1.6.0", "You are running the latest version: v1.7.0"},
		{"v1.5.0", "", "Current version: v1.5.0"},
	}

	for _, tt := range tests {
		got := formatUpdateMessage(tt.current, tt.latest)
		assert.Equal(t, tt.want, got)
	}
}

func TestService_IsUpdateAvailable(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	service := &Service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     newStateStore(statePath, defaultCheckInterval),
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	// Test with disabled service
	service.enabled = false
	available, err := service.IsUpdateAvailable(context.Background())
	assert.NoError(t, err)
	assert.False(t, available)
}

func TestService_GetLatestVersion(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	service := &Service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     newStateStore(statePath, defaultCheckInterval),
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	latestVersion, err := service.GetLatestVersion(context.Background())
	// May fail if GitHub API is unavailable, but should not panic
	if err != nil {
		// Expected in test environment without network access
		t.Logf("GetLatestVersion returned expected error: %v", err)
		assert.Contains(t, []string{"none", "error", "disabled"}, err.Error()[:5])
	} else {
		// If version was returned, it should not be empty
		if latestVersion != "" {
			assert.NotEmpty(t, latestVersion)
		}
	}
}

func TestService_StartBackgroundCheck(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	store := newStateStore(statePath, defaultCheckInterval)
	service := &Service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start background check
	service.StartBackgroundCheck(ctx, 1*time.Second)

	// Wait a bit to let it run
	time.Sleep(1500 * time.Millisecond)

	// Verify state was updated (or at least tried)
	state, _ := store.LoadState()
	if state != nil {
		assert.NotEmpty(t, state.Source)
	}
}

func TestService_StartBackgroundCheck_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	store := newStateStore(statePath, defaultCheckInterval)
	service := &Service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start background check on disabled service - should not run
	service.StartBackgroundCheck(ctx, 1*time.Second)

	// Wait and verify state was not modified
	time.Sleep(1500 * time.Millisecond)

	state, _ := store.LoadState()
	assert.Nil(t, state, "Disabled service should not modify state")
}

func TestService_ShouldCheck(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	store := newStateStore(statePath, 24*time.Hour)
	service := &Service{
		store:    store,
		interval: 24 * time.Hour,
		enabled:  true,
	}

	// Test with nil state
	assert.True(t, service.ShouldCheck(nil))

	// Test with state from now (should not check)
	state := &updateState{
		CheckedAt: nowISO8601(),
	}
	store.SetState(state)
	assert.False(t, service.ShouldCheck(state))

	// Test with old state (should check)
	state = &updateState{
		CheckedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
	}
	store.SetState(state)
	assert.True(t, service.ShouldCheck(state))
}

func TestService_GetStatus_WithExistingState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	// Pre-populate state
	initialState := &updateState{
		Version:   "v1.5.0",
		CheckedAt: time.Now().Add(-12 * time.Hour).UTC().Format(time.RFC3339),
		Available: false,
		Source:    UpdateSourceCached,
	}
	err := saveStateToFile(afero.NewOsFs(), statePath, initialState)
	require.NoError(t, err)

	service := &Service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     newStateStore(statePath, 24*time.Hour),
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := service.GetStatus(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "v1.5.0", status.Version)
	assert.Equal(t, UpdateSourceCached, status.Source)
}

func TestService_ForceCheck_PrereleaseToStableIsAvailable(t *testing.T) {
	origVersion := appversion.Version
	defer func() {
		appversion.Version = origVersion
	}()
	appversion.Version = "v1.6.0-rc1"

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	service := &Service{
		checker: &mockChecker{
			version: &versionInfo{
				Version:    "v1.6.0",
				TagName:    "v1.6.0",
				Prerelease: false,
			},
		},
		store:     newStateStore(statePath, defaultCheckInterval),
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := service.ForceCheck(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "v1.6.0", status.Version)
	assert.True(t, status.Available)
	assert.False(t, status.Prerelease)
}

// TestService_ForceCheck_StableOnlyConfig verifies the service honors the
// config-driven stableOnly flag when the latest release is a prerelease:
// with stableOnly=false (the default) the prerelease is surfaced as an
// available update; with stableOnly=true the version is still cached (for
// transparency) but Available is suppressed so the user isn't notified about a
// prerelease.
func TestService_ForceCheck_StableOnlyConfig(t *testing.T) {
	origVersion := appversion.Version
	defer func() { appversion.Version = origVersion }()
	appversion.Version = "v0.3.14-alpha"

	mkService := func(stableOnly bool) *Service {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "update_cache.json")
		return &Service{
			checker: &mockChecker{
				version: &versionInfo{
					Version:    "v0.3.15-alpha",
					TagName:    "v0.3.15-alpha",
					Prerelease: true,
				},
			},
			store:      newStateStore(statePath, defaultCheckInterval),
			statePath:  statePath,
			interval:   24 * time.Hour,
			enabled:    true,
			stableOnly: stableOnly,
		}
	}

	t.Run("stableOnly=false surfaces prerelease update", func(t *testing.T) {
		svc := mkService(false)
		status, err := svc.ForceCheck(context.Background())
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, "v0.3.15-alpha", status.Version)
		assert.True(t, status.Available, "prerelease should be offered when stableOnly=false")
		assert.True(t, status.Prerelease)
	})

	t.Run("stableOnly=true suppresses prerelease update", func(t *testing.T) {
		svc := mkService(true)
		status, err := svc.ForceCheck(context.Background())
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, "v0.3.15-alpha", status.Version, "version still cached for transparency")
		assert.False(t, status.Available, "prerelease must NOT be offered when stableOnly=true")
		assert.True(t, status.Prerelease)
	})
}

// TestService_ForceCheck_NotModifiedKeepsCachedState verifies the 304 path:
// when the checker reports ErrNotModified (GitHub returned 304 for our
// If-None-Match), the service keeps the cached version/availability and only
// refreshes CheckedAt. This is the rate-limit-free steady state for a
// frequently-restarted instance.
func TestService_ForceCheck_NotModifiedKeepsCachedState(t *testing.T) {
	origVersion := appversion.Version
	defer func() { appversion.Version = origVersion }()
	appversion.Version = "v0.3.14-alpha"

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")
	store := newStateStore(statePath, defaultCheckInterval)

	// Prime the cache with a prior successful check (including the ETag +
	// no-stable-latest flag a real check would have persisted).
	prior := &updateState{
		Version:        "v0.3.15-alpha",
		CheckedAt:      "2026-01-01T00:00:00Z",
		Available:      true,
		Prerelease:     true,
		Source:         UpdateSourceFresh,
		ETag:           `W/"deadbeef"`,
		NoStableLatest: true,
	}
	require.NoError(t, store.SaveState(prior))

	chk := &mockChecker{err: ErrNotModified}
	svc := &Service{
		checker:    chk,
		store:      store,
		statePath:  statePath,
		interval:   24 * time.Hour,
		enabled:    true,
		stableOnly: false,
	}

	status, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)

	// Cached version is preserved; availability is RE-EVALUATED under the
	// current stableOnly policy (here stableOnly=false, so the prerelease stays
	// available — the re-evaluation reproduces the cached value).
	assert.Equal(t, "v0.3.15-alpha", status.Version)
	assert.True(t, status.Available, "prerelease stays available with stableOnly=false")
	assert.True(t, status.Prerelease)
	// CheckedAt is refreshed; the stale timestamp must be gone.
	assert.NotEqual(t, "2026-01-01T00:00:00Z", status.CheckedAt)
	assert.NotEmpty(t, status.CheckedAt)
	// Source flips to cached (not fresh) since we served from the 304 path.
	assert.Equal(t, UpdateSourceCached, status.Source)
	assert.Empty(t, status.Error)

	// The service threaded the cached ETag + no-stable-latest flag into the
	// checker so the real request would have been rate-limit-friendly.
	assert.Equal(t, `W/"deadbeef"`, chk.gotIfNoneMatch)
	assert.True(t, chk.gotSkipLatest, "skipLatest threaded from cached NoStableLatest")

	// The persisted state carries the ETag + flag forward for the next check.
	reloaded, err := store.LoadState()
	require.NoError(t, err)
	assert.Equal(t, `W/"deadbeef"`, reloaded.ETag)
	assert.True(t, reloaded.NoStableLatest)
}

// TestService_ForceCheck_NotModifiedWithoutCache verifies the edge case where
// a 304 arrives but there is no prior cached state to reuse: the service
// reports an error state rather than fabricating version data.
func TestService_ForceCheck_NotModifiedWithoutCache(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")
	store := newStateStore(statePath, defaultCheckInterval) // empty cache

	chk := &mockChecker{err: ErrNotModified}
	svc := &Service{
		checker:   chk,
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, UpdateSourceError, status.Source)
	assert.Empty(t, status.Version, "no version fabricated from a 304 with no cache")
}

// TestService_ForceCheck_NotModifiedReEvaluatesStableOnly is the regression
// test for the bug CodeRabbit flagged on the 304 path: a prerelease previously
// cached as Available=true must be suppressed on a 304 once the user enables
// version_check_stable_only. The releases are unchanged (304), but the policy
// term changed, so availability is re-derived rather than carried over.
func TestService_ForceCheck_NotModifiedReEvaluatesStableOnly(t *testing.T) {
	origVersion := appversion.Version
	defer func() { appversion.Version = origVersion }()
	appversion.Version = "v0.3.14-alpha"

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")
	store := newStateStore(statePath, defaultCheckInterval)

	// Prior check ran with stableOnly=false, so a prerelease was cached as
	// available — the state the bug would blindly preserve.
	prior := &updateState{
		Version:    "v0.3.15-alpha",
		CheckedAt:  "2026-01-01T00:00:00Z",
		Available:  true,
		Prerelease: true,
		Source:     UpdateSourceFresh,
		ETag:       `W/"deadbeef"`,
	}
	require.NoError(t, store.SaveState(prior))

	chk := &mockChecker{err: ErrNotModified}
	svc := &Service{
		checker:    chk,
		store:      store,
		statePath:  statePath,
		interval:   24 * time.Hour,
		enabled:    true,
		stableOnly: true, // user toggled stable-only ON since the prior check
	}

	status, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)

	// Version + prerelease flag preserved (the 304 means releases unchanged).
	assert.Equal(t, "v0.3.15-alpha", status.Version)
	assert.True(t, status.Prerelease)
	// Available is RE-EVALUATED and suppressed under the new stableOnly policy.
	assert.False(t, status.Available, "prerelease must be suppressed on 304 once stableOnly is enabled")
	assert.Equal(t, UpdateSourceCached, status.Source)

	// The suppression persists into the on-disk cache for subsequent reads.
	reloaded, err := store.LoadState()
	require.NoError(t, err)
	assert.False(t, reloaded.Available)
}

// TestService_ForceCheck_NotModifiedReEnablesWhenStableOnlyCleared verifies
// the inverse of the stableOnly re-evaluation: if a prerelease was cached with
// Available=false (because stableOnly was on) and the user later disables it,
// a 304 re-evaluates availability back to true.
func TestService_ForceCheck_NotModifiedReEnablesWhenStableOnlyCleared(t *testing.T) {
	origVersion := appversion.Version
	defer func() { appversion.Version = origVersion }()
	appversion.Version = "v0.3.14-alpha"

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")
	store := newStateStore(statePath, defaultCheckInterval)

	prior := &updateState{
		Version:    "v0.3.15-alpha",
		CheckedAt:  "2026-01-01T00:00:00Z",
		Available:  false, // suppressed under the old stableOnly=true policy
		Prerelease: true,
		Source:     UpdateSourceFresh,
		ETag:       `W/"deadbeef"`,
	}
	require.NoError(t, store.SaveState(prior))

	chk := &mockChecker{err: ErrNotModified}
	svc := &Service{
		checker:    chk,
		store:      store,
		statePath:  statePath,
		interval:   24 * time.Hour,
		enabled:    true,
		stableOnly: false, // user disabled stable-only since the prior check
	}

	status, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Available, "prerelease re-surfaced once stableOnly is cleared")
	assert.True(t, status.Prerelease)
}

// TestService_ForceCheck_ResetsConditionalHintsWhenCacheEmpty verifies the
// sticky-state fix on the ConditionalChecker path: the githubChecker is shared
// across checks, so if the on-disk cache is absent (fresh install, deleted
// cache file), the service must reset the ETag/skipLatest hints to empty/false
// rather than leaving a prior check's values in place — otherwise a stale
// If-None-Match or an unintended /releases/latest skip would leak forward.
func TestService_ForceCheck_ResetsConditionalHintsWhenCacheEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")
	store := newStateStore(statePath, defaultCheckInterval) // empty cache

	chk := &mockChecker{
		version: &versionInfo{Version: "v1.0.0", TagName: "v1.0.0"},
		// Simulate stale state left by a prior check against a different cache.
		gotIfNoneMatch: `W/"stale-from-prior-check"`,
		gotSkipLatest:  true,
	}
	svc := &Service{
		checker:   chk,
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	_, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)

	// With no cache, both hints must be reset to empty/false — not left sticky.
	assert.Empty(t, chk.gotIfNoneMatch, "stale ETag must be reset when cache is empty")
	assert.False(t, chk.gotSkipLatest, "stale skipLatest must be reset when cache is empty")
}
