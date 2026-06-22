package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- checker.go uncovered ---

func TestParseGitHubReleaseVersion_MoreCases_Uncovered(t *testing.T) {
	t.Run("version without v prefix", func(t *testing.T) {
		info, err := parseGitHubReleaseVersion("1.2.3")
		require.NoError(t, err)
		assert.Equal(t, "v1.2.3", info.Version)
		assert.False(t, info.Prerelease)
	})

	t.Run("prerelease with hyphen", func(t *testing.T) {
		info, err := parseGitHubReleaseVersion("v2.0.0-rc1")
		require.NoError(t, err)
		assert.True(t, info.Prerelease)
	})

	t.Run("invalid version format", func(t *testing.T) {
		_, err := parseGitHubReleaseVersion("not-a-version")
		assert.Error(t, err)
	})
}

func TestCompareVersions_MoreCases_Uncovered(t *testing.T) {
	t.Run("equal versions", func(t *testing.T) {
		assert.Equal(t, 0, CompareVersions("v1.0.0", "v1.0.0"))
	})

	t.Run("prerelease is less than stable", func(t *testing.T) {
		assert.Equal(t, -1, CompareVersions("1.0.0-rc1", "1.0.0"))
	})

	t.Run("stable is greater than prerelease", func(t *testing.T) {
		assert.Equal(t, 1, CompareVersions("1.0.0", "1.0.0-rc1"))
	})

	t.Run("legacy comparison with fewer parts", func(t *testing.T) {
		// Non-semver values fall through to legacy
		assert.Equal(t, 0, CompareVersions("1.0", "1.0"))
	})
}

func TestNormalizeSemver_Uncovered(t *testing.T) {
	assert.Equal(t, "v1.0.0", normalizeSemver("1.0.0"))
	assert.Equal(t, "v1.0.0", normalizeSemver("v1.0.0"))
	assert.Equal(t, "", normalizeSemver(""))
	// normalizeSemver trims whitespace
	assert.Equal(t, "v1.0.0", normalizeSemver("  v1.0.0"))
}

func TestParseInt_Uncovered(t *testing.T) {
	n, err := parseInt("123")
	require.NoError(t, err)
	assert.Equal(t, 123, n)

	n, err = parseInt("1-rc1")
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	_, err = parseInt("abc")
	assert.Error(t, err)
}

func TestParseStringToInt_Uncovered(t *testing.T) {
	n, err := parseStringToInt("42")
	require.NoError(t, err)
	assert.Equal(t, 42, n)
}

func TestNewGitHubCheckerWithBaseURL_Uncovered(t *testing.T) {
	c := newGitHubCheckerWithBaseURL("owner/repo", "http://localhost:1234")
	assert.Equal(t, "http://localhost:1234", c.apiBaseURL)
	assert.Equal(t, "owner/repo", c.repo)
}

func TestVersionInfo_JSON_Uncovered(t *testing.T) {
	vi := versionInfo{
		Version:     "v1.0.0",
		TagName:     "v1.0.0",
		Prerelease:  false,
		PublishedAt: "2024-01-01T00:00:00Z",
	}
	data, err := json.Marshal(vi)
	require.NoError(t, err)
	assert.Contains(t, string(data), "v1.0.0")

	var parsed versionInfo
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, vi.Version, parsed.Version)
}

// --- state.go uncovered ---

func TestStateStore_SetState_GetState_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")
	store := newStateStore(path, 24*time.Hour)

	state := &updateState{
		Version:   "v2.0.0",
		CheckedAt: nowISO8601(),
		Available: true,
		Source:    UpdateSourceFresh,
	}

	store.SetState(state)

	got := store.GetState()
	require.NotNil(t, got)
	assert.Equal(t, "v2.0.0", got.Version)
	assert.True(t, got.Available)
}

func TestStateStore_GetState_Nil_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")
	store := newStateStore(path, 24*time.Hour)

	assert.Nil(t, store.GetState())
}

func TestStateStore_ClearState_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	state := &updateState{Version: "v1.0.0", CheckedAt: nowISO8601(), Source: UpdateSourceFresh}
	require.NoError(t, saveStateToFile(afero.NewOsFs(), path, state))

	store := newStateStore(path, 24*time.Hour)
	// Load first so state is cached
	_, err := store.LoadState()
	require.NoError(t, err)

	require.NoError(t, store.ClearState())
	assert.Nil(t, store.GetState())
}

func TestWriteTestState_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test_state.json")

	require.NoError(t, writeTestState(path, "v1.0.0", nowISO8601(), true, false, UpdateSourceFresh))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var state updateState
	require.NoError(t, json.Unmarshal(data, &state))
	assert.Equal(t, "v1.0.0", state.Version)
	assert.True(t, state.Available)
}

