package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"golang.org/x/mod/semver"
)

const maxUpdateResponseSize = 1024 * 1024 // 1MB

// versionInfo represents information about a GitHub release.
type versionInfo struct {
	Version     string `json:"version"`      // Human-readable version (e.g., "v1.6.0")
	TagName     string `json:"tag_name"`     // Git tag (e.g., "v1.6.0")
	Prerelease  bool   `json:"prerelease"`   // Whether this is a prerelease
	PublishedAt string `json:"published_at"` // ISO8601 timestamp
}

// checker is the interface for checking version information.
type checker interface {
	CheckLatestVersion(ctx context.Context) (*versionInfo, error)
}

// githubChecker checks versions from GitHub releases.
type githubChecker struct {
	repo       string
	httpClient *http.Client
	apiBaseURL string // For testing - override the GitHub API base URL
	envLookup  func(string) string
}

// newGitHubChecker creates a new GitHub checker.
// The repo should be in format "owner/repo" (e.g., "javinizer/Javinizer").
func newGitHubChecker(repo string) *githubChecker {
	return &githubChecker{
		repo:       repo,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		apiBaseURL: "https://api.github.com",
		envLookup:  os.Getenv,
	}
}

// newGitHubCheckerWithBaseURL creates a new GitHub checker with a custom base URL (for testing).
func newGitHubCheckerWithBaseURL(repo, baseURL string) *githubChecker {
	return &githubChecker{
		repo:       repo,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		apiBaseURL: baseURL,
		envLookup:  os.Getenv,
	}
}

