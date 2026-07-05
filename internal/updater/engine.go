// Package updater implements bundle-level self-upgrade for desktop builds.
//
// Unlike internal/update (which swaps the CLI binary in place), this package
// swaps the whole native bundle (.app / .exe / .AppImage) and relaunches the
// app. It is a leaf package: it imports internal/update (for the reusable
// download/checksum/checker primitives) and internal/system, but NEVER
// internal/desktop — the desktop app injects a platform Swapper + a
// Relauncher so this package stays cycle-free and testable in normal CI.
//
// The Engine is generic (OS-agnostic); platform specifics live behind the
// Swapper interface, implemented per-OS in swap_darwin.go / swap_windows.go /
// swap_linux.go (build-tagged). The OS selection happens in internal/desktop
// (//go:build desktop), which constructs the right Swapper and injects it via
// NewEngine.
package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/update"
)

// State is the lifecycle state of a bundle upgrade. The API surfaces this via
// GET /api/v1/desktop/upgrade/status so the UI can show progress during the
// (synchronous) download/verify/stage phase. Once SwapAndRelaunch fires, the
// process is about to exit and the new app window opens fresh — there is no
// long-poll across the restart.
type State string

// State values track the upgrade state machine, from idle through a
// completed swap or a terminal failure.
const (
	StateIdle        State = "idle"
	StateDownloading State = "downloading"
	StateVerifying   State = "verifying"
	StateStaging     State = "staging"
	StateSwapping    State = "swapping"
	StateRelaunching State = "relaunching"
	StateFailed      State = "failed"
)

