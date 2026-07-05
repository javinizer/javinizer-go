package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubChecker returns a fixed latest version without touching the network.
type stubChecker struct {
	version string
	err     error
}

func (s *stubChecker) CheckLatestVersion(_ context.Context) (*VersionInfo, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &VersionInfo{Version: s.version, TagName: s.version}, nil
}

func TestAssetName(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		goarch  string
		want    string
		wantErr bool
	}{
		{"darwin universal", "darwin", "amd64", "javinizer-darwin-universal", false},
		{"darwin arm64 still universal", "darwin", "arm64", "javinizer-darwin-universal", false},
		{"linux amd64", "linux", "amd64", "javinizer-linux-amd64", false},
		{"linux arm64", "linux", "arm64", "javinizer-linux-arm64", false},
		{"windows amd64", "windows", "amd64", "javinizer-windows-amd64.exe", false},
		{"windows arm64 unsupported (no CI build)", "windows", "arm64", "", true},
		{"linux 386 unsupported", "linux", "386", "", true},
		{"freebsd unsupported", "freebsd", "amd64", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AssetName(tc.goos, tc.goarch)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDetectInstallMethod(t *testing.T) {
	tests := []struct {
		name string
		path string
		want InstallMethod
	}{
		{"homebrew cellar", "/opt/homebrew/Cellar/javinizer/1.0.0/bin/javinizer", InstallMethodHomebrew},
		{"linuxbrew cellar", "/home/linuxbrew/.linuxbrew/Cellar/javinizer/1.0.0/bin/javinizer", InstallMethodHomebrew},
		{"scoop apps", "C:/Users/me/scoop/apps/javinizer/current/javinizer.exe", InstallMethodScoop},
		{"manual usr local", "/usr/local/bin/javinizer", InstallMethodManual},
		{"manual home bin", "/home/choong/bin/javinizer", InstallMethodManual},
		{"manual windows", "C:/Tools/javinizer.exe", InstallMethodManual},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DetectInstallMethod(tc.path))
		})
	}
}

func TestInstallMethodString(t *testing.T) {
	tests := []struct {
		method InstallMethod
		want   string
	}{
		{InstallMethodManual, "manual"},
		{InstallMethodHomebrew, "homebrew"},
		{InstallMethodScoop, "scoop"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.method.String())
		})
	}
}

func TestParseChecksums(t *testing.T) {
	const asset = "javinizer-linux-amd64"
	tests := []struct {
		name    string
		data    string
		want    string
		wantErr bool
	}{
		{"two-space format", "abc123  javinizer-linux-amd64\n", "abc123", false},
		{"star format", "abc123 *javinizer-linux-amd64\n", "abc123", false},
		{"multiple entries", "deadbeef  javinizer-darwin-universal\nabc123  javinizer-linux-amd64\nfff  javinizer-windows-amd64.exe\n", "abc123", false},
		{"missing asset", "deadbeef  javinizer-darwin-universal\n", "", true},
		{"empty", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseChecksums([]byte(tc.data), asset)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestReplaceBinary_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only replace path")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer")
	source := filepath.Join(dir, "new")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(source, []byte("new"), 0o755))

	require.NoError(t, ReplaceBinary(target, source))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	_, err = os.Stat(source)
	assert.True(t, os.IsNotExist(err), "source should have been renamed away")
}

func TestUpgrade_CheckOnly_Available(t *testing.T) {
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v0.0.1",
		CheckOnly:      true,
		Checker:        &stubChecker{version: "v9.9.9"},
		Out:            &out,
	})
	require.NoError(t, err)
	assert.False(t, res.UpToDate)
	assert.False(t, res.Upgraded)
	assert.Contains(t, out.String(), "Update available: v9.9.9")
}

func TestUpgrade_CheckOnly_UpToDate(t *testing.T) {
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v9.9.9",
		CheckOnly:      true,
		Checker:        &stubChecker{version: "v9.9.9"},
		Out:            &out,
	})
	require.NoError(t, err)
	assert.True(t, res.UpToDate)
	assert.False(t, res.Upgraded)
	assert.Contains(t, out.String(), "Already up to date")
}

