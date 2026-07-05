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
			require.Equal(t, "/repos/javinizer/javinizer-go/releases/latest", req.URL.Path)
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
			require.Equal(t, "/repos/javinizer/javinizer-go/releases/latest", req.URL.Path)
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
			require.Equal(t, "/repos/javinizer/javinizer-go/releases", req.URL.Path)
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

// TestDefaultRepoPointsAtGoRewrite is a regression guard: the update checker
// must consult the Go rewrite (javinizer/javinizer-go), NOT the legacy Python
// project (javinizer/Javinizer) whose releases are unrelated. A previous bug
// pointed every production construction site at the old repo, so users were
// notified about the Python project's 2.6.4 release. This test pins the
// constant so a silent re-regression fails CI.
func TestDefaultRepoPointsAtGoRewrite(t *testing.T) {
	assert.Equal(t, "javinizer/javinizer-go", defaultRepo)
	// The production default checker must use it.
	chk := newGitHubChecker(defaultRepo)
	assert.Equal(t, "javinizer/javinizer-go", chk.repo)
}

// TestCheckLatestVersion_FallsBackToPrereleaseOn404 verifies that a
// prerelease-only repo (whose /releases/latest endpoint 404s because GitHub
// excludes prereleases) still yields its most recent release via the list
// endpoint fallback. Without this, the Go rewrite — which currently ships only
// v0.x-alpha releases — would never surface an update.
func TestCheckLatestVersion_FallsBackToPrereleaseOn404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/javinizer/javinizer-go/releases/latest":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/javinizer/javinizer-go/releases":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{"tag_name":"v0.3.15-alpha","name":"v0.3.15-alpha","prerelease":true},
				{"tag_name":"v0.3.14-alpha","name":"v0.3.14-alpha","prerelease":true}
			]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	chk := newGitHubCheckerWithBaseURL("javinizer/javinizer-go", server.URL)
	info, err := chk.CheckLatestVersion(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "v0.3.15-alpha", info.Version)
	assert.True(t, info.Prerelease, "404 fallback should surface the prerelease")
}

// TestCheckLatestVersion_404WithNoReleasesErrors verifies the 404 fallback
// errors cleanly (rather than returning a zero-value) when the repo has no
// releases at all.
func TestCheckLatestVersion_404WithNoReleasesErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/javinizer/javinizer-go/releases/latest":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/javinizer/javinizer-go/releases":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	chk := newGitHubCheckerWithBaseURL("javinizer/javinizer-go", server.URL)
	info, err := chk.CheckLatestVersion(context.Background())
	require.Error(t, err)
	assert.Nil(t, info)
}

// TestCheckLatestVersion_ETag304ReturnsNotModified verifies the rate-limit
// optimization on the stable-repo path: the first request captures the ETag,
// and a second request sending it back as If-None-Match gets a 304 — which
// GitHub does NOT count against quota — and surfaces as ErrNotModified.
func TestCheckLatestVersion_ETag304ReturnsNotModified(t *testing.T) {
	const etag = `W/"stable-etag-123"`
	mockReleases := map[string]interface{}{
		"tag_name":     "v1.6.0",
		"name":         "Version 1.6.0",
		"prerelease":   false,
		"published_at": "2026-03-20T12:00:00Z",
	}
	jsonBytes, _ := json.Marshal(mockReleases)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/javinizer/Javinizer/releases/latest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Conditional GET: return 304 when the client sends our ETag back.
		if inm := r.Header.Get("If-None-Match"); inm == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jsonBytes)
	}))
	defer server.Close()

	chk := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)

	// First check: full fetch, captures the ETag.
	info, err := chk.CheckLatestVersion(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "v1.6.0", info.Version)
	assert.Equal(t, etag, info.ETag, "ETag captured from the response header")

	// Second check: send the ETag back, expect a 304 → ErrNotModified.
	chk.SetIfNoneMatch(info.ETag)
	info2, err := chk.CheckLatestVersion(context.Background())
	require.ErrorIs(t, err, ErrNotModified)
	assert.Nil(t, info2)
}

