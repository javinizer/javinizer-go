package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

func TestCompareVersionsAcceptedLegacyTags(t *testing.T) {
	for _, tc := range []struct {
		name, current, latest string
		want                  int
	}{
		{"numeric suffix increases", "v1.2.3beta", "v1.2.10beta", -1},
		{"numeric suffix decreases", "v1.2.10beta", "v1.2.3beta", 1},
		{"legacy prerelease precedes stable", "v1.2.3-01", "v1.2.3", -1},
		{"stable follows legacy prerelease", "v1.2.3", "v1.2.3-01", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The release parser accepts both unhyphenated suffixes and
			// leading-zero numeric prereleases, unlike strict semver. Verify
			// those preconditions so valid semver cannot mask legacy behavior.
			for _, tag := range []string{tc.current, tc.latest} {
				_, err := parseGitHubReleaseVersion(tag)
				require.NoError(t, err, "supported release tag %q", tag)
			}
			require.False(t, semver.IsValid(tc.current) && semver.IsValid(tc.latest))
			require.Equal(t, tc.want, CompareVersions(tc.current, tc.latest))
		})
	}
}

func TestPrereleaseCheckerRateLimitReturnsNoRelease(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/repos/fixture/repo/releases?per_page=1", r.URL.RequestURI())
				w.WriteHeader(status)
				// Even a parseable release must not escape a rate-limit response.
				_, _ = io.WriteString(w, `[{"tag_name":"v2.0.0-rc1"}]`)
			}))
			defer server.Close()
			checker := NewCheckerWithBaseURL("fixture/repo", server.URL)
			prereleaseChecker, ok := checker.(PreReleaseChecker)
			require.True(t, ok)
			prereleaseChecker.SetPreRelease(true)

			latest, err := checker.CheckLatestVersion(context.Background())
			require.EqualError(t, err, fmt.Sprintf("prerelease release lookup failed: rate limited by GitHub API (status: %d)", status))
			require.Nil(t, latest)
			require.EqualValues(t, 1, requests.Load(), "must not retry or fall back to stable releases")
		})
	}
}

func TestUpgradeCheckOnlyUsesRealCheckerWithoutMutatingInstall(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	for _, tc := range []struct {
		name, current, latest, requestURI, response, output string
		prerelease, upToDate                                bool
	}{
		{
			name: "new stable release", current: "v1.0.0", latest: "v2.0.0",
			requestURI: "/repos/fixture/repo/releases/latest",
			response:   `{"tag_name":"v2.0.0","name":"Ignored display name","prerelease":false}`,
			output:     "Update available: v2.0.0 (current: v1.0.0)\n",
		},
		{
			name: "already current", current: "v2.0.0", latest: "v2.0.0", upToDate: true,
			requestURI: "/repos/fixture/repo/releases/latest",
			response:   `{"tag_name":"v2.0.0","prerelease":false}`,
			output:     "Already up to date: v2.0.0\n",
		},
		{
			name: "prerelease opt in", current: "v2.0.0", latest: "v3.0.0-rc1", prerelease: true,
			requestURI: "/repos/fixture/repo/releases?per_page=1",
			response:   `[{"tag_name":"v3.0.0-rc1","prerelease":true}]`,
			output:     "Update available: v3.0.0-rc1 (current: v2.0.0)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "javinizer")
			require.NoError(t, os.WriteFile(target, []byte("preserve installed fixture"), 0o600))
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, tc.requestURI, r.URL.RequestURI())
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			downloads := &rejectedDownloadTransport{}
			var out bytes.Buffer
			result, err := Upgrade(context.Background(), UpgradeOptions{
				CurrentVersion: tc.current, CheckOnly: true, PreRelease: tc.prerelease,
				Repo: "fixture/repo", APIBaseURL: server.URL, Checker: nil,
				DownloadBaseURL: "https://downloads.invalid", HTTPClient: &http.Client{Transport: downloads},
				ExePath: target, Out: &out,
			})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.current, result.CurrentVersion)
			require.Equal(t, tc.latest, result.LatestVersion)
			require.Equal(t, tc.latest, result.TagName)
			require.Equal(t, tc.upToDate, result.UpToDate)
			require.False(t, result.Upgraded)
			require.False(t, result.Handoff)
			require.Empty(t, result.AssetName)
			require.Equal(t, tc.output, out.String())
			require.EqualValues(t, 1, requests.Load())
			require.Zero(t, downloads.calls.Load(), "check-only must not request checksums or executable bytes")
			retained, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "preserve installed fixture", string(retained))
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "check-only must not create staging files or backups")
		})
	}
}

func TestUpgradeFinalRenameFailurePreservesTargetAndCleansStaging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix replacement rejects a file renamed over a directory; Windows has a separate backup flow")
	}
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer")
	require.NoError(t, os.Mkdir(target, 0o700))
	sentinel := filepath.Join(target, "keep.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("preserve existing directory"), 0o600))
	// Upgrade resolves the target before replacement, including symlinks in
	// the temporary root. Keep the raw input but compare canonical paths.
	canonicalTarget, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)
	payload := "verified fixture payload; never executed\n"
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte(payload)), asset)
	var requested []string
	client := &http.Client{Transport: edgeTransport(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		var body string
		switch r.URL.String() {
		case "https://downloads.invalid/fixture/repo/releases/download/v2.0.0/checksums.txt":
			body = checksum
		case "https://downloads.invalid/fixture/repo/releases/download/v2.0.0/" + asset:
			body = payload
		default:
			return nil, fmt.Errorf("unexpected fixture download: %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	var out bytes.Buffer
	result, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v1.0.0", Repo: "fixture/repo", Checker: &stubChecker{version: "v2.0.0"},
		DownloadBaseURL: "https://downloads.invalid", HTTPClient: client, ExePath: target, Out: &out,
	})
	require.ErrorContains(t, err, "replace binary")
	var renameErr *os.LinkError
	require.ErrorAs(t, err, &renameErr)
	require.Equal(t, "rename", renameErr.Op)
	require.Equal(t, canonicalTarget, renameErr.New)
	require.Equal(t, filepath.Dir(canonicalTarget), filepath.Dir(renameErr.Old))
	require.True(t, strings.HasPrefix(filepath.Base(renameErr.Old), ".javinizer-upgrade-"))
	require.NoFileExists(t, renameErr.Old, "failed install must remove its verified staging file")
	require.NotNil(t, result)
	require.False(t, result.Upgraded)
	require.False(t, result.UpToDate)
	require.False(t, result.Handoff)
	require.Equal(t, "v2.0.0", result.LatestVersion)
	require.Equal(t, asset, result.AssetName)
	require.Equal(t, []string{
		"https://downloads.invalid/fixture/repo/releases/download/v2.0.0/checksums.txt",
		"https://downloads.invalid/fixture/repo/releases/download/v2.0.0/" + asset,
	}, requested)
	require.Contains(t, out.String(), "Installing...\n", "download, checksum verification and chmod must precede the rename failure")
	require.NotContains(t, out.String(), "Upgraded to")
	retained, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, "preserve existing directory", string(retained))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no staging file or backup should remain")
	require.Equal(t, "javinizer", entries[0].Name())
	require.True(t, entries[0].IsDir())
}
