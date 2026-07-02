package update

import (
	"context"
	"encoding/json"
	"errors"
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

// defaultRepo is the GitHub repository (owner/repo) consulted for release
// updates. This is the Go rewrite (javinizer/javinizer-go), NOT the legacy
// Python project (javinizer/Javinizer) whose releases are unrelated. Hardcoded
// at every production construction site so a regression here can't silently
// point the update checker at the wrong project again.
const defaultRepo = "javinizer/javinizer-go"

// ErrNotModified is returned by CheckLatestVersion when GitHub responds 304
// (the cached ETag is still valid). 304 responses do NOT count against the
// GitHub rate limit, so callers can re-check freely. The service treats this as
// a "keep the cached state" signal rather than an error.
var ErrNotModified = errors.New("release data not modified since last check")

// versionInfo represents information about a GitHub release.
type versionInfo struct {
	Version     string `json:"version"`      // Human-readable version (e.g., "v1.6.0")
	TagName     string `json:"tag_name"`     // Git tag (e.g., "v1.6.0")
	Prerelease  bool   `json:"prerelease"`   // Whether this is a prerelease
	PublishedAt string `json:"published_at"` // ISO8601 timestamp
	// ETag captured from the GitHub response header. The service persists it
	// in the on-disk cache and sends it back as If-None-Match on the next
	// check so unchanged releases return 304 (rate-limit-free).
	ETag string `json:"-"`
	// NoStableLatest is true when this result came from the /releases list
	// fallback because /releases/latest 404'd (no stable latest release). The
	// service caches it so the next check skips the 404-throwing call.
	NoStableLatest bool `json:"-"`
}

// VersionInfo is the exported alias for versionInfo, enabling external callers
// (and tests in other packages) to construct values returned by a stub
// Checker. It is identical to versionInfo — no copy is made.
type VersionInfo = versionInfo

// Checker is the interface for checking the latest released version.
// It is exported so callers can inject a stub via NewServiceWithOptions,
// avoiding real network calls in hermetic tests.
type Checker interface {
	CheckLatestVersion(ctx context.Context) (*VersionInfo, error)
}

// ConditionalChecker is an OPTIONAL capability a Checker may implement to
// support rate-limit-friendly conditional requests. The service uses it to
// thread the cached ETag (→ If-None-Match, 304 is free) and the
// "no-stable-latest" flag (→ skip the /releases/latest 404) from the
// on-disk cache into the checker before a call. Stub checkers in tests need
// not implement it — the service type-asserts and silently falls back to a
// full fetch, so the interface stays backward-compatible.
type ConditionalChecker interface {
	SetIfNoneMatch(etag string)
	SetSkipLatest(skip bool)
}

// PreReleaseChecker is an OPTIONAL capability a Checker may implement to opt
// into prerelease discovery. When SetPreRelease(true) is called, the checker
// consults the /releases list endpoint (the single most recent release,
// including prereleases) instead of /releases/latest (stable only). The
// self-upgrade command uses this so a user on a stable release can opt into
// jumping to a newer prerelease with `javinizer upgrade --prerelease`. It is
// separate from ConditionalChecker so adding it cannot disturb the service
// layer's ETag/skipLatest probes (and the mockChecker that implements them).
type PreReleaseChecker interface {
	SetPreRelease(enable bool)
}

// checker is an alias retaining the original unexported name used internally.
type checker = Checker

// githubChecker checks versions from GitHub releases.
type githubChecker struct {
	repo       string
	httpClient *http.Client
	apiBaseURL string // For testing - override the GitHub API base URL
	envLookup  func(string) string
	// ifNonMatch, when non-empty, is sent as If-None-Match so GitHub returns
	// 304 (rate-limit-free) when releases are unchanged.
	ifNoneMatch string
	// skipLatest, when true, skips the /releases/latest call (which 404s for a
	// prerelease-only repo) and goes straight to the /releases list.
	skipLatest bool
	// preRelease, when true, always consults the /releases list (the newest
	// release, including prereleases) instead of /releases/latest (stable
	// only). Set via SetPreRelease by callers that want prerelease discovery.
	preRelease bool
}

// SetIfNoneMatch implements ConditionalChecker.
func (c *githubChecker) SetIfNoneMatch(etag string) { c.ifNoneMatch = etag }

// SetSkipLatest implements ConditionalChecker.
func (c *githubChecker) SetSkipLatest(skip bool) { c.skipLatest = skip }

// SetPreRelease implements PreReleaseChecker. When enabled, CheckLatestVersion
// uses the /releases list endpoint so the newest release (including
// prereleases) is returned, instead of /releases/latest (stable only).
func (c *githubChecker) SetPreRelease(enable bool) { c.preRelease = enable }

// newGitHubChecker creates a new GitHub checker.
// The repo should be in format "owner/repo" (e.g., "javinizer/javinizer-go").
// Production callers should pass defaultRepo; the parameter is kept for tests
// that point at a stub server with an arbitrary path segment.
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
//
// Rate-limit friendly: when the caller sets ifNoneMatch (via SetIfNoneMatch),
// it is sent as If-None-Match and an unchanged response returns ErrNotModified
// (GitHub 304s do NOT count against the rate limit). When skipLatest is set
// (via SetSkipLatest), the /releases/latest call — which 404s for a
// prerelease-only repo — is skipped entirely, going straight to the /releases
// list and halving API calls.
func (c *githubChecker) CheckLatestVersion(ctx context.Context) (*versionInfo, error) {
	// PreRelease opt-in: consult the /releases list so the newest release
	// (including prereleases) is returned, instead of /releases/latest which
	// excludes them. This lets a user on a stable release jump to a newer
	// prerelease via `javinizer upgrade --prerelease`. Inlined (rather than
	// reusing latestFromReleaseList) so the error context and the
	// NoStableLatest flag don't carry the 404-fallback framing that doesn't
	// apply when the list was chosen deliberately.
	if c.preRelease {
		versions, etag, err := c.getRecentReleases(ctx, 1)
		if err != nil {
			return nil, fmt.Errorf("prerelease release lookup failed: %w", err)
		}
		if len(versions) == 0 {
			return nil, fmt.Errorf("no releases found")
		}
		v := versions[0]
		return &versionInfo{Version: v, TagName: v, Prerelease: IsPrerelease(v), ETag: etag}, nil
	}
	// Skip the /releases/latest 404 we already know is coming for a
	// prerelease-only repo; go straight to the list endpoint.
	if c.skipLatest {
		return c.latestFromReleaseList(ctx)
	}

	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.apiBaseURL, c.repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setCommonHeaders(req)

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

	// Unchanged since the last check: return the sentinel so the service keeps
	// its cached state. 304 does not count against the rate limit.
	if resp.StatusCode == http.StatusNotModified {
		return nil, ErrNotModified
	}

	// GitHub's /releases/latest endpoint EXCLUDES prereleases, so a repo that
	// has only prereleases (the Go rewrite currently ships v0.x-alpha only)
	// responds 404. Fall back to the most recent release via the list endpoint
	// rather than erroring — otherwise the update checker would never find an
	// update for a prerelease-only repo. The returned version may be a
	// prerelease; the caller (service layer) applies the user's
	// check-prerelease preference to decide whether to surface it.
	if resp.StatusCode == http.StatusNotFound {
		return c.latestFromReleaseList(ctx)
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
		Version:        version,
		TagName:        release.TagName,
		Prerelease:     release.Prerelease,
		PublishedAt:    release.PublishedAt,
		ETag:           resp.Header.Get("ETag"),
		NoStableLatest: false,
	}, nil
}

// setCommonHeaders sets the headers shared by every GitHub API request:
// User-Agent, Accept, optional bearer auth (GH_TOKEN/GITHUB_TOKEN), and the
// conditional-request If-None-Match when an ETag is cached.
func (c *githubChecker) setCommonHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Javinizer-Updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token := c.envLookup("GH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if token := c.envLookup("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.ifNoneMatch != "" {
		req.Header.Set("If-None-Match", c.ifNoneMatch)
	}
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

// latestFromReleaseList fetches the single most recent release (which may be
// a prerelease) via the /releases list endpoint and wraps it as a versionInfo.
// It is the 404 fallback for CheckLatestVersion when no stable "latest"
// release exists, and the direct path when skipLatest is set. The returned
// versionInfo carries NoStableLatest=true so the service can cache that flag
// and skip the 404-throwing /releases/latest call on subsequent checks.
func (c *githubChecker) latestFromReleaseList(ctx context.Context) (*versionInfo, error) {
	versions, etag, err := c.getRecentReleases(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("no stable latest release (404) and list lookup failed: %w", err)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("GitHub API returned 404 for latest release and no releases were listed")
	}
	v := versions[0]
	return &versionInfo{
		Version:        v,
		TagName:        v,
		Prerelease:     IsPrerelease(v),
		ETag:           etag,
		NoStableLatest: true,
	}, nil
}

// getLatestStableVersion checks GitHub and returns the latest stable version.
// Returns (version, isAvailable, error).
func getLatestStableVersion(ctx context.Context) (*versionInfo, error) {
	checker := newGitHubChecker(defaultRepo)
	return checker.CheckLatestVersion(ctx)
}

// checkForUpdate checks if an update is available and returns status.
// If checkPrerelease is true, prereleases will also be considered.
func checkForUpdate(ctx context.Context, currentVersion string, checkPrerelease bool) (*versionInfo, bool, error) {
	return checkForUpdateWithChecker(ctx, currentVersion, checkPrerelease, newGitHubChecker(defaultRepo))
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
		if versions, _, err := chk.getRecentReleases(ctx, 10); err == nil {
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
// Uses the checker's apiBaseURL for testing flexibility. Returns the parsed
// versions plus the response ETag (for subsequent If-None-Match requests).
// Returns ErrNotModified (with no versions) when GitHub responds 304 — the
// caller should keep its cached result in that case.
func (c *githubChecker) getRecentReleases(ctx context.Context, limit int) ([]string, string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", c.apiBaseURL, c.repo, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	c.setCommonHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer func() {
		_ = httpclient.DrainAndClose(resp.Body)
	}()

	// Unchanged since the last check: 304 does not count against the rate
	// limit. Surface the sentinel so the caller keeps its cached result.
	if resp.StatusCode == http.StatusNotModified {
		return nil, "", ErrNotModified
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", fmt.Errorf("rate limited by GitHub API (status: %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpdateResponseSize))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	var releases []struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	}

	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, "", fmt.Errorf("failed to parse response: %w", err)
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

	return versions, resp.Header.Get("ETag"), nil
}

// getRecentReleases fetches recent releases using the default checker.
func getRecentReleases(ctx context.Context, limit int) ([]string, error) {
	checker := newGitHubChecker(defaultRepo)
	versions, _, err := checker.getRecentReleases(ctx, limit)
	return versions, err
}