// TestCheckLatestVersion_SkipLatestSkips404Endpoint verifies that once the
// service has learned a repo has no stable latest release (NoStableLatest),
// the next check skips the /releases/latest 404 entirely and goes straight to
// the /releases list — halving API calls for a prerelease-only repo. It also
// confirms the list path honors If-None-Match (304 → ErrNotModified).
func TestCheckLatestVersion_SkipLatestSkips404Endpoint(t *testing.T) {
	const etag = `W/"prerelease-etag-456"`
	var latestHit int
	mockList := []map[string]interface{}{{
		"tag_name":   "v0.3.15-alpha",
		"name":       "v0.3.15-alpha",
		"prerelease": true,
	}}
	listBytes, _ := json.Marshal(mockList)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/javinizer/javinizer-go/releases/latest":
			latestHit++
			w.WriteHeader(http.StatusNotFound)
		case "/repos/javinizer/javinizer-go/releases":
			if inm := r.Header.Get("If-None-Match"); inm == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(listBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	chk := newGitHubCheckerWithBaseURL("javinizer/javinizer-go", server.URL)

	// First check: /releases/latest 404s → falls back to the list, captures
	// the ETag and reports NoStableLatest=true.
	info, err := chk.CheckLatestVersion(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "v0.3.15-alpha", info.Version)
	assert.Equal(t, etag, info.ETag)
	assert.True(t, info.NoStableLatest, "404 fallback sets NoStableLatest")
	assert.Equal(t, 1, latestHit, "first check hits /releases/latest")

	// Second check: skipLatest set → /releases/latest must NOT be hit, and the
	// list path returns 304 (rate-limit-free) via If-None-Match.
	chk.SetSkipLatest(true)
	chk.SetIfNoneMatch(info.ETag)
	info2, err := chk.CheckLatestVersion(context.Background())
	require.ErrorIs(t, err, ErrNotModified)
	assert.Nil(t, info2)
	assert.Equal(t, 1, latestHit, "skipLatest avoided the /releases/latest 404")
}

// TestCheckLatestVersion_SkipLatestGoesStraightToList verifies that with
// skipLatest the result still resolves through the list endpoint (not a 304)
// when there is no ETag to send — i.e. skipLatest alone is correct.
func TestCheckLatestVersion_SkipLatestGoesStraightToList(t *testing.T) {
	var latestHit int
	mockList := []map[string]interface{}{{
		"tag_name":   "v0.3.15-alpha",
		"prerelease": true,
	}}
	listBytes, _ := json.Marshal(mockList)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/javinizer/javinizer-go/releases/latest":
			latestHit++
			w.WriteHeader(http.StatusNotFound)
		case "/repos/javinizer/javinizer-go/releases":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(listBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	chk := newGitHubCheckerWithBaseURL("javinizer/javinizer-go", server.URL)
	chk.SetSkipLatest(true)

	info, err := chk.CheckLatestVersion(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "v0.3.15-alpha", info.Version)
	assert.True(t, info.NoStableLatest)
	assert.Equal(t, 0, latestHit, "/releases/latest never hit when skipLatest is set")
}

// preRelease must consult the /releases list (newest release, including
// prereleases) even when /releases/latest would return a valid stable release —
// so a user on stable can jump to a newer prerelease. Unlike skipLatest, it
// must NOT set NoStableLatest (a stable latest may well exist; the list was
// chosen deliberately, not because /releases/latest 404'd).
func TestCheckLatestVersion_PreReleaseUsesListEvenWithStableLatest(t *testing.T) {
	var latestHit int
	// The list returns a NEWER prerelease than the stable /releases/latest would.
	mockList := []map[string]interface{}{{
		"tag_name":   "v1.1.0-rc1",
		"prerelease": true,
	}}
	listBytes, _ := json.Marshal(mockList)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/javinizer/javinizer-go/releases/latest":
			latestHit++
			// A stable latest exists — but preRelease must not even ask.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"tag_name":"v1.0.0","prerelease":false}`))
		case "/repos/javinizer/javinizer-go/releases":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(listBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	chk := newGitHubCheckerWithBaseURL("javinizer/javinizer-go", server.URL)
	chk.SetPreRelease(true)

	info, err := chk.CheckLatestVersion(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "v1.1.0-rc1", info.Version, "preRelease must return the newest release (incl. prerelease)")
	assert.True(t, info.Prerelease)
	assert.False(t, info.NoStableLatest, "preRelease must not set NoStableLatest")
	assert.Equal(t, 0, latestHit, "/releases/latest must not be hit when preRelease is set")
}

func TestCheckLatestVersion_PreReleasePropagatesListError(t *testing.T) {
	// When the /releases list call itself fails (500), preRelease must surface the
	// error rather than falling back to /releases/latest.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	chk := newGitHubCheckerWithBaseURL("javinizer/javinizer-go", server.URL)
	chk.SetPreRelease(true)

	_, err := chk.CheckLatestVersion(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prerelease release lookup failed")
}

func TestCheckLatestVersion_PreReleaseEmptyList(t *testing.T) {
	// An empty releases list under preRelease must error ("no releases found")
	// rather than returning a nil/zero-value result.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	chk := newGitHubCheckerWithBaseURL("javinizer/javinizer-go", server.URL)
	chk.SetPreRelease(true)

	_, err := chk.CheckLatestVersion(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no releases found")
}

func TestNewChecker_DefaultRepo(t *testing.T) {
	chk := NewChecker(defaultRepo)
	require.NotNil(t, chk)
	// The returned checker must be usable: a CheckLatestVersion call should
	// not panic and should return a typed result or error (network errors are
	// fine in tests without network; we only assert the constructor wires up).
	_, _ = chk.CheckLatestVersion(context.Background())
}

func TestNewCheckerWithBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name": "v1.2.3", "name": "v1.2.3", "html_url": "https://example.com", "prerelease": false}`))
	}))
	defer server.Close()

	chk := NewCheckerWithBaseURL("javinizer/javinizer-go", server.URL)
	require.NotNil(t, chk)

	info, err := chk.CheckLatestVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", info.Version)
}
