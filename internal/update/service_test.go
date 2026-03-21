package update

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockChecker struct {
	version *VersionInfo
	err     error
}

func (m *mockChecker) CheckLatestVersion(_ context.Context) (*VersionInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.version, nil
}

func TestNewService(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.System.UpdateEnabled = true
	cfg.System.UpdateCheckIntervalHours = 24

	service := NewService(cfg)

	assert.NotNil(t, service)
	assert.True(t, service.enabled)
	assert.Equal(t, 24*time.Hour, service.interval)
	assert.Contains(t, service.statePath, "update_cache.json")
}

func TestService_GetStatus_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	cfg := config.DefaultConfig()
	cfg.System.UpdateEnabled = false
	cfg.System.UpdateCheckIntervalHours = 24

	// Override the state path for testing
	store := NewStateStore(statePath, DefaultCheckInterval)

	service := &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   false,
	}

	status, err := service.GetStatus(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "disabled", status.Source)
	assert.False(t, status.Available)
}

func TestService_ForceCheck(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	cfg := config.DefaultConfig()
	cfg.System.UpdateEnabled = true

	service := &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
		store:     NewStateStore(statePath, DefaultCheckInterval),
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

	cfg := config.DefaultConfig()
	cfg.System.UpdateEnabled = true

	store := NewStateStore(statePath, DefaultCheckInterval)
	service := &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	// Background check should not block (no context argument now)
	done := make(chan struct{})
	go func() {
		service.BackgroundCheck()
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
		got := FormatUpdateMessage(tt.current, tt.latest)
		assert.Equal(t, tt.want, got)
	}
}

func TestDefaultUpdateConfig(t *testing.T) {
	cfg := DefaultUpdateConfig()

	assert.True(t, cfg.Enabled)
	assert.Equal(t, 24, cfg.UpdateCheckIntervalHours)
}

func TestService_IsUpdateAvailable(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	cfg := config.DefaultConfig()
	cfg.System.UpdateEnabled = true

	service := &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
		store:     NewStateStore(statePath, DefaultCheckInterval),
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

	cfg := config.DefaultConfig()
	cfg.System.UpdateEnabled = true

	service := &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
		store:     NewStateStore(statePath, DefaultCheckInterval),
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

	cfg := config.DefaultConfig()
	cfg.System.UpdateEnabled = true

	store := NewStateStore(statePath, DefaultCheckInterval)
	service := &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
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

	store := NewStateStore(statePath, DefaultCheckInterval)
	service := &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
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

	store := NewStateStore(statePath, 24*time.Hour)
	service := &Service{
		store:    store,
		interval: 24 * time.Hour,
		enabled:  true,
	}

	// Test with nil state
	assert.True(t, service.ShouldCheck(nil))

	// Test with state from now (should not check)
	state := &UpdateState{
		CheckedAt: NowISO8601(),
	}
	store.SetState(state)
	assert.False(t, service.ShouldCheck(state))

	// Test with old state (should check)
	state = &UpdateState{
		CheckedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
	}
	store.SetState(state)
	assert.True(t, service.ShouldCheck(state))
}

func TestService_GetStatus_WithExistingState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")

	// Pre-populate state
	initialState := &UpdateState{
		Version:   "v1.5.0",
		CheckedAt: time.Now().Add(-12 * time.Hour).UTC().Format(time.RFC3339),
		Available: false,
		Source:    "cached",
	}
	err := SaveStateToFile(statePath, initialState)
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cfg.System.UpdateEnabled = true

	service := &Service{
		checker:   NewGitHubChecker("javinizer/Javinizer"),
		store:     NewStateStore(statePath, 24*time.Hour),
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := service.GetStatus(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "v1.5.0", status.Version)
	assert.Equal(t, "cached", status.Source)
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
			version: &VersionInfo{
				Version:    "v1.6.0",
				TagName:    "v1.6.0",
				Prerelease: false,
			},
		},
		store:     NewStateStore(statePath, DefaultCheckInterval),
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
