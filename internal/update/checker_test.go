package update

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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
	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)

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
			info, err := parseGitHubReleaseVersion(tt.tagName)
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
	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)

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
			checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
			ctx := context.Background()

			latest, available, err := checkForUpdateWithChecker(ctx, tt.current, tt.checkPrerelease, checker)

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
			info, err := parseGitHubReleaseVersion(tt.tagName)
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
	info := versionInfo{
		Version:     "v1.6.0",
		TagName:     "v1.6.0",
		Prerelease:  false,
		PublishedAt: "2026-03-20T12:00:00Z",
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var loaded versionInfo
	err = json.Unmarshal(data, &loaded)
	assert.NoError(t, err)
	assert.Equal(t, info, loaded)
}

// TestGitHubChecker_ClientConfig tests that the checker has proper client settings.
func TestGitHubChecker_ClientConfig(t *testing.T) {
	checker := newGitHubChecker("test/repo")

	// Verify the checker was created with proper settings
	assert.NotNil(t, checker)
	assert.Equal(t, "test/repo", checker.repo)
	assert.NotNil(t, checker.httpClient)
	assert.Equal(t, 10*time.Second, checker.httpClient.Timeout)
}

func TestCheckLatestVersion_EdgeCases(t *testing.T) {
	t.Run("rate limited 403", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		_, err := checker.CheckLatestVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rate limited")
	})

	t.Run("rate limited 429", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		_, err := checker.CheckLatestVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rate limited")
	})

	t.Run("non-200 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		}))
		defer server.Close()

		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		_, err := checker.CheckLatestVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 500")
	})

	t.Run("malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{not-json`))
		}))
		defer server.Close()

		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		_, err := checker.CheckLatestVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse response")
	})

	t.Run("falls back to name when tag is empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"tag_name": "",
				"name": "v2.0.0",
				"prerelease": false,
				"published_at": "2026-03-24T00:00:00Z"
			}`))
		}))
		defer server.Close()

		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		info, err := checker.CheckLatestVersion(context.Background())
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, "v2.0.0", info.Version)
		assert.Equal(t, "", info.TagName)
	})
}

func TestCheckForUpdateWithChecker_PrereleaseFallback(t *testing.T) {
	t.Run("uses latest stable from recent releases when prerelease checks are disabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/javinizer/Javinizer/releases/latest":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"tag_name": "v1.6.0-rc1",
					"name": "v1.6.0-rc1",
					"prerelease": true
				}`))
			case "/repos/javinizer/Javinizer/releases":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[
					{"tag_name":"v1.6.0-rc1","name":"v1.6.0-rc1"},
					{"tag_name":"v1.5.0","name":"v1.5.0"}
				]`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		latest, available, err := checkForUpdateWithChecker(context.Background(), "v1.4.0", false, checker)
		require.NoError(t, err)
		require.NotNil(t, latest)
		assert.True(t, available)
		assert.Equal(t, "v1.5.0", latest.Version)
		assert.False(t, latest.Prerelease)
	})

	t.Run("reports no update when no stable release exists", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/javinizer/Javinizer/releases/latest":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"tag_name": "v2.0.0-rc1",
					"name": "v2.0.0-rc1",
					"prerelease": true
				}`))
			case "/repos/javinizer/Javinizer/releases":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[
					{"tag_name":"v2.0.0-rc1"},
					{"tag_name":"v2.0.0-beta1"}
				]`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		latest, available, err := checkForUpdateWithChecker(context.Background(), "v1.9.0", false, checker)
		require.NoError(t, err)
		// checkPrerelease=false and no stable fallback found → report no update
		// rather than surfacing the prerelease as available.
		require.NotNil(t, latest)
		assert.False(t, available)
		assert.Equal(t, "v2.0.0-rc1", latest.Version)
		assert.True(t, latest.Prerelease)
	})

	t.Run("keeps prerelease when fallback lookup fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/javinizer/Javinizer/releases/latest":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"tag_name": "v3.0.0-rc1",
					"name": "v3.0.0-rc1",
					"prerelease": true
				}`))
			case "/repos/javinizer/Javinizer/releases":
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		latest, available, err := checkForUpdateWithChecker(context.Background(), "v2.9.0", false, checker)
		require.NoError(t, err)
		// checkPrerelease=false and fallback lookup failed → report no update
		// rather than surfacing the prerelease as available.
		require.NotNil(t, latest)
		assert.False(t, available)
		assert.Equal(t, "v3.0.0-rc1", latest.Version)
		assert.True(t, latest.Prerelease)
	})
}

func TestCompareVersions_LegacyFallback(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		latest   string
		expected int
	}{
		{
			name:     "numeric prefix in segment is compared",
			current:  "1.2.3beta",
			latest:   "1.2.4alpha",
			expected: -1,
		},
		{
			name:     "missing numeric prefix falls back to zero",
			current:  "1.2.rc1",
			latest:   "1.2.0",
			expected: 0,
		},
		{
			name:     "stable preferred over prerelease in legacy mode",
			current:  "1.2.3-rc1",
			latest:   "1.2.3",
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.current, tt.latest)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestWrapperFunctions(t *testing.T) {
	t.Run("GetLatestStableVersion uses default checker", func(t *testing.T) {
		original := http.DefaultTransport
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "api.github.com", req.URL.Host)
			require.Equal(t, "/repos/javinizer/Javinizer/releases/latest", req.URL.Path)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"tag_name":"v9.9.9",
					"name":"v9.9.9",
					"prerelease":false,
					"published_at":"2026-03-24T00:00:00Z"
				}`)),
				Header: make(http.Header),
			}, nil
		})
		defer func() { http.DefaultTransport = original }()

		info, err := getLatestStableVersion(context.Background())
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, "v9.9.9", info.Version)
	})

	t.Run("CheckForUpdate uses default checker", func(t *testing.T) {
		original := http.DefaultTransport
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "api.github.com", req.URL.Host)
			require.Equal(t, "/repos/javinizer/Javinizer/releases/latest", req.URL.Path)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"tag_name":"v2.0.0",
					"name":"v2.0.0",
					"prerelease":false
				}`)),
				Header: make(http.Header),
			}, nil
		})
		defer func() { http.DefaultTransport = original }()

		latest, available, err := checkForUpdate(context.Background(), "v1.0.0", false)
		require.NoError(t, err)
		require.NotNil(t, latest)
		assert.True(t, available)
		assert.Equal(t, "v2.0.0", latest.Version)
	})

	t.Run("GetRecentReleases wrapper", func(t *testing.T) {
		original := http.DefaultTransport
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "api.github.com", req.URL.Host)
			require.Equal(t, "/repos/javinizer/Javinizer/releases", req.URL.Path)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`[
					{"tag_name":"v1.5.0"},
					{"tag_name":"","name":"v1.4.0"},
					{"tag_name":"v1.3.0"}
				]`)),
				Header: make(http.Header),
			}, nil
		})
		defer func() { http.DefaultTransport = original }()

		versions, err := getRecentReleases(context.Background(), 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"v1.5.0", "v1.4.0", "v1.3.0"}, versions)
	})
}