func TestUpgrade_HomebrewHandoff(t *testing.T) {
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v0.0.1",
		Checker:        &stubChecker{version: "v9.9.9"},
		ExePath:        "/opt/homebrew/Cellar/javinizer/1.0.0/bin/javinizer",
		Out:            &out,
	})
	require.NoError(t, err)
	assert.True(t, res.Handoff)
	assert.False(t, res.Upgraded)
	assert.Equal(t, InstallMethodHomebrew, res.InstallMethod)
	assert.Contains(t, out.String(), "brew upgrade javinizer")
}

// TestUpgrade_DockerHandoff verifies a docker build never attempts an in-place
// self-swap (the image is read-only; the replace would be lost on recreate).
// Instead it hands off with the `docker pull` instruction. The exe path is a
// manual install path to prove the docker check short-circuits BEFORE the
// brew/scoop/manual install-method detection.
func TestUpgrade_DockerHandoff(t *testing.T) {
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v0.0.1",
		Checker:        &stubChecker{version: "v9.9.9"},
		ExePath:        "/usr/local/bin/javinizer",
		Environment:    system.EnvironmentDocker,
		Out:            &out,
	})
	require.NoError(t, err)
	assert.True(t, res.Handoff, "docker must hand off, not self-swap")
	assert.False(t, res.Upgraded)
	assert.Equal(t, system.EnvironmentDocker, res.InstallEnvironment)
	assert.Contains(t, out.String(), "docker pull")
	assert.Contains(t, out.String(), "ghcr.io/javinizer/javinizer-go")
}

// TestUpgrade_DesktopHandoff verifies a desktop bundle never attempts an
// inner-binary swap (it would orphan the .app/.exe/.AppImage wrapper). It
// hands off pointing at the GitHub releases page instead.
func TestUpgrade_DesktopHandoff(t *testing.T) {
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v0.0.1",
		Checker:        &stubChecker{version: "v9.9.9"},
		ExePath:        "/Applications/Javinizer.app/Contents/MacOS/javinizer",
		Environment:    system.EnvironmentDesktop,
		Out:            &out,
	})
	require.NoError(t, err)
	assert.True(t, res.Handoff, "desktop must hand off, not self-swap")
	assert.False(t, res.Upgraded)
	assert.Equal(t, system.EnvironmentDesktop, res.InstallEnvironment)
	assert.Contains(t, out.String(), "releases")
}

// TestUpgrade_DockerUpToDateNoHandoff confirms the environment handoff only
// fires when an upgrade would actually happen — an up-to-date docker install
// reports "already up to date" without telling the user to docker pull.
func TestUpgrade_DockerUpToDateNoHandoff(t *testing.T) {
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v9.9.9",
		Checker:        &stubChecker{version: "v9.9.9"},
		Environment:    system.EnvironmentDocker,
		Out:            &out,
	})
	require.NoError(t, err)
	assert.False(t, res.Handoff, "up-to-date must not hand off")
	assert.True(t, res.UpToDate)
	assert.Contains(t, out.String(), "Already up to date")
}

// newAssetServer serves a release asset plus its checksums.txt from a fake
// release download tree over TLS. DownloadTo enforces HTTPS, so test servers
// must be TLS; server.Client() returns a client that trusts the test cert.
func newAssetServer(t *testing.T, assetBytes []byte, checksums string) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(checksums))
		case strings.HasSuffix(r.URL.Path, "javinizer-darwin-universal"),
			strings.HasSuffix(r.URL.Path, "javinizer-linux-amd64"),
			strings.HasSuffix(r.URL.Path, "javinizer-linux-arm64"),
			strings.HasSuffix(r.URL.Path, "javinizer-windows-amd64.exe"):
			_, _ = w.Write(assetBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestUpgrade_FullReplace(t *testing.T) {
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)

	assetBytes := []byte("new-binary-content")
	sum := sha256.Sum256(assetBytes)
	checksums := fmt.Sprintf("%x  %s\n", sum, asset)
	server := newAssetServer(t, assetBytes, checksums)
	defer server.Close()

	exePath := filepath.Join(t.TempDir(), "javinizer")
	require.NoError(t, os.WriteFile(exePath, []byte("old"), 0o755))

	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion:  "v0.0.1",
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Checker:         &stubChecker{version: "v9.9.9"},
		ExePath:         exePath,
		Out:             &out,
	})
	require.NoError(t, err)
	assert.True(t, res.Upgraded)
	assert.Equal(t, "v9.9.9", res.LatestVersion)
	assert.Equal(t, asset, res.AssetName)

	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "new-binary-content", string(got))
}

