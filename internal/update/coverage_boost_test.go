package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_ForceCheck_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := map[string]any{"tag_name": "v99.0.0", "prerelease": false}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "update_cache.json")
	store := newStateStore(statePath, defaultCheckInterval)

	chk := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	svc := &service{
		checker:   chk,
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	assert.Equal(t, UpdateSourceFresh, status.Source)
	assert.Equal(t, "v99.0.0", status.Version)
}

func TestService_ForceCheck_DisabledCoverage(t *testing.T) {
	tmpDir := t.TempDir()
	svc := &service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     newStateStore(filepath.Join(tmpDir, "state.json"), defaultCheckInterval),
		statePath: filepath.Join(tmpDir, "state.json"),
		interval:  24 * time.Hour,
		enabled:   false,
	}

	status, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	assert.Equal(t, UpdateSourceDisabled, status.Source)
}

func TestService_ForceCheck_ErrorWithCache(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Pre-populate state
	store := newStateStore(statePath, defaultCheckInterval)
	store.SetState(&updateState{Version: "v1.0.0", CheckedAt: time.Now().UTC().Format(time.RFC3339), Source: UpdateSourceCached})

	// Use a server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	svc := &service{
		checker:   newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	// Should return cached state with error set
	assert.Equal(t, UpdateSourceCached, status.Source)
}

func TestService_ForceCheck_ErrorNoCache(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	svc := &service{
		checker:   newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL),
		store:     newStateStore(statePath, defaultCheckInterval),
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	assert.Equal(t, UpdateSourceError, status.Source)
}

func TestService_GetStatus_CachedState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := newStateStore(statePath, defaultCheckInterval)
	store.SetState(&updateState{Version: "v1.0.0", CheckedAt: time.Now().UTC().Format(time.RFC3339), Available: true, Source: UpdateSourceFresh})

	svc := &service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", status.Version)
	assert.True(t, status.Available)
}

func TestService_GetStatus_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := newStateStore(statePath, defaultCheckInterval)

	svc := &service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, UpdateSourceNone, status.Source)
}

func TestService_GetStatus_StaleTriggersBg(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := newStateStore(statePath, defaultCheckInterval)
	// Set old state that is stale
	oldTime := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	store.SetState(&updateState{Version: "v0.9.0", CheckedAt: oldTime, Source: UpdateSourceCached})

	svc := &service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v0.9.0", status.Version)
	// Background check triggered — give it a moment
	time.Sleep(100 * time.Millisecond)
}

func TestService_IsUpdateAvailable_Cached(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := newStateStore(statePath, defaultCheckInterval)
	store.SetState(&updateState{Version: "v2.0.0", Available: true, CheckedAt: time.Now().UTC().Format(time.RFC3339), Source: UpdateSourceFresh})

	svc := &service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	available, err := svc.IsUpdateAvailable(context.Background())
	require.NoError(t, err)
	assert.True(t, available)
}

func TestService_GetLatestVersion_Cached(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := newStateStore(statePath, defaultCheckInterval)
	store.SetState(&updateState{Version: "v3.0.0", CheckedAt: time.Now().UTC().Format(time.RFC3339), Source: UpdateSourceFresh})

	svc := &service{
		checker:   newGitHubChecker("javinizer/Javinizer"),
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	version, err := svc.GetLatestVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v3.0.0", version)
}

func TestService_BackgroundCheck_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := map[string]any{"tag_name": "v5.0.0", "prerelease": false}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := newStateStore(statePath, defaultCheckInterval)

	chk := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	svc := &service{
		checker:   chk,
		store:     store,
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	svc.BackgroundCheck(context.Background())
	// Verify state was saved
	state := store.GetState()
	require.NotNil(t, state)
	assert.Equal(t, "v5.0.0", state.Version)
}

func TestService_StartBgCheck_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := map[string]any{"tag_name": "v1.0.0", "prerelease": false}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	svc := &service{
		checker:   newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL),
		store:     newStateStore(statePath, defaultCheckInterval),
		statePath: statePath,
		interval:  24 * time.Hour,
		enabled:   true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartBackgroundCheck(ctx, 50*time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestCompareVersions_LegacyComparison(t *testing.T) {
	// Non-semver strings fall back to legacy comparison
	assert.Equal(t, -1, CompareVersions("1.0.0", "2.0.0"))
	assert.Equal(t, 1, CompareVersions("2.0.0", "1.0.0"))
	assert.Equal(t, 0, CompareVersions("1.0.0", "1.0.0"))
}

func TestCompareVersions_PrereleaseVsStable(t *testing.T) {
	// Stable release is newer than prerelease of same version
	assert.Equal(t, -1, CompareVersions("1.0.0-rc1", "1.0.0"))
	assert.Equal(t, 1, CompareVersions("1.0.0", "1.0.0-rc1"))
}