func TestWriteTestStateWithError_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test_state_err.json")

	require.NoError(t, writeTestStateWithError(path, "v1.0.0", nowISO8601(), false, false, UpdateSourceError, "connection failed"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var state updateState
	require.NoError(t, json.Unmarshal(data, &state))
	assert.Equal(t, "connection failed", state.Error)
	assert.Equal(t, UpdateSourceError, state.Source)
}

func TestNewDefaultStateStore_Uncovered(t *testing.T) {
	store := newDefaultStateStore()
	require.NotNil(t, store)
	assert.Equal(t, updateStatePath(), store.path)
}

func TestLoadStateFromFile_NotFound_Uncovered(t *testing.T) {
	state, err := loadStateFromFile(afero.NewOsFs(), "/nonexistent/path.json")
	require.NoError(t, err)
	assert.Nil(t, state)
}

func TestSaveStateToFile_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "save_test.json")

	state := &updateState{Version: "v3.0.0", CheckedAt: nowISO8601(), Source: UpdateSourceCached}
	require.NoError(t, saveStateToFile(afero.NewOsFs(), path, state))

	loaded, err := loadStateFromFile(afero.NewOsFs(), path)
	require.NoError(t, err)
	assert.Equal(t, "v3.0.0", loaded.Version)
}

// --- paths.go uncovered ---

func TestDataDir_EnvOverride_Uncovered(t *testing.T) {
	original := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", original)

	os.Setenv("JAVINIZER_DATA_DIR", "/custom/data")
	assert.Equal(t, "/custom/data", dataDir())

	os.Setenv("JAVINIZER_DATA_DIR", "")
	assert.Equal(t, "data", dataDir())
}

func TestUpdateStatePath_UsesDataDir_Uncovered(t *testing.T) {
	original := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", original)

	os.Setenv("JAVINIZER_DATA_DIR", "/test/dir")
	path := updateStatePath()
	assert.Equal(t, filepath.Join("/test/dir", "update_cache.json"), path)
}

// --- service.go uncovered ---

func TestFormatUpdateMessage_Uncovered(t *testing.T) {
	t.Run("no latest version", func(t *testing.T) {
		msg := formatUpdateMessage("1.0.0", "")
		assert.Contains(t, msg, "Current version: 1.0.0")
	})

	t.Run("up to date", func(t *testing.T) {
		msg := formatUpdateMessage("1.0.0", "1.0.0")
		assert.Contains(t, msg, "latest version")
	})

	t.Run("update available", func(t *testing.T) {
		msg := formatUpdateMessage("1.0.0", "2.0.0")
		assert.Contains(t, msg, "Update available")
	})
}

func TestNewService_Uncovered(t *testing.T) {
	t.Run("default interval when zero", func(t *testing.T) {
		s := NewService(UpdateConfig{Enabled: true, VersionCheckIntervalHours: 0})
		assert.Equal(t, defaultCheckInterval, s.interval)
	})

	t.Run("custom interval", func(t *testing.T) {
		s := NewService(UpdateConfig{Enabled: true, VersionCheckIntervalHours: 48})
		assert.Equal(t, 48*time.Hour, s.interval)
	})
}

func TestService_GetStatus_Disabled_Uncovered(t *testing.T) {
	s := NewService(UpdateConfig{Enabled: false})
	state, err := s.GetStatus(nil)
	require.NoError(t, err)
	assert.Equal(t, UpdateSourceDisabled, state.Source)
}

func TestService_IsUpdateAvailable_Uncovered(t *testing.T) {
	s := NewService(UpdateConfig{Enabled: false})
	available, err := s.IsUpdateAvailable(nil)
	require.NoError(t, err)
	assert.False(t, available)
}

func TestService_GetLatestVersion_Uncovered(t *testing.T) {
	s := NewService(UpdateConfig{Enabled: false})
	version, err := s.GetLatestVersion(nil)
	require.NoError(t, err)
	assert.Empty(t, version)
}

func TestService_StartBackgroundCheck_Disabled_Uncovered(t *testing.T) {
	s := NewService(UpdateConfig{Enabled: false})
	// Should not block or panic when disabled
	s.StartBackgroundCheck(nil, time.Second)
}

func TestUpdateSource_Constants_Uncovered(t *testing.T) {
	assert.Equal(t, UpdateSource("cached"), UpdateSourceCached)
	assert.Equal(t, UpdateSource("fresh"), UpdateSourceFresh)
	assert.Equal(t, UpdateSource("disabled"), UpdateSourceDisabled)
	assert.Equal(t, UpdateSource("none"), UpdateSourceNone)
	assert.Equal(t, UpdateSource("error"), UpdateSourceError)
}

func TestUpdateState_JSON_RoundTrip_Uncovered(t *testing.T) {
	state := &updateState{
		Version:    "v1.2.3",
		CheckedAt:  "2024-06-01T00:00:00Z",
		Available:  true,
		Prerelease: false,
		Source:     UpdateSourceFresh,
		Error:      "",
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)

	var parsed updateState
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, state.Version, parsed.Version)
	assert.Equal(t, state.Available, parsed.Available)
	assert.Equal(t, state.Source, parsed.Source)
}