func TestUpgrade_ChecksumMismatch_Aborts(t *testing.T) {
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)

	assetBytes := []byte("new-binary-content")
	// Deliberately wrong checksum.
	checksums := fmt.Sprintf("deadbeef  %s\n", asset)
	server := newAssetServer(t, assetBytes, checksums)
	defer server.Close()

	exePath := filepath.Join(t.TempDir(), "javinizer")
	require.NoError(t, os.WriteFile(exePath, []byte("old"), 0o755))

	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion:  "v0.0.1",
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Checker:         &stubChecker{version: "v9.9.9"},
		ExePath:         exePath,
		Out:             &out,
	})
	require.Error(t, err)
	assert.False(t, res.Upgraded)
	assert.Contains(t, err.Error(), "checksum mismatch")

	// The running binary must be untouched on verification failure.
	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "old", string(got))
}

func TestUpgrade_Force_ReinstallLatest(t *testing.T) {
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)

	assetBytes := []byte("reinstalled")
	sum := sha256.Sum256(assetBytes)
	checksums := fmt.Sprintf("%x  %s\n", sum, asset)
	server := newAssetServer(t, assetBytes, checksums)
	defer server.Close()

	exePath := filepath.Join(t.TempDir(), "javinizer")
	require.NoError(t, os.WriteFile(exePath, []byte("old"), 0o755))

	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion:  "v9.9.9", // same as latest — would be "up to date" without --force
		Force:           true,
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Checker:         &stubChecker{version: "v9.9.9"},
		ExePath:         exePath,
		Out:             &out,
	})
	require.NoError(t, err)
	assert.True(t, res.Upgraded)
	assert.Contains(t, out.String(), "forced")

	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "reinstalled", string(got))
}

func TestUpgrade_ScoopHandoff(t *testing.T) {
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v0.0.1",
		Checker:        &stubChecker{version: "v9.9.9"},
		ExePath:        "C:/Users/me/scoop/apps/javinizer/current/javinizer.exe",
		Out:            &out,
	})
	require.NoError(t, err)
	assert.True(t, res.Handoff)
	assert.False(t, res.Upgraded)
	assert.Equal(t, InstallMethodScoop, res.InstallMethod)
	assert.Contains(t, out.String(), "scoop update javinizer")
}

func TestUpgrade_AlreadyUpToDate_NonCheckPath(t *testing.T) {
	// The non-check-only "already up to date" branch (result.UpToDate && !Force,
	// CheckOnly == false) was previously unexercised — TestUpgrade_CheckOnly_upToDate
	// uses CheckOnly: true.
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v9.9.9",
		Checker:        &stubChecker{version: "v9.9.9"},
		ExePath:        filepath.Join(t.TempDir(), "javinizer"),
		Out:            &out,
	})
	require.NoError(t, err)
	assert.True(t, res.UpToDate)
	assert.False(t, res.Upgraded)
	assert.Contains(t, out.String(), "Already up to date")
}