// Status is a snapshot of the upgrade state machine, safe to read concurrently.
type Status struct {
	State   State  `json:"state"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Swapper abstracts the per-OS bundle swap. Each platform implements it in a
// build-tagged file (swap_<os>.go); the Engine is OS-agnostic and drives it
// through these four steps.
//
//   - Target:     resolve the on-disk bundle path to replace
//     (.app walk-up on macOS, APPIMAGE env on Linux, os.Executable on Windows).
//   - CanSwap:    permission check (write access on the target's parent dir).
//   - Stage:      transform the downloaded asset into final bundle form at a
//     path adjacent to the target on the SAME filesystem (unzip
//     the .app on macOS; pass-through on Windows/Linux).
//   - SwapAndRelaunch: spawn a DETACHED helper process that (1) waits for the
//     current process to exit, (2) swaps staged -> target, (3)
//     relaunches the new bundle, (4) exits. Returns immediately
//     after spawning; the helper runs independently.
type Swapper interface {
	Target() (string, error)
	CanSwap() error
	Stage(ctx context.Context, downloadedPath, assetName string) (stagedPath string, err error)
	SwapAndRelaunch(ctx context.Context, stagedPath string, oldPID int) error
}

// BundleUpdater is the seam the API layer (internal/api/desktop) and the
// desktop app (internal/desktop) use to drive a bundle self-upgrade without
// importing internal/updater's concrete Engine (which would pull the download
// machinery into the API layer). The concrete *Engine satisfies this.
//
// It is defined here (a leaf package) so internal/commandutil can hold a field
// of this type and internal/api/desktop can consume it — both without an
// import cycle. internal/desktop constructs the *Engine and injects it via
// CoreDeps.SetBundleUpdater at StartServer.
type BundleUpdater interface {
	// Upgrade checks for, downloads, verifies, stages, and spawns the swap
	// helper for the latest desktop bundle. Returns once the helper is
	// spawned (StateRelaunching); the caller must then Relaunch.
	Upgrade(ctx context.Context, opts UpgradeOptions) (*UpgradeResult, error)
	// Status returns a snapshot of the current upgrade state (thread-safe).
	Status() Status
	// Relaunch shuts the app down so the detached swap helper can complete.
	// The API handler calls this AFTER writing its HTTP response.
	Relaunch(ctx context.Context) error
}

// Relauncher shuts down the running app so the detached helper can complete
// the swap. In the desktop build this calls runtime.Quit on the captured
// Wails ctx (internal/desktop wires it). The Engine does not call it
// directly; the API handler calls Relaunch after writing its HTTP response
// and after the Engine's Upgrade has spawned the helper, so the response
// flushes before the window closes.
type Relauncher interface {
	Relaunch(ctx context.Context) error
}

// Option configures an Engine at construction.
type Option func(*Engine)

// WithChecker injects a release checker (tests pass a stub; production uses
// update.NewChecker).
func WithChecker(c update.Checker) Option {
	return func(e *Engine) { e.checker = c }
}

// WithHTTPClient overrides the download HTTP client (tests).
func WithHTTPClient(c *http.Client) Option {
	return func(e *Engine) { e.httpClient = c }
}

// WithDownloadBase overrides the release-asset host (tests).
func WithDownloadBase(url string) Option {
	return func(e *Engine) { e.downloadBase = url }
}

// WithRelauncher injects the app-shutdown hook (the desktop build's Wails
// Quit wrapper). If nil, Upgrade still performs download+verify+stage+spawn
// but the caller must arrange shutdown itself.
func WithRelauncher(r Relauncher) Option {
	return func(e *Engine) { e.relauncher = r }
}

var (
	// syncTempFile flushes the downloaded temp file. It is a package-level
	// seam so tests can inject a Sync failure to cover the engine's flush
	// error branch without a custom filesystem (real host fs does not fail
	// fsync under CI conditions). The default is (*os.File).Sync.
	syncTempFile = func(f *os.File) error { return f.Sync() }

	// closeTempFile closes the downloaded temp file. It is a package-level
	// seam so tests can inject a Close failure to cover the engine's close
	// error branch. The default is (*os.File).Close.
	closeTempFile = func(f *os.File) error { return f.Close() }
)

const (
	// defaultDownloadBase is the host serving release assets (github.com). The
	// API base (api.github.com) lives inside the checker.
	defaultDownloadBase = "https://github.com"
	defaultRepo         = "javinizer/javinizer-go"
	// maxBundleSize caps a downloaded bundle (the macOS universal zip is the
	// largest at ~40-60MB; leave headroom).
	maxBundleSize    = 512 * 1024 * 1024 // 512MB
	maxChecksumsSize = 1 * 1024 * 1024   // 1MB
)

// Engine orchestrates a desktop bundle upgrade. It is safe for concurrent
// Status reads; Upgrade is serialized via mu (only one upgrade at a time).
type Engine struct {
	swapper      Swapper
	relauncher   Relauncher
	checker      update.Checker
	httpClient   *http.Client
	downloadBase string
	current      string

	// mu serializes Upgrade runs (only one upgrade at a time).
	mu sync.Mutex
	// statusMu guards the status snapshot so GET /status can read progress
	// concurrently while an Upgrade is mid-flight (Upgrade holds mu but NOT
	// statusMu except for the brief setState/fail writes).
	statusMu sync.RWMutex
	status   Status
}

// NewEngine constructs an Engine over the given Swapper. The current version
// is the running build's version.Short(). Pass Options to inject a checker,
// HTTP client, download base, and/or relauncher.
func NewEngine(s Swapper, currentVersion string, opts ...Option) *Engine {
	e := &Engine{
		swapper:      s,
		current:      currentVersion,
		httpClient:   &http.Client{Timeout: 5 * time.Minute},
		downloadBase: defaultDownloadBase,
		status:       Status{State: StateIdle},
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.checker == nil {
		e.checker = update.NewChecker(defaultRepo)
	}
	return e
}

// Status returns a snapshot of the current upgrade state (thread-safe). Safe
// to call concurrently with Upgrade — it takes only the read lock, so a
// GET /upgrade/status poll reports live progress instead of blocking until
// the upgrade finishes.
func (e *Engine) Status() Status {
	e.statusMu.RLock()
	defer e.statusMu.RUnlock()
	return e.status
}

// ErrAlreadyInProgress is returned by Upgrade when an upgrade is mid-flight.
var ErrAlreadyInProgress = errors.New("a bundle upgrade is already in progress")

// UpgradeOptions configures a single Upgrade run.
type UpgradeOptions struct {
	Force      bool
	PreRelease bool
}

// UpgradeResult is the outcome of a successful Upgrade (the swap helper has
// been spawned; the caller should now shut the app down via Relaunch).
type UpgradeResult struct {
	CurrentVersion string
	LatestVersion  string
	AssetName      string
	StagedPath     string
	UpToDate       bool
}

// Upgrade checks for, downloads, verifies, and stages the latest desktop
// bundle, then spawns the detached swap+relaunch helper. It returns once the
// helper is spawned (StateRelaunching); the caller MUST then shut down the app
// so the helper can complete the file swap (the running bundle is locked on
// Windows and holds the FUSE mount on Linux).
//
// Errors at any step set StateFailed and return. A concurrent Upgrade call
// returns ErrAlreadyInProgress without touching state.
func (e *Engine) Upgrade(ctx context.Context, opts UpgradeOptions) (*UpgradeResult, error) {
	if err := e.begin(); err != nil {
		return nil, err
	}
	defer e.mu.Unlock()

	if e.current == "" {
		e.fail("current version is required")
		return nil, errors.New("current version is required")
	}

	chk := e.checker
	if pc, ok := chk.(update.PreReleaseChecker); ok {
		pc.SetPreRelease(opts.PreRelease)
	}

	e.setState(StateDownloading, "")
	logging.Infof("updater: checking latest release (current: %s)", e.current)
	latest, err := chk.CheckLatestVersion(ctx)
	if err != nil {
		e.fail("failed to check latest release: " + err.Error())
		return nil, fmt.Errorf("failed to check latest release: %w", err)
	}

	result := &UpgradeResult{
		CurrentVersion: e.current,
		LatestVersion:  latest.Version,
	}

	if !opts.Force && update.CompareVersions(e.current, latest.Version) >= 0 {
		e.setState(StateIdle, "")
		result.UpToDate = true
		return result, nil
	}

	asset, err := currentBundleAsset()
	if err != nil {
		e.fail(err.Error())
		return nil, err
	}
	result.AssetName = asset

	target, err := e.swapper.Target()
	if err != nil {
		e.fail("resolve bundle target: " + err.Error())
		return nil, fmt.Errorf("resolve bundle target: %w", err)
	}
	if err := e.swapper.CanSwap(); err != nil {
		e.fail("permission denied: " + err.Error())
		return nil, fmt.Errorf("cannot swap bundle: %w", err)
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		e.fail("create target dir: " + err.Error())
		return nil, fmt.Errorf("create target dir: %w", err)
	}

	checksumURL := fmt.Sprintf("%s/%s/releases/download/%s/checksums.txt", e.downloadBase, defaultRepo, latest.Version)
	assetURL := fmt.Sprintf("%s/%s/releases/download/%s/%s", e.downloadBase, defaultRepo, latest.Version, asset)

	var checksumBuf bytes.Buffer
	if err := update.DownloadTo(ctx, e.httpClient, checksumURL, &checksumBuf, maxChecksumsSize); err != nil {
		e.fail("download checksums: " + err.Error())
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	expected, err := update.ParseChecksums(checksumBuf.Bytes(), asset)
	if err != nil {
		e.fail("parse checksums: " + err.Error())
		return nil, err
	}

	tmp, err := os.CreateTemp(dir, ".javinizer-bundle-upgrade-*.tmp")
	if err != nil {
		e.fail("create temp file: " + err.Error())
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if result.StagedPath == "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := update.DownloadTo(ctx, e.httpClient, assetURL, tmp, maxBundleSize); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		e.fail("download asset: " + err.Error())
		return nil, fmt.Errorf("download asset: %w", err)
	}
	if err := syncTempFile(tmp); err != nil {
		_ = closeTempFile(tmp)
		_ = os.Remove(tmpPath)
		e.fail("flush asset: " + err.Error())
		return nil, fmt.Errorf("flush asset: %w", err)
	}
	if err := closeTempFile(tmp); err != nil {
		_ = os.Remove(tmpPath)
		e.fail("close asset: " + err.Error())
		return nil, fmt.Errorf("close asset: %w", err)
	}

	e.setState(StateVerifying, latest.Version)
	if err := update.VerifyFileSHA256(tmpPath, expected); err != nil {
		_ = os.Remove(tmpPath)
		e.fail("checksum mismatch: " + err.Error())
		return nil, err
	}

	e.setState(StateStaging, latest.Version)
	staged, err := e.swapper.Stage(ctx, tmpPath, asset)
	if err != nil {
		_ = os.Remove(tmpPath)
		e.fail("stage bundle: " + err.Error())
		return nil, fmt.Errorf("stage bundle: %w", err)
	}
	result.StagedPath = staged

	e.setState(StateSwapping, latest.Version)
	pid := os.Getpid()
	if err := e.swapper.SwapAndRelaunch(ctx, staged, pid); err != nil {
		e.fail("spawn swap helper: " + err.Error())
		return nil, fmt.Errorf("spawn swap helper: %w", err)
	}

	e.setState(StateRelaunching, latest.Version)
	logging.Infof("updater: swap helper spawned; bundle %s -> %s, relaunching", e.current, latest.Version)
	return result, nil
}

// Relaunch shuts the app down so the detached helper can complete the swap.
// The API handler calls this AFTER writing its HTTP response. No-op if no
// Relauncher was injected.
func (e *Engine) Relaunch(ctx context.Context) error {
	if e.relauncher == nil {
		return nil
	}
	return e.relauncher.Relaunch(ctx)
}

// begin acquires the upgrade lock. Returns ErrAlreadyInProgress if busy.
func (e *Engine) begin() error {
	e.mu.Lock()
	if e.Status().State != StateIdle && e.Status().State != StateFailed {
		e.mu.Unlock()
		return ErrAlreadyInProgress
	}
	e.setStatus(Status{State: StateDownloading})
	return nil
}

// setState updates the status under the status lock (Upgrade holds the
// serialization mu, but statusMu is only held briefly here so /status reads
// are not blocked for the upgrade's duration).
func (e *Engine) setState(s State, version string) {
	e.setStatus(Status{State: s, Version: version})
}

// fail records a failure state under the status lock.
func (e *Engine) fail(msg string) {
	logging.Errorf("updater: %s", msg)
	e.setStatus(Status{State: StateFailed, Error: msg})
}

// setStatus is the single writer for the status snapshot.
func (e *Engine) setStatus(s Status) {
	e.statusMu.Lock()
	e.status = s
	e.statusMu.Unlock()
}
