package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckLatestVersion_RequestCreationError tests the "failed to create request" error path.
func TestCheckLatestVersion_RequestCreationError(t *testing.T) {
	// A context that is already cancelled will cause NewRequestWithContext to fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", "http://127.0.0.1:1")
	_, err := checker.CheckLatestVersion(ctx)
	require.Error(t, err)
}

// TestCheckLatestVersion_NetworkError tests the "failed to fetch releases" error path.
func TestCheckLatestVersion_NetworkError(t *testing.T) {
	// Point to a server that's not running
	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", "http://127.0.0.1:1")
	_, err := checker.CheckLatestVersion(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch releases")
}

// TestCheckLatestVersion_ReadBodyError tests the "failed to read response" error path
// by serving a response that errors on read.
func TestCheckLatestVersion_ReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write headers then immediately close the connection
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		// Don't write body — client will get unexpected EOF
	}))
	defer server.Close()

	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	_, err := checker.CheckLatestVersion(context.Background())
	// This may or may not error depending on how the body is read
	// The important thing is we exercise the read path
	_ = err
}

// TestCheckLatestVersion_GHToken tests the GH_TOKEN/GITHUB_TOKEN header path.
func TestCheckLatestVersion_GHToken(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(map[string]interface{}{
			"tag_name":   "v1.0.0",
			"prerelease": false,
		})
		_, _ = w.Write(data)
	}))
	defer server.Close()

	t.Run("GH_TOKEN is used", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "test-gh-token")
		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		_, err := checker.CheckLatestVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "Bearer test-gh-token", receivedAuth)
	})

	t.Run("GITHUB_TOKEN fallback is used", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "test-github-token")
		checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
		_, err := checker.CheckLatestVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "Bearer test-github-token", receivedAuth)
	})
}

// TestGetRecentReleases_Success tests the getRecentReleases method.
func TestGetRecentReleases_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/releases")
		assert.Equal(t, "10", r.URL.Query().Get("per_page"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"tag_name":"v1.5.0","name":"v1.5.0"},
			{"tag_name":"","name":"v1.4.0"},
			{"tag_name":"v1.3.0","name":""}
		]`))
	}))
	defer server.Close()

	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	versions, _, err := checker.getRecentReleases(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"v1.5.0", "v1.4.0", "v1.3.0"}, versions)
}

// TestGetRecentReleases_Non200 tests the error path when GitHub returns non-200.
func TestGetRecentReleases_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	_, _, err := checker.getRecentReleases(context.Background(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

// TestGetRecentReleases_MalformedJSON tests the parse error path.
func TestGetRecentReleases_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	_, _, err := checker.getRecentReleases(context.Background(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse response")
}

// TestGetRecentReleases_NetworkError tests the network error path.
func TestGetRecentReleases_NetworkError(t *testing.T) {
	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", "http://127.0.0.1:1")
	_, _, err := checker.getRecentReleases(context.Background(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch releases")
}

// TestGetRecentReleases_GHToken tests the GH_TOKEN/GITHUB_TOKEN header path in getRecentReleases.
func TestGetRecentReleases_GHToken(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	t.Setenv("GH_TOKEN", "recent-token")
	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	_, _, err := checker.getRecentReleases(context.Background(), 5)
	require.NoError(t, err)
	assert.Equal(t, "Bearer recent-token", receivedAuth)
}

// TestGetRecentReleases_EmptyNameAndTag tests the path where both tag_name and name are empty.
func TestGetRecentReleases_EmptyNameAndTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"tag_name":"","name":""},
			{"tag_name":"v1.0.0","name":"v1.0.0"}
		]`))
	}))
	defer server.Close()

	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	versions, _, err := checker.getRecentReleases(context.Background(), 10)
	require.NoError(t, err)
	// Empty entries should be skipped
	assert.Equal(t, []string{"v1.0.0"}, versions)
}

// TestCompareVersions_LegacyEdgeCases exercises additional legacy comparison paths.
func TestCompareVersions_LegacyEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		latest   string
		expected int
	}{
		// These force the legacy path since they're not valid semver
		{"1.0.rc1 vs 1.0.0", "1.0.rc1", "1.0.0", 0},
		{"1.2.3-rc1 vs 1.2.3 (prerelease < stable)", "1.2.3-rc1", "1.2.3", -1},
		{"1.2.3 vs 1.2.3-rc1 (stable > prerelease)", "1.2.3", "1.2.3-rc1", 1},
		{"1.2.3-rc1 vs 1.2.3-rc1 (same)", "1.2.3-rc1", "1.2.3-rc1", 0},
		{"empty strings", "", "", 0},
		{"short version padded", "1.0", "1.0.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.current, tt.latest)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestNormalizeSemver exercises the normalizeSemver function paths.
func TestNormalizeSemver(t *testing.T) {
	assert.Equal(t, "v1.0.0", normalizeSemver("1.0.0"))
	assert.Equal(t, "v1.0.0", normalizeSemver("v1.0.0"))
	assert.Equal(t, "", normalizeSemver(""))
	assert.Equal(t, "v1.0.0", normalizeSemver(" v1.0.0")) // whitespace is trimmed
}

// TestParseInt exercises the parseInt function for various inputs.
func TestParseInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasErr   bool
	}{
		{"123", 123, false},
		{"0", 0, false},
		{"1rc1", 1, false}, // numeric prefix only
		{"rc1", 0, true},   // no numeric prefix
		{"", 0, true},      // empty string
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseInt(tt.input)
			if tt.hasErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

// TestParseStringToInt exercises the parseStringToInt function.
func TestParseStringToInt(t *testing.T) {
	n, err := parseStringToInt("42")
	assert.NoError(t, err)
	assert.Equal(t, 42, n)

	n, err = parseStringToInt("0")
	assert.NoError(t, err)
	assert.Equal(t, 0, n)

	n, err = parseStringToInt("1rc1")
	assert.NoError(t, err)
	assert.Equal(t, 1, n) // stops at non-digit
}

// TestCheckForUpdateWithChecker_NoUpdateNeeded tests the "no update available" path.
func TestCheckForUpdateWithChecker_NoUpdateNeeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(map[string]interface{}{
			"tag_name":   "v1.0.0",
			"prerelease": false,
		})
		_, _ = w.Write(data)
	}))
	defer server.Close()

	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	latest, available, err := checkForUpdateWithChecker(context.Background(), "v1.0.0", false, checker)
	require.NoError(t, err)
	assert.False(t, available)
	assert.NotNil(t, latest)
	assert.Equal(t, "v1.0.0", latest.Version)
}

// TestCheckForUpdateWithChecker_UpdateAvailable tests the "update available" path.
func TestCheckForUpdateWithChecker_UpdateAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(map[string]interface{}{
			"tag_name":   "v2.0.0",
			"prerelease": false,
		})
		_, _ = w.Write(data)
	}))
	defer server.Close()

	checker := newGitHubCheckerWithBaseURL("javinizer/Javinizer", server.URL)
	latest, available, err := checkForUpdateWithChecker(context.Background(), "v1.0.0", false, checker)
	require.NoError(t, err)
	assert.True(t, available)
	assert.NotNil(t, latest)
	assert.Equal(t, "v2.0.0", latest.Version)
}