func TestUpgrade_ChecksumsDownloadFailure_Aborts(t *testing.T) {
	// A release missing checksums.txt (the asset is served but checksums 404)
	// must abort without touching the running binary.
	assetBytes := []byte("new-binary-content")
	sum := sha256.Sum256(assetBytes)
	// Server serves the asset but 404s checksums.txt.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(assetBytes)
	}))
	defer server.Close()
	_ = sum

	exePath := filepath.Join(t.TempDir(), "javinizer")
	require.NoError(t, os.WriteFile(exePath, []byte("old"), 0o755))

	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion:  "v0.0.1",
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Checker:         &stubChecker{version: "v9.9.9"},
		ExePath:         exePath,
		Out:             &out,
	})
	require.Error(t, err)
	assert.False(t, res.Upgraded)
	assert.Contains(t, err.Error(), "download checksums")

	// Running binary untouched.
	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "old", string(got))
}

func TestReplaceBinary_Windows_Success(t *testing.T) {
	// replaceBinaryWindows uses only os.Rename/os.Remove, so it is testable on
	// any OS despite the runtime.GOOS gate in ReplaceBinary.
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer.exe")
	source := filepath.Join(dir, "new.exe")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(source, []byte("new"), 0o755))

	require.NoError(t, replaceBinaryWindows(target, source))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	// The .old backup must be cleaned up on success.
	_, err = os.Stat(target + ".old")
	assert.True(t, os.IsNotExist(err), ".old should be removed on success")
}

func TestReplaceBinary_Windows_RollbackOnFailure(t *testing.T) {
	// If the new-binary rename fails (source missing), the previous binary is
	// restored to target and an error is returned — the install is not bricked.
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer.exe")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o755))
	// No source file -> the second os.Rename(source, target) will fail.

	err := replaceBinaryWindows(target, filepath.Join(dir, "missing.exe"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install new binary")

	// Previous binary restored.
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "old", string(got))
}

func TestReplaceBinary_Windows_SelfHealFromBrick(t *testing.T) {
	// Simulate a prior interrupted upgrade: target is missing, only .old exists.
	// replaceBinaryWindows must restore .old -> target (so os.Remove(old) below
	// doesn't delete the only good copy) and then complete the upgrade.
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer.exe")
	old := target + ".old"
	source := filepath.Join(dir, "new.exe")
	require.NoError(t, os.WriteFile(old, []byte("previous"), 0o755))
	require.NoError(t, os.WriteFile(source, []byte("new"), 0o755))

	require.NoError(t, replaceBinaryWindows(target, source))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	_, err = os.Stat(old)
	assert.True(t, os.IsNotExist(err), ".old should be removed after self-heal upgrade")
}

// prereleaseAwareChecker is a stubChecker that also implements PreReleaseChecker,
// recording whether the caller opted into prerelease discovery.
type prereleaseAwareChecker struct {
	stubChecker
	gotPreRelease bool
}

func (p *prereleaseAwareChecker) SetPreRelease(enable bool) { p.gotPreRelease = enable }

func TestUpgrade_PreRelease_FlowsToChecker(t *testing.T) {
	stub := &prereleaseAwareChecker{stubChecker: stubChecker{version: "v1.1.0-rc1"}}
	var out bytes.Buffer
	_, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v1.0.0",
		PreRelease:     true,
		CheckOnly:      true,
		Checker:        stub,
		Out:            &out,
	})
	require.NoError(t, err)
	assert.True(t, stub.gotPreRelease, "PreRelease must be threaded to the checker via SetPreRelease")
	assert.Contains(t, out.String(), "v1.1.0-rc1")
}

func TestUpgrade_PreRelease_NotSetByDefault(t *testing.T) {
	stub := &prereleaseAwareChecker{stubChecker: stubChecker{version: "v1.1.0"}}
	var out bytes.Buffer
	_, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v1.0.0",
		CheckOnly:      true,
		Checker:        stub,
		Out:            &out,
	})
	require.NoError(t, err)
	assert.False(t, stub.gotPreRelease, "PreRelease must not be set by default")
}

