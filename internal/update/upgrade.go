package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/system"
)

const (
	// defaultDownloadBase is the host that serves release assets
	// (github.com/.../releases/download/...). It is distinct from the GitHub
	// API host (api.github.com) used by the checker.
	defaultDownloadBase = "https://github.com"
	// maxAssetSize caps how much we will download/write for a release binary,
	// guarding against a malformed or hostile response streaming forever.
	maxAssetSize = 256 * 1024 * 1024 // 256MB
	// maxChecksumSize caps the checksums.txt download.
	maxChecksumSize = 1 * 1024 * 1024 // 1MB
)

// InstallMethod describes how the running binary was installed, so the
// self-upgrade can defer to the right package manager instead of clobbering a
// managed install.
type InstallMethod int

const (
	// InstallMethodManual covers a direct binary download or build-from-source.
	// These are safe to self-replace in place.
	InstallMethodManual InstallMethod = iota
	// InstallMethodHomebrew means the binary lives under a Homebrew Cellar;
	// self-replacing it would diverge from brew's bookkeeping, so we hand off.
	InstallMethodHomebrew
	// InstallMethodScoop means the binary lives under a Scoop apps dir;
	// self-replacing it would diverge from scoop's manifest, so we hand off.
	InstallMethodScoop
)

// String returns a human-readable label for the install method.
func (m InstallMethod) String() string {
	switch m {
	case InstallMethodHomebrew:
		return "homebrew"
	case InstallMethodScoop:
		return "scoop"
	default:
		return "manual"
	}
}

// AssetName returns the release asset filename for the given GOOS/GOARCH.
// macOS uses the universal binary (one asset for both arches); linux is
// arch-specific; windows publishes only amd64 today. This must stay in sync
// with the asset names produced by .github/workflows/cli-release.yml
// (javinizer-<os>-<arch>).
func AssetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		// A universal binary is published for darwin, so arch is irrelevant.
		return "javinizer-darwin-universal", nil
	case "linux":
		switch goarch {
		case "amd64", "arm64":
			return "javinizer-linux-" + goarch, nil
		}
	case "windows":
		// Only windows-amd64 is published by CI today; windows-arm64 would 404.
		if goarch == "amd64" {
			return "javinizer-windows-amd64.exe", nil
		}
	}
	return "", fmt.Errorf("no release asset for %s/%s", goos, goarch)
}

// DetectInstallMethod inspects the running executable path to guess how
// javinizer was installed. It is intentionally conservative: only paths that
// clearly belong to a package manager are flagged as such, so a manual install
// that happens to live somewhere unusual still gets the self-replace path.
func DetectInstallMethod(exePath string) InstallMethod {
	p := strings.ToLower(filepath.ToSlash(exePath))
	if strings.Contains(p, "/cellar/javinizer") || strings.Contains(p, "/linuxbrew/") && strings.Contains(p, "/cellar/") {
		return InstallMethodHomebrew
	}
	if strings.Contains(p, "/scoop/apps/javinizer") {
		return InstallMethodScoop
	}
	return InstallMethodManual
}

// ParseChecksums extracts the SHA256 hash for assetName from the contents of a
// checksums.txt file (sha256sum output: "<hash>  <name>" or "<hash> *<name>").
// Returns an error if the asset is not listed.
func ParseChecksums(data []byte, assetName string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		for _, f := range fields[1:] {
			if f == assetName || strings.TrimPrefix(f, "*") == assetName {
				return hash, nil
			}
		}
	}
	return "", fmt.Errorf("checksum for %s not found in checksums.txt", assetName)
}

