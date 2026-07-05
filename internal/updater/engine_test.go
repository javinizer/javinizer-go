package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/update"
)

// stubChecker returns a fixed latest version, no network.
type stubChecker struct {
	version string
	err     error
	mu      sync.Mutex
	calls   int
	pre     bool
}

func (s *stubChecker) CheckLatestVersion(ctx context.Context) (*update.VersionInfo, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return &update.VersionInfo{Version: s.version, TagName: s.version}, nil
}

func (s *stubChecker) SetPreRelease(b bool) { s.pre = b }

// mockSwapper records calls and never actually swaps.
type mockSwapper struct {
	target     string
	targetErr  error
	canSwapErr error
	stageErr   error
	swapErr    error
	staged     string
	swapCalled bool
	swapPID    int
	swapStaged string
	mu         sync.Mutex
}

func (m *mockSwapper) Target() (string, error) {
	if m.targetErr != nil {
		return "", m.targetErr
	}
	if m.target == "" {
		return "/tmp/fake-bundle", nil
	}
	return m.target, nil
}

func (m *mockSwapper) CanSwap() error { return m.canSwapErr }

func (m *mockSwapper) Stage(ctx context.Context, downloaded, asset string) (string, error) {
	if m.stageErr != nil {
		return "", m.stageErr
	}
	if m.staged != "" {
		return m.staged, nil
	}
	return downloaded, nil
}

func (m *mockSwapper) SwapAndRelaunch(ctx context.Context, staged string, oldPID int) error {
	m.mu.Lock()
	m.swapCalled = true
	m.swapPID = oldPID
	m.swapStaged = staged
	m.mu.Unlock()
	return m.swapErr
}

func (m *mockSwapper) swapInvoked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.swapCalled
}

// newTestServer serves checksums.txt + asset bytes over TLS (DownloadTo
// refuses plain HTTP). The asset's real SHA is listed under the running
// platform's bundle asset name so ParseChecksums + VerifyFileSHA256 pass.
func newTestServer(t *testing.T, asset []byte, checksumOverride string) *httptest.Server {
	t.Helper()
	assetName, err := BundleAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("no bundle asset for test host: %v", err)
	}
	sum := sha256.Sum256(asset)
	expected := hex.EncodeToString(sum[:])
	if checksumOverride != "" {
		expected = checksumOverride
	}
	checksums := fmt.Sprintf("%s  %s\n", expected, assetName)
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			_, _ = w.Write([]byte(checksums))
		default:
			_, _ = w.Write(asset)
		}
	}))
}

// newFailingServer serves 404 for the named resource (checksums.txt or the
// asset) to exercise download error branches. Other paths succeed.
func newFailingServer(t *testing.T, failSuffix string) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, failSuffix) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
}

// newTestEngine wires an Engine against the TLS test server + stub checker.
func newTestEngine(t *testing.T, sw Swapper, current string, latest string, asset []byte, checksumOverride string) (*Engine, *httptest.Server) {
	t.Helper()
	srv := newTestServer(t, asset, checksumOverride)
	e := NewEngine(sw, current,
		WithChecker(&stubChecker{version: latest}),
		WithHTTPClient(srv.Client()),
		WithDownloadBase(srv.URL),
	)
	return e, srv
}

func TestEngine_HappyPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "fake-bundle")
	sw := &mockSwapper{target: target}
	asset := []byte("fake bundle bytes")
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "")
	defer srv.Close()

	res, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}
	if !sw.swapInvoked() {
		t.Fatal("SwapAndRelaunch was not invoked")
	}
	if res.UpToDate {
		t.Fatal("expected UpToDate=false")
	}
	if res.LatestVersion != "v9.9.9" {
		t.Fatalf("LatestVersion = %q, want v9.9.9", res.LatestVersion)
	}
	if got := e.Status().State; got != StateRelaunching {
		t.Fatalf("Status = %q, want %q", got, StateRelaunching)
	}
	if sw.swapPID <= 0 {
		t.Fatalf("swapPID = %d, want >0", sw.swapPID)
	}
}