// CheckLatestVersion fetches the latest stable release from GitHub.
// If no stable release is found, it returns the latest release (which may be a prerelease).
func (c *githubChecker) CheckLatestVersion(ctx context.Context) (*versionInfo, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.apiBaseURL, c.repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set User-Agent
	req.Header.Set("User-Agent", "Javinizer-Updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	// Check for GitHub token in environment
	if token := c.envLookup("GH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if token := c.envLookup("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer func() {
		_ = httpclient.DrainAndClose(resp.Body)
	}()

	// Handle rate limiting
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited by GitHub API (status: %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpdateResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var release struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		Prerelease  bool   `json:"prerelease"`
		PublishedAt string `json:"published_at"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Use tag name as version, or name if tag name is empty
	version := release.TagName
	if version == "" && release.Name != "" {
		version = release.Name
	}

	return &versionInfo{
		Version:     version,
		TagName:     release.TagName,
		Prerelease:  release.Prerelease,
		PublishedAt: release.PublishedAt,
	}, nil
}

// parseGitHubReleaseVersion extracts version information from a GitHub tag name.
func parseGitHubReleaseVersion(tagName string) (*versionInfo, error) {
	// Ensure tag starts with 'v' for version
	version := tagName
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	// Validate version format (semver-like)
	// Supports: v1.2.3, v1.2.3-rc1, v1.2.3+build, v1.2.3-rc1+build
	versionPattern := regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-?[a-zA-Z0-9.]+)?(?:\+[a-zA-Z0-9.]+)?$`)
	if !versionPattern.MatchString(version) {
		return nil, fmt.Errorf("invalid version format: %s", version)
	}

	return &versionInfo{
		Version:    version,
		TagName:    tagName,
		Prerelease: strings.Contains(version, "-"),
	}, nil
}

// IsPrerelease checks if a version string represents a prerelease.
func IsPrerelease(version string) bool {
	// Remove leading 'v' if present
	v := strings.TrimPrefix(version, "v")
	// Prereleases contain a hyphen followed by identifiers (e.g., 1.6.0-rc1)
	return strings.Contains(v, "-")
}

// CompareVersions compares two version strings.
// Returns:
//   - -1 if current < latest
//   - 0 if current == latest
//   - 1 if current > latest
func CompareVersions(current, latest string) int {
	// Prefer strict semver ordering when both versions are valid semver strings.
	// This correctly handles prerelease progression (for example rc1 < rc2).
	if cmp, ok := compareSemver(current, latest); ok {
		return cmp
	}

	// Fall back to legacy comparison for non-semver values.
	return compareLegacyVersions(current, latest)
}

func compareSemver(current, latest string) (int, bool) {
	c := normalizeSemver(current)
	l := normalizeSemver(latest)
	if !semver.IsValid(c) || !semver.IsValid(l) {
		return 0, false
	}

	cmp := semver.Compare(c, l)
	if cmp < 0 {
		return -1, true
	}
	if cmp > 0 {
		return 1, true
	}
	return 0, true
}

func normalizeSemver(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return v
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

func compareLegacyVersions(current, latest string) int {
	// Normalize versions (remove 'v' prefix)
	c := strings.TrimPrefix(current, "v")
	l := strings.TrimPrefix(latest, "v")

	// Extract numeric parts
	cParts := strings.Split(c, ".")
	lParts := strings.Split(l, ".")

	// Pad shorter array with zeros
	for len(cParts) < 3 {
		cParts = append(cParts, "0")
	}
	for len(lParts) < 3 {
		lParts = append(lParts, "0")
	}

	for i := 0; i < 3; i++ {
		cNum, _ := parseInt(cParts[i])
		lNum, _ := parseInt(lParts[i])

		if cNum < lNum {
			return -1
		}
		if cNum > lNum {
			return 1
		}
	}

	// Same numeric version: stable release is newer than prerelease.
	cPre := IsPrerelease(c)
	lPre := IsPrerelease(l)
	if cPre && !lPre {
		return -1
	}
	if !cPre && lPre {
		return 1
	}

	return 0
}

func parseInt(s string) (int, error) {
	// Extract just the numeric prefix (for cases like "1-rc1")
	re := regexp.MustCompile(`^\d+`)
	match := re.FindString(s)
	if match == "" {
		return 0, fmt.Errorf("no numeric prefix in %s", s)
	}
	return parseStringToInt(match)
}

// helper function to parse string to int without error return for CompareVersions
func parseStringToInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// getLatestStableVersion checks GitHub and returns the latest stable version.
// Returns (version, isAvailable, error).
func getLatestStableVersion(ctx context.Context) (*versionInfo, error) {
	checker := newGitHubChecker("javinizer/Javinizer")
	return checker.CheckLatestVersion(ctx)
}

// checkForUpdate checks if an update is available and returns status.
// If checkPrerelease is true, prereleases will also be considered.
func checkForUpdate(ctx context.Context, currentVersion string, checkPrerelease bool) (*versionInfo, bool, error) {
	return checkForUpdateWithChecker(ctx, currentVersion, checkPrerelease, newGitHubChecker("javinizer/Javinizer"))
}

// checkForUpdateWithChecker checks if an update is available using a custom checker (for testing).
func checkForUpdateWithChecker(ctx context.Context, currentVersion string, checkPrerelease bool, chk *githubChecker) (*versionInfo, bool, error) {
	logging.Debugf("Checking for update (current: %s, checkPrerelease: %v)", currentVersion, checkPrerelease)

	latest, err := chk.CheckLatestVersion(ctx)
	if err != nil {
		return nil, false, err
	}

	// If not checking prereleases and latest is prerelease, skip it
	if !checkPrerelease && IsPrerelease(latest.Version) {
		// Try to get the latest non-prerelease
		foundStable := false
		if versions, err := chk.getRecentReleases(ctx, 10); err == nil {
			for _, v := range versions {
				if !IsPrerelease(v) {
					latest = &versionInfo{Version: v, Prerelease: false}
					foundStable = true
					break
				}
			}
		}
		// If no stable release was found (or the lookup failed), report no
		// update instead of falling through with the prerelease candidate.
		if !foundStable {
			return latest, false, nil
		}
	}

	// Compare versions
	cmp := CompareVersions(currentVersion, latest.Version)

	// If current < latest, an update is available
	if cmp < 0 {
		logging.Infof("Update available: %s (current: %s)", latest.Version, currentVersion)
		return latest, true, nil
	}

	logging.Debugf("No update available (current: %s, latest: %s)", currentVersion, latest.Version)
	return latest, false, nil
}

// getRecentReleases fetches recent releases to find a stable version.
// Uses the checker's apiBaseURL for testing flexibility.
func (c *githubChecker) getRecentReleases(ctx context.Context, limit int) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", c.apiBaseURL, c.repo, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Javinizer-Updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	// Check for GitHub token in environment
	if token := c.envLookup("GH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if token := c.envLookup("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer func() {
		_ = httpclient.DrainAndClose(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpdateResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var releases []struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	}

	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var versions []string
	for _, r := range releases {
		version := r.TagName
		if version == "" && r.Name != "" {
			version = r.Name
		}
		if version != "" {
			versions = append(versions, version)
		}
	}

	return versions, nil
}

// getRecentReleases fetches recent releases using the default checker.
func getRecentReleases(ctx context.Context, limit int) ([]string, error) {
	checker := newGitHubChecker("javinizer/Javinizer")
	return checker.getRecentReleases(ctx, limit)
}