func TestUpgrade_PreRelease_AllowsPrereleaseUpgrade(t *testing.T) {
	// End-to-end: with PreRelease, a newer prerelease is installed through the
	// full download/verify/replace path (the prerelease tag flows into the
	// download URL). Confirms prereleases are accepted, not rejected, when opted in.
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)

	assetBytes := []byte("rc-binary")
	sum := sha256.Sum256(assetBytes)
	checksums := fmt.Sprintf("%x  %s\n", sum, asset)
	server := newAssetServer(t, assetBytes, checksums)
	defer server.Close()

	exePath := filepath.Join(t.TempDir(), "javinizer")
	require.NoError(t, os.WriteFile(exePath, []byte("old"), 0o755))

	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion:  "v1.0.0",
		PreRelease:      true,
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Checker:         &stubChecker{version: "v1.1.0-rc1"},
		ExePath:         exePath,
		Out:             &out,
	})
	require.NoError(t, err)
	assert.True(t, res.Upgraded)
	assert.Equal(t, "v1.1.0-rc1", res.LatestVersion)
	assert.Contains(t, out.String(), "prerelease")

	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "rc-binary", string(got))
}

func TestDownloadTo_RefusesNonHTTPS(t *testing.T) {
	// DownloadTo must refuse a plain-HTTP URL outright: a checksum fetched over
	// the same insecure channel as the binary authenticates nothing.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := DownloadTo(context.Background(), server.Client(), server.URL+"/asset", &buf, 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-HTTPS")
	assert.Empty(t, buf.Bytes())
}

func TestDownloadTo_RefusesHTTPRedirect(t *testing.T) {
	// A TLS server that redirects to a plain-HTTP target: the upgrade must
	// refuse to follow the redirect rather than fetch the asset insecurely.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("insecure"))
	}))
	defer target.Close()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/asset", http.StatusFound)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := DownloadTo(context.Background(), server.Client(), server.URL+"/asset", &buf, 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-HTTPS redirect")
	assert.Empty(t, buf.Bytes())
}

func TestDownloadTo_ErrorsWhenExceedingSizeCap(t *testing.T) {
	// An oversized response must fail closed rather than be silently truncated
	// into a partial (and potentially valid-looking) parse.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := DownloadTo(context.Background(), server.Client(), server.URL, &buf, 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum size")
}

func TestDownloadTo_HTTPStatusError(t *testing.T) {
	// A non-200 TLS response surfaces a clear HTTP-status error.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := DownloadTo(context.Background(), server.Client(), server.URL, &buf, 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestDownloadTo_RequestError(t *testing.T) {
	// A transport-level failure (server gone) surfaces as an error rather than
	// a silent success.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	var buf bytes.Buffer
	err := DownloadTo(context.Background(), server.Client(), server.URL, &buf, 1024)
	require.Error(t, err)
}

func TestDownloadTo_ChainsBaseCheckRedirect(t *testing.T) {
	// A caller-supplied CheckRedirect must still be invoked (chained) after the
	// HTTPS check passes, so custom redirect policy is preserved.
	called := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	c := server.Client()
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		called = true
		return nil
	}
	var buf bytes.Buffer
	require.NoError(t, DownloadTo(context.Background(), c, server.URL+"/asset", &buf, 1024))
	assert.True(t, called, "base CheckRedirect must be chained after the HTTPS check")
	assert.Equal(t, "ok", buf.String())
}

func TestDownloadTo_StopsAfterTenRedirects(t *testing.T) {
	// An HTTPS redirect loop must be bounded; DownloadTo refuses to follow past
	// 10 redirects rather than looping forever.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := DownloadTo(context.Background(), server.Client(), server.URL+"/loop", &buf, 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped after 10 redirects")
}