// UpgradeOptions configures an Upgrade run. All fields are optional: a
// zero-value UpgradeOptions (with a non-empty CurrentVersion) performs a real
// upgrade against the live GitHub release. Tests inject an HTTPClient pointed
// at an httptest server and/or a stub Checker to avoid the network.
type UpgradeOptions struct {
	// CurrentVersion is the running version (e.g. version.Short()). Required.
	CurrentVersion string
	// Force upgrades even when already at the latest version.
	Force bool
	// CheckOnly reports whether an update is available without downloading.
	CheckOnly bool
	// PreRelease, when true, discovers the newest release including
	// prereleases (via the /releases list) instead of the latest stable
	// (/releases/latest). Lets a user on stable opt into a newer prerelease.
	PreRelease bool
	// Repo overrides the GitHub owner/repo (default defaultRepo).
	Repo string
	// APIBaseURL overrides the GitHub API base (tests).
	APIBaseURL string
	// DownloadBaseURL overrides the release-asset host (tests).
	DownloadBaseURL string
	// HTTPClient overrides the HTTP client used for downloads + API.
	HTTPClient *http.Client
	// Checker overrides the release checker (tests). When nil, a real
	// githubChecker is constructed from Repo/APIBaseURL.
	Checker Checker
	// ExePath overrides the running-binary path (default os.Executable()).
	ExePath string
	// Out receives progress messages (default os.Stdout).
	Out io.Writer
	// Environment classifies how javinizer is running (docker/desktop/cli),
	// set by the caller (cmd layer) via system.DetectEnvironment. The library
	// can't import internal/desktop to compute it itself (import cycle), so the
	// command layer injects it. Docker and desktop builds can't self-swap and
	// get an environment-specific handoff instead. Zero value ("") behaves as
	// CLI to keep backward compatibility for callers that don't set it.
	Environment system.Environment
}

// UpgradeResult is the outcome of an Upgrade run.
type UpgradeResult struct {
	CurrentVersion string
	LatestVersion  string
	TagName        string
	AssetName      string
	UpToDate       bool
	Upgraded       bool
	// Handoff is true when the install is managed by a package manager OR the
	// running environment can't self-swap (docker image / desktop bundle), and
	// the caller was told the right upgrade command instead.
	Handoff            bool
	InstallMethod      InstallMethod
	InstallEnvironment system.Environment
}

// Upgrade checks for, downloads, and applies the latest release, replacing the
// running binary in place. It never touches a Homebrew/Scoop-managed install —
// for those it prints the correct upgrade command and returns Handoff=true.
func Upgrade(ctx context.Context, opts UpgradeOptions) (*UpgradeResult, error) {
	opts = resolveUpgradeDefaults(opts)
	out := opts.Out

	if opts.CurrentVersion == "" {
		return nil, errors.New("current version is required")
	}

	chk := opts.Checker
	if chk == nil {
		chk = newGitHubCheckerWithBaseURL(opts.Repo, opts.APIBaseURL)
	}
	// Thread the prerelease opt-in into the real checker. Stub checkers in
	// tests need not implement PreReleaseChecker — the assert fails silently
	// and the stub returns whatever it is configured to return.
	if opts.PreRelease {
		if pc, ok := chk.(PreReleaseChecker); ok {
			pc.SetPreRelease(true)
		}
	}

	logging.Infof("Checking for the latest release (current: %s)", opts.CurrentVersion)
	latest, err := chk.CheckLatestVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check latest release: %w", err)
	}

	result := &UpgradeResult{
		CurrentVersion: opts.CurrentVersion,
		LatestVersion:  latest.Version,
		TagName:        latest.TagName,
	}
	if result.TagName == "" {
		result.TagName = latest.Version
	}

	cmp := CompareVersions(opts.CurrentVersion, latest.Version)
	result.UpToDate = cmp >= 0

	if opts.CheckOnly {
		if result.UpToDate {
			_, _ = fmt.Fprintf(out, "Already up to date: %s\n", opts.CurrentVersion)
		} else {
			_, _ = fmt.Fprintf(out, "Update available: %s (current: %s)\n", latest.Version, opts.CurrentVersion)
		}
		return result, nil
	}

	if result.UpToDate && !opts.Force {
		_, _ = fmt.Fprintf(out, "Already up to date: %s\n", opts.CurrentVersion)
		return result, nil
	}

	// Record the running environment on the result so callers (and tests) can
	// see which path was taken even when no swap happens.
	result.InstallEnvironment = opts.Environment

	// Environment-aware handoff: docker images are read-only (an in-place
	// binary replace would be lost on container recreate), and desktop builds
	// perform a bundle-level self-swap via the in-app "Update & restart" button
	// rather than via this CLI command. In both cases, defer to the right
	// upgrade path instead of attempting a CLI swap that would silently fail
	// or break the install. This runs after the up-to-date check so a no-op
	// upgrade still reports "already up to date" without a handoff.
	if opts.Environment == system.EnvironmentDocker || opts.Environment == system.EnvironmentDesktop {
		result.Handoff = true
		_, _ = fmt.Fprintf(out, "%s\n", system.UpgradeInstructions(opts.Environment))
		return result, nil
	}

	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	result.AssetName = asset

	exePath, err := resolveExePath(opts.ExePath)
	if err != nil {
		return nil, fmt.Errorf("locate running binary: %w", err)
	}
	method := DetectInstallMethod(exePath)
	result.InstallMethod = method
	switch method {
	case InstallMethodHomebrew:
		result.Handoff = true
		_, _ = fmt.Fprintf(out, "Javinizer was installed with Homebrew.\n")
		_, _ = fmt.Fprintf(out, "Run `brew upgrade javinizer` to update (self-upgrade would break brew's bookkeeping).\n")
		return result, nil
	case InstallMethodScoop:
		result.Handoff = true
		_, _ = fmt.Fprintf(out, "Javinizer was installed with Scoop.\n")
		_, _ = fmt.Fprintf(out, "Run `scoop update javinizer` to update (self-upgrade would break scoop's manifest).\n")
		return result, nil
	}

	if result.UpToDate && opts.Force {
		_, _ = fmt.Fprintf(out, "Reinstalling %s (forced)...\n", latest.Version)
	} else {
		_, _ = fmt.Fprintf(out, "Upgrading %s -> %s\n", opts.CurrentVersion, latest.Version)
	}
	if latest.Prerelease || IsPrerelease(latest.Version) {
		_, _ = fmt.Fprintf(out, "Note: %s is a prerelease.\n", latest.Version)
	}

	if err := downloadAndReplace(ctx, opts, exePath, result.TagName, asset); err != nil {
		return result, err
	}

	result.Upgraded = true
	_, _ = fmt.Fprintf(out, "Upgraded to %s. Restart any running javinizer processes to use the new version.\n", latest.Version)
	return result, nil
}