func TestEngine_AlreadyUpToDate(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "v9.9.9", "v9.9.9", asset, "")
	defer srv.Close()

	res, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}
	if !res.UpToDate {
		t.Fatal("expected UpToDate=true")
	}
	if sw.swapInvoked() {
		t.Fatal("SwapAndRelaunch should not be invoked when up-to-date")
	}
	if got := e.Status().State; got != StateIdle {
		t.Fatalf("Status = %q, want %q", got, StateIdle)
	}
}

func TestEngine_Force(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "v9.9.9", "v9.9.9", asset, "")
	defer srv.Close()

	res, err := e.Upgrade(context.Background(), UpgradeOptions{Force: true})
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}
	if res.UpToDate {
		t.Fatal("expected UpToDate=false with Force")
	}
	if !sw.swapInvoked() {
		t.Fatal("SwapAndRelaunch should be invoked with Force")
	}
}

func TestEngine_AlreadyInProgress(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "")
	defer srv.Close()

	// Simulate an in-flight upgrade so begin() observes a non-idle state.
	e.setState(StateDownloading, "")

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err != ErrAlreadyInProgress {
		t.Fatalf("err = %v, want ErrAlreadyInProgress", err)
	}
}

func TestEngine_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("real bytes")
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "0000000000000000000000000000000000000000000000000000000000000000")
	defer srv.Close()

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
	if sw.swapInvoked() {
		t.Fatal("SwapAndRelaunch should not be invoked on checksum mismatch")
	}
}

func TestEngine_CanSwapFails(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{
		target:     filepath.Join(dir, "fake-bundle"),
		canSwapErr: fmt.Errorf("permission denied"),
	}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "")
	defer srv.Close()

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
}

func TestEngine_SwapFailureLeavesFailed(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{
		target:  filepath.Join(dir, "fake-bundle"),
		swapErr: fmt.Errorf("helper spawn failed"),
	}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "")
	defer srv.Close()

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil {
		t.Fatal("expected swap error")
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
}

func TestBundleAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"darwin", "amd64", "Javinizer-macos-universal.zip"},
		{"darwin", "arm64", "Javinizer-macos-universal.zip"},
		{"windows", "amd64", "Javinizer.exe"},
		{"linux", "amd64", "Javinizer-linux-x86_64.AppImage"},
		{"linux", "arm64", "Javinizer-linux-aarch64.AppImage"},
	}
	for _, c := range cases {
		got, err := BundleAssetName(c.goos, c.goarch)
		if err != nil {
			t.Errorf("%s/%s: unexpected error: %v", c.goos, c.goarch, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s/%s: got %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
	if _, err := BundleAssetName("windows", "arm64"); err == nil {
		t.Error("expected error for windows/arm64 (no published asset)")
	}
	if _, err := BundleAssetName("freebsd", "amd64"); err == nil {
		t.Error("expected error for freebsd/amd64 (unsupported)")
	}
}

func TestEngine_TargetError(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{
		target:    filepath.Join(dir, "fake-bundle"),
		targetErr: fmt.Errorf("no app bundle"),
	}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "")
	defer srv.Close()

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "resolve bundle target") {
		t.Fatalf("err = %v, want resolve bundle target", err)
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
	if sw.swapInvoked() {
		t.Fatal("SwapAndRelaunch should not be invoked on target error")
	}
}

func TestEngine_StageError(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{
		target:   filepath.Join(dir, "fake-bundle"),
		stageErr: fmt.Errorf("disk full"),
	}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "")
	defer srv.Close()

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "stage bundle") {
		t.Fatalf("err = %v, want stage bundle", err)
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
	if sw.swapInvoked() {
		t.Fatal("SwapAndRelaunch should not be invoked on stage error")
	}
}

func TestEngine_ParseChecksumsError(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	// checksumOverride is a malformed checksums.txt (no entry for the asset),
	// so ParseChecksums returns an error.
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "deadbeef  other-asset.zip\n")
	defer srv.Close()

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil {
		t.Fatal("expected parse checksums error")
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
	if sw.swapInvoked() {
		t.Fatal("SwapAndRelaunch should not be invoked on checksum parse error")
	}
}

func TestEngine_CheckerError(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	srv := newTestServer(t, asset, "")
	defer srv.Close()
	e := NewEngine(sw, "v1.0.0",
		WithChecker(&stubChecker{err: fmt.Errorf("network down")}),
		WithHTTPClient(srv.Client()),
		WithDownloadBase(srv.URL),
	)

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "failed to check latest release") {
		t.Fatalf("err = %v, want failed to check latest release", err)
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
}