func TestUpgrade_EmptyCurrentVersion_ReturnsError(t *testing.T) {
	// Also exercises resolveUpgradeDefaults' default HTTPClient + Out paths
	// (both nil -> production defaults) before the version guard fires.
	_, err := Upgrade(context.Background(), UpgradeOptions{CurrentVersion: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "current version is required")
}

func TestResolveExePath_FallbackOnSymlinkError(t *testing.T) {
	// A path that does not exist makes EvalSymlinks fail; resolveExePath falls
	// back to the raw path rather than erroring, so an interrupted upgrade
	// (binary just replaced) still yields a usable target.
	got, err := resolveExePath(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err)
	assert.Contains(t, got, "does-not-exist")
}

func TestResolveExePath_UsesOsExecutableWhenEmpty(t *testing.T) {
	// An empty override resolves to the running binary's path via os.Executable.
	got, err := resolveExePath("")
	require.NoError(t, err)
	assert.NotEmpty(t, got)
}

func TestVerifyFileSHA256_OpenError(t *testing.T) {
	err := VerifyFileSHA256(filepath.Join(t.TempDir(), "missing"), "abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open downloaded asset")
}

func TestUpgrade_DownloadAssetFailure_Aborts(t *testing.T) {
	// checksums.txt is served (valid) but the asset 404s: the upgrade must abort
	// with a "download asset" error and leave the running binary untouched.
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)

	assetBytes := []byte("never-served")
	sum := sha256.Sum256(assetBytes)
	checksums := fmt.Sprintf("%x  %s\n", sum, asset)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
			_, _ = w.Write([]byte(checksums))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	exePath := filepath.Join(t.TempDir(), "javinizer")
	require.NoError(t, os.WriteFile(exePath, []byte("old"), 0o755))

	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion:  "v0.0.1",
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Checker:         &stubChecker{version: "v9.9.9"},
		ExePath:         exePath,
		Out:             &out,
	})
	require.Error(t, err)
	assert.False(t, res.Upgraded)
	assert.Contains(t, err.Error(), "download asset")

	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "old", string(got))
}

func TestReplaceBinary_Unix_PermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only replace path")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root — permission test is meaningless")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer")
	source := filepath.Join(dir, "new")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(source, []byte("new"), 0o755))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := replaceBinaryUnix(target, source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestUpgrade_CheckerError_ReturnsWrappedError(t *testing.T) {
	// Covers the CheckLatestVersion error branch: the checker failing must
	// surface a wrapped "failed to check latest release" error.
	var out bytes.Buffer
	_, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v0.0.1",
		Checker:        &stubChecker{err: errors.New("boom")},
		Out:            &out,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check latest release")
	assert.Contains(t, err.Error(), "boom")
}

func TestUpgrade_EmptyTagNameFallsBackToVersion(t *testing.T) {
	// Covers the `if result.TagName == ""` branch: a checker returning a
	// VersionInfo with no TagName must fall back to Version for the download URL.
	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion: "v0.0.1",
		CheckOnly:      true,
		Checker:        &emptyTagChecker{version: "v9.9.9"},
		Out:            &out,
	})
	require.NoError(t, err)
	assert.Equal(t, "v9.9.9", res.TagName, "empty TagName must fall back to Version")
}

// emptyTagChecker returns a VersionInfo with an empty TagName so the
// TagName-fallback branch in Upgrade is exercised.
type emptyTagChecker struct{ version string }

func (c *emptyTagChecker) CheckLatestVersion(_ context.Context) (*VersionInfo, error) {
	return &VersionInfo{Version: c.version}, nil
}

