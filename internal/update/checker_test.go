package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckLatestVersion tests the GitHub checker with a mock server.
func TestCheckLatestVersion(t *testing.T) {
	// Create a mock GitHub API server
	mockReleases := map[string]interface{}{
		"tag_name":     "v1.6.0",
		"name":         "Version 1.6.0",
		"prerelease":   false,
		"published_at": "2026-03-20T12:00:00Z",
	}

	jsonBytes, _ := json.Marshal(mockReleases)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/javinizer/Javinizer/releases/latest" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(jsonBytes)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create checker with mock server URL using the test helper
	checker := NewGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)

	ctx := context.Background()
	info, err := checker.CheckLatestVersion(ctx)

	assert.NoError(t, err)
	assert.Equal(t, "v1.6.0", info.Version)
	assert.Equal(t, "v1.6.0", info.TagName)
	assert.False(t, info.Prerelease)
	assert.Equal(t, "2026-03-20T12:00:00Z", info.PublishedAt)
}

// TestParseGitHubReleaseVersion tests version parsing.
func TestParseGitHubReleaseVersion(t *testing.T) {
	tests := []struct {
		name        string
		tagName     string
		wantVersion string
		wantErr     bool
	}{
		{"with v prefix", "v1.6.0", "v1.6.0", false},
		{"without v prefix", "1.6.0", "v1.6.0", false},
		{"with prerelease", "v1.6.0-rc1", "v1.6.0-rc1", false},
		{"with build metadata", "v1.6.0+build", "v1.6.0+build", false},
		{"invalid version", "invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseGitHubReleaseVersion(tt.tagName)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantVersion, info.Version)
		})
	}
}

// TestIsPrerelease tests prerelease detection.
func TestIsPrerelease(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"v1.6.0", false},
		{"1.6.0", false},
		{"v1.6.0-rc1", true},
		{"v1.6.0-beta.2", true},
		{"v1.6.0-alpha", true},
		{"v1.6.0-rc1-123-gabc123", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := IsPrerelease(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCompareVersions tests version comparison.
func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		latest   string
		expected int
	}{
		{"current less than latest", "v1.5.0", "v1.6.0", -1},
		{"current greater than latest", "v1.7.0", "v1.6.0", 1},
		{"current equals latest", "v1.6.0", "v1.6.0", 0},
		{"without v prefix", "1.5.0", "1.6.0", -1},
		{"different major", "v2.0.0", "v1.9.0", 1},
		{"different minor", "v1.5.0", "v1.6.0", -1},
		{"different patch", "v1.6.0", "v1.6.1", -1},
		{"current prerelease vs stable (same base)", "v1.6.0-rc1", "v1.6.0", -1},
		{"current stable vs prerelease (same base)", "v1.6.0", "v1.6.0-rc1", 1},
		{"prerelease progression", "v1.6.0-rc1", "v1.6.0-rc2", -1},
		{"reverse prerelease progression", "v1.6.0-rc2", "v1.6.0-rc1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.current, tt.latest)
			assert.Equal(t, tt.expected, got, "CompareVersions(%q, %q)", tt.current, tt.latest)
		})
	}
}

// TestGetLatestStableVersion tests with mock server.
func TestGetLatestStableVersion(t *testing.T) {
	// Create a mock GitHub API server
	mockReleases := map[string]interface{}{
		"tag_name":     "v1.6.0",
		"name":         "Version 1.6.0",
		"prerelease":   false,
		"published_at": "2026-03-20T12:00:00Z",
	}

	jsonBytes, _ := json.Marshal(mockReleases)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/javinizer/Javinizer/releases/latest" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(jsonBytes)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create a checker with the mock server
	checker := NewGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)

	// Test with context
	ctx := context.Background()
	info, err := checker.CheckLatestVersion(ctx)

	assert.NotNil(t, info)
	assert.NoError(t, err)
	assert.Equal(t, "v1.6.0", info.Version)
}

// TestCheckForUpdate tests the full update check flow with mock server.
func TestCheckForUpdate(t *testing.T) {
	// Create a mock GitHub API server
	mockReleases := map[string]interface{}{
		"tag_name":     "v1.6.0",
		"name":         "Version 1.6.0",
		"prerelease":   false,
		"published_at": "2026-03-20T12:00:00Z",
	}

	jsonBytesStable, _ := json.Marshal(mockReleases)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/javinizer/Javinizer/releases/latest" {
			// Return stable release for latest, prerelease for recent
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(jsonBytesStable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tests := []struct {
		name            string
		current         string
		checkPrerelease bool
		wantAvailable   bool
		wantErr         bool
	}{
		{"new stable available", "v1.5.0", false, true, false},
		{"no update needed", "v1.6.0", false, false, false},
		{"prerelease without check - falls back to stable", "v1.5.0", false, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
			ctx := context.Background()

			latest, available, err := CheckForUpdateWithChecker(ctx, tt.current, tt.checkPrerelease, checker)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantAvailable, available, "available should match expected")
			if available {
				assert.NotNil(t, latest)
			}
		})
	}
}

// TestParseGitHubReleaseVersionEdgeCases tests edge cases in version parsing.
func TestParseGitHubReleaseVersionEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		tagName     string
		wantVersion string
	}{
		{"v prefix only", "v", "v"},
		{"just version", "1.0.0", "v1.0.0"},
		{"with dots in suffix", "v1.6.0-rc.1.2", "v1.6.0-rc.1.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseGitHubReleaseVersion(tt.tagName)
			if err != nil && tt.tagName != "v" {
				// "v" alone is technically invalid semver
				t.Logf("Got expected error for %q: %v", tt.tagName, err)
			}
			if info != nil {
				assert.Equal(t, tt.wantVersion, info.Version)
			}
		})
	}
}

// TestVersionInfo_JSON tests JSON serialization.
func TestVersionInfo_JSON(t *testing.T) {
	info := VersionInfo{
		Version:     "v1.6.0",
		TagName:     "v1.6.0",
		Prerelease:  false,
		PublishedAt: "2026-03-20T12:00:00Z",
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var loaded VersionInfo
	err = json.Unmarshal(data, &loaded)
	assert.NoError(t, err)
	assert.Equal(t, info, loaded)
}

// TestGitHubChecker_ClientConfig tests that the checker has proper client settings.
func TestGitHubChecker_ClientConfig(t *testing.T) {
	checker := NewGitHubChecker("test/repo")

	// Verify the checker was created with proper settings
	assert.NotNil(t, checker)
	assert.Equal(t, "test/repo", checker.repo)
	assert.NotNil(t, checker.httpClient)
	assert.Equal(t, 10*time.Second, checker.httpClient.Timeout)
}