// resolveUpgradeDefaults fills in zero-value fields with production defaults.
func resolveUpgradeDefaults(opts UpgradeOptions) UpgradeOptions {
	if opts.Repo == "" {
		opts.Repo = defaultRepo
	}
	if opts.APIBaseURL == "" {
		opts.APIBaseURL = "https://api.github.com"
	}
	if opts.DownloadBaseURL == "" {
		opts.DownloadBaseURL = defaultDownloadBase
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	return opts
}

// resolveExePath returns the absolute path of the running binary, following
// symlinks so the replacement targets the real file (not a /usr/local/bin
// symlink whose target lives elsewhere).
func resolveExePath(override string) (string, error) {
	p := override
	if p == "" {
		var err error
		p, err = os.Executable()
		if err != nil {
			return "", err
		}
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		// Fall back to the raw path if symlink resolution fails (e.g. the file
		// was just replaced); the caller still gets a usable target.
		//nolint:nilerr // intentional fallback: a resolution error is not fatal here.
		return p, nil
	}
	return resolved, nil
}

// downloadAndReplace fetches the checksums and the asset, verifies the SHA256,
// and atomically swaps the binary into place.
func downloadAndReplace(ctx context.Context, opts UpgradeOptions, exePath, tag, asset string) error {
	out := opts.Out
	dir := filepath.Dir(exePath)

	assetURL := fmt.Sprintf("%s/%s/releases/download/%s/%s", opts.DownloadBaseURL, opts.Repo, tag, asset)
	checksumURL := fmt.Sprintf("%s/%s/releases/download/%s/checksums.txt", opts.DownloadBaseURL, opts.Repo, tag)

	_, _ = fmt.Fprintf(out, "Downloading checksums...\n")
	var checksums bytes.Buffer
	if err := DownloadTo(ctx, opts.HTTPClient, checksumURL, &checksums, maxChecksumSize); err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	expected, err := ParseChecksums(checksums.Bytes(), asset)
	if err != nil {
		return err
	}

	// Write the asset to a temp file in the same directory as the running
	// binary so the final rename is atomic (same filesystem). A cross-device
	// rename would fail with EXDEV.
	tmp, err := os.CreateTemp(dir, ".javinizer-upgrade-*.tmp")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("no write permission to %s — re-run with sudo, or install via Homebrew/Scoop", dir)
		}
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	// Ensure the temp file is removed if anything below fails.
	defer func() {
		if err := tmp.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			logging.Debugf("closing temp upgrade file: %v", err)
		}
		_ = os.Remove(tmpPath)
	}()

	_, _ = fmt.Fprintf(out, "Downloading %s...\n", asset)
	if err := DownloadTo(ctx, opts.HTTPClient, assetURL, tmp, maxAssetSize); err != nil {
		return fmt.Errorf("download asset: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("flush asset: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close asset: %w", err)
	}

	if err := VerifyFileSHA256(tmpPath, expected); err != nil {
		return err
	}

	// The binary must be executable before it takes the target path.
	//nolint:gosec // G302: the downloaded binary must be executable (0o755) to run.
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod asset: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Installing...\n")
	if err := ReplaceBinary(exePath, tmpPath); err != nil {
		return err
	}
	return nil
}