func TestEngine_EmptyCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "", "v9.9.9", asset, "")
	defer srv.Close()

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "current version is required") {
		t.Fatalf("err = %v, want current version is required", err)
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
}

type stubRelauncher struct {
	called bool
	err    error
	mu     sync.Mutex
}

func (r *stubRelauncher) Relaunch(ctx context.Context) error {
	r.mu.Lock()
	r.called = true
	r.mu.Unlock()
	return r.err
}

func (r *stubRelauncher) invoked() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.called
}

func TestEngine_RelaunchWithoutRelauncher(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	e, srv := newTestEngine(t, sw, "v1.0.0", "v9.9.9", asset, "")
	defer srv.Close()

	// No relauncher injected: Relaunch is a no-op (nil error).
	if err := e.Relaunch(context.Background()); err != nil {
		t.Fatalf("Relaunch without relauncher should be no-op, got %v", err)
	}
}

func TestEngine_RelaunchWithRelauncher(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	r := &stubRelauncher{}
	srv := newTestServer(t, asset, "")
	defer srv.Close()
	e := NewEngine(sw, "v1.0.0",
		WithChecker(&stubChecker{version: "v9.9.9"}),
		WithHTTPClient(srv.Client()),
		WithDownloadBase(srv.URL),
		WithRelauncher(r),
	)

	if err := e.Relaunch(context.Background()); err != nil {
		t.Fatalf("Relaunch failed: %v", err)
	}
	if !r.invoked() {
		t.Fatal("Relauncher.Relaunch was not invoked")
	}
}

func TestEngine_PreReleaseOption(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	asset := []byte("bytes")
	chk := &stubChecker{version: "v9.9.9-pre"}
	srv := newTestServer(t, asset, "")
	defer srv.Close()
	e := NewEngine(sw, "v1.0.0",
		WithChecker(chk),
		WithHTTPClient(srv.Client()),
		WithDownloadBase(srv.URL),
	)

	res, err := e.Upgrade(context.Background(), UpgradeOptions{PreRelease: true})
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}
	if res.LatestVersion != "v9.9.9-pre" {
		t.Fatalf("LatestVersion = %q, want v9.9.9-pre", res.LatestVersion)
	}
	if !chk.pre {
		t.Fatal("SetPreRelease(true) should be called when PreRelease option is set")
	}
}

func TestEngine_DownloadChecksumsFailure(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	srv := newFailingServer(t, "checksums.txt")
	defer srv.Close()
	e := NewEngine(sw, "v1.0.0",
		WithChecker(&stubChecker{version: "v9.9.9"}),
		WithHTTPClient(srv.Client()),
		WithDownloadBase(srv.URL),
	)

	_, err := e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "download checksums") {
		t.Fatalf("err = %v, want download checksums", err)
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
}

func TestEngine_DownloadAssetFailure(t *testing.T) {
	dir := t.TempDir()
	sw := &mockSwapper{target: filepath.Join(dir, "fake-bundle")}
	assetName, err := BundleAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("no bundle asset for test host: %v", err)
	}
	// Serve valid checksums but 404 for the asset download.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  " + assetName + "\n"))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	e := NewEngine(sw, "v1.0.0",
		WithChecker(&stubChecker{version: "v9.9.9"}),
		WithHTTPClient(srv.Client()),
		WithDownloadBase(srv.URL),
	)

	_, err = e.Upgrade(context.Background(), UpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "download asset") {
		t.Fatalf("err = %v, want download asset", err)
	}
	if got := e.Status().State; got != StateFailed {
		t.Fatalf("Status = %q, want %q", got, StateFailed)
	}
}

func TestEngine_NewEngineDefaultsChecker(t *testing.T) {
	// When no WithChecker option is passed, NewEngine wires a real GitHub
	// checker (the default path). Covers the e.checker == nil branch.
	sw := &mockSwapper{target: "/tmp/fake-bundle"}
	e := NewEngine(sw, "v1.0.0")
	if e.checker == nil {
		t.Fatal("NewEngine should default a checker when none is injected")
	}
}