func TestReplaceBinary_Unix_MissingSource(t *testing.T) {
	// Covers the generic (non-permission) error branch of replaceBinaryUnix:
	// renaming a missing source over an existing target fails.
	if runtime.GOOS == "windows" {
		t.Skip("unix-only replace path")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o755))

	err := replaceBinaryUnix(target, filepath.Join(dir, "missing"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace binary")
	assert.NotContains(t, err.Error(), "permission denied")
}

func TestReplaceBinary_Windows_MissingTargetNoOld(t *testing.T) {
	// Covers the `rename current binary to .old` error branch: when target is
	// missing and no .old exists, os.Rename(target, old) fails. replaceBinaryWindows
	// uses only os.Rename/os.Remove so it is testable on any OS.
	dir := t.TempDir()
	source := filepath.Join(dir, "new.exe")
	require.NoError(t, os.WriteFile(source, []byte("new"), 0o755))

	err := replaceBinaryWindows(filepath.Join(dir, "missing.exe"), source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename current binary to .old")
}

func TestReplaceBinary_Windows_RestoreOldFails(t *testing.T) {
	// Covers the `restore previous binary from .old` error branch: target is
	// missing, .old exists, but restoring it fails because the directory is
	// read-only (the rename can't write target into the dir).
	if runtime.GOOS == "windows" {
		t.Skip("read-only-dir technique is Unix-only")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer.exe")
	old := target + ".old"
	require.NoError(t, os.WriteFile(old, []byte("previous"), 0o755))
	// Read-only dir: os.Rename(old, target) cannot create target here.
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := replaceBinaryWindows(target, filepath.Join(dir, "new.exe"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore previous binary from .old")
}

func TestUpgrade_MalformedChecksums_Aborts(t *testing.T) {
	// Covers the ParseChecksums error branch in downloadAndReplace: checksums.txt
	// is served but does not list the asset, so parsing fails before any download.
	// Checksums for a DIFFERENT asset -> ParseChecksums won't find ours.
	checksums := "deadbeef  some-other-asset\n"
	server := newAssetServer(t, []byte("unused"), checksums)
	defer server.Close()

	exePath := filepath.Join(t.TempDir(), "javinizer")
	require.NoError(t, os.WriteFile(exePath, []byte("old"), 0o755))

	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion:  "v0.0.1",
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Checker:         &stubChecker{version: "v9.9.9"},
		ExePath:         exePath,
		Out:             &out,
	})
	require.Error(t, err)
	assert.False(t, res.Upgraded)
	assert.Contains(t, err.Error(), "not found in checksums.txt")

	// Running binary untouched.
	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "old", string(got))
}

func TestUpgrade_CreateTempPermissionDenied_Aborts(t *testing.T) {
	// Covers the CreateTemp permission-error branch in downloadAndReplace: the
	// target directory is read-only, so the temp file cannot be created. The
	// checksums download succeeds (proving the flow got past it) before failing.
	if runtime.GOOS == "windows" {
		t.Skip("read-only-dir technique is Unix-only")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root")
	}
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)

	assetBytes := []byte("new-binary-content")
	sum := sha256.Sum256(assetBytes)
	checksums := fmt.Sprintf("%x  %s\n", sum, asset)
	server := newAssetServer(t, assetBytes, checksums)
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "javinizer")
	require.NoError(t, os.WriteFile(exePath, []byte("old"), 0o755))
	// Read-only dir: CreateTemp cannot write here.
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	var out bytes.Buffer
	res, err := Upgrade(context.Background(), UpgradeOptions{
		CurrentVersion:  "v0.0.1",
		DownloadBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Checker:         &stubChecker{version: "v9.9.9"},
		ExePath:         exePath,
		Out:             &out,
	})
	require.Error(t, err)
	assert.False(t, res.Upgraded)
	assert.Contains(t, err.Error(), "no write permission")
}

func TestUpgrade_ReplaceBinaryFailure_Aborts(t *testing.T) {
	// Covers the ReplaceBinary error branch: the checksums + asset download +
	// verify all succeed, but the final binary replace fails because the target
	// directory is read-only. (The Upgrade-level path hits CreateTemp first when
	// the dir is locked; this direct call isolates ReplaceBinary's own error
	// branch, which downloadAndReplace surfaces unchanged.)
	if runtime.GOOS == "windows" {
		t.Skip("read-only-dir technique is Unix-only")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer")
	source := filepath.Join(dir, "new")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(source, []byte("new"), 0o755))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := ReplaceBinary(target, source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

// The rollback-also-failed branch (replaceBinaryWindows) requires staging a
// state where rename-to-old succeeds, install-rename fails, AND rollback-rename
// fails — the dir would need to be writable for the first two renames but not
// the third, which cannot be staged deterministically without fragile mid-
// operation filesystem locking. It is left as a documented defensive branch.