// DownloadTo streams a URL into w, capping the total bytes read at maxSize.
// It enforces HTTPS end to end: the initial URL and every redirect must be
// https, because a checksum fetched over the same insecure channel as the
// binary authenticates nothing (a MITM could swap both). An oversized response
// fails closed rather than being silently truncated into a partial parse.
func DownloadTo(ctx context.Context, client *http.Client, url string, w io.Writer, maxSize int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing non-HTTPS download URL: %s", req.URL.Redacted())
	}
	req.Header.Set("User-Agent", "Javinizer-Updater")
	req.Header.Set("Accept", "application/octet-stream")

	safeClient := *client
	baseRedirect := safeClient.CheckRedirect
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-HTTPS redirect URL: %s", req.URL.Redacted())
		}
		if baseRedirect != nil {
			return baseRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}

	resp, err := safeClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = httpclient.DrainAndClose(resp.Body) }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if n > maxSize {
		return fmt.Errorf("response exceeds maximum size of %d bytes for %s", maxSize, url)
	}
	return nil
}

// VerifyFileSHA256 reads the file at path and compares its SHA256 to expected.
func VerifyFileSHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open downloaded asset: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, maxAssetSize)); err != nil {
		return fmt.Errorf("hash asset: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s — refusing to install", expected, actual)
	}
	logging.Debugf("Checksum verified for %s", filepath.Base(path))
	return nil
}

// ReplaceBinary atomically replaces the binary at target with the file at
// source. source and target must be on the same filesystem.
//
// On Unix, os.Rename atomically swaps the file even when target is the running
// binary (the running process keeps the old inode).
//
// On Windows, the running exe is locked and cannot be overwritten in place, so
// the running binary is renamed to "<target>.old" (Windows permits renaming a
// running exe) before the new file is moved into place. The .old file is
// removed best-effort; if it is still locked it is left for a later run.
//
// The Windows path is split into replaceBinaryWindows so it can be unit-tested
// on any OS (it uses only os.Rename/os.Remove, which are cross-platform);
// ReplaceBinary dispatches on runtime.GOOS at runtime.
func ReplaceBinary(target, source string) error {
	if runtime.GOOS == "windows" {
		return replaceBinaryWindows(target, source)
	}
	return replaceBinaryUnix(target, source)
}

// replaceBinaryUnix swaps source over target via a single atomic rename.
func replaceBinaryUnix(target, source string) error {
	if err := os.Rename(source, target); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied replacing %s — re-run with sudo, or install via Homebrew/Scoop", target)
		}
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

// replaceBinaryWindows implements the Windows running-exe replacement.
//
// It self-heals a prior interrupted upgrade: if target is missing but a .old
// backup exists, the backup is restored first — otherwise the os.Remove(old)
// below would delete the only good copy and brick the install permanently.
//
// If the new-binary rename fails, the previous binary is rolled back from .old;
// if the rollback ALSO fails, the error is surfaced (not swallowed) and names
// the .old path so the user can recover manually.
func replaceBinaryWindows(target, source string) error {
	old := target + ".old"

	// Self-heal from a prior interrupted upgrade: restore .old -> target so the
	// remove+rename below has a valid binary to work with and doesn't delete the
	// only good copy.
	if _, err := os.Stat(target); os.IsNotExist(err) {
		if _, err := os.Stat(old); err == nil {
			if err := os.Rename(old, target); err != nil {
				return fmt.Errorf("restore previous binary from .old: %w", err)
			}
		}
	}

	_ = os.Remove(old) // best effort; may be locked from a prior run
	if err := os.Rename(target, old); err != nil {
		return fmt.Errorf("rename current binary to .old: %w", err)
	}
	if err := os.Rename(source, target); err != nil {
		// Rollback: restore the previous binary. If the rollback ALSO fails,
		// surface it (don't silently leave no binary at target) and point the
		// user at the .old file for manual recovery.
		if rbErr := os.Rename(old, target); rbErr != nil {
			return fmt.Errorf("install new binary: %w (rollback also failed: %v; previous binary preserved at %s — rename it back manually)", err, rbErr, old)
		}
		return fmt.Errorf("install new binary: %w", err)
	}
	_ = os.Remove(old) // best effort
	return nil
}
