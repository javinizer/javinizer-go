//go:build darwin

package updater

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// executableFunc returns the running binary path. It is a package-level seam
// so tests can point the swapper at a synthetic .app layout under t.TempDir
// without monkeypatching os.Executable globally.
var executableFunc = os.Executable

// newSwapHelperCmd builds the detached sh helper command for SwapAndRelaunch.
// It is a package-level seam so tests can inject a failing exec (e.g. a
// nonexistent interpreter) to cover the cmd.Start error branch without
// spawning a real helper that outlives the test.
var newSwapHelperCmd = func(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", script) //nolint:gosec // script is generated from shellQuote-escaped paths derived from os.Executable, not user input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// closeZipEntry closes a just-written zip entry file. It is a package-level
// seam (mirroring closeTempFile in engine.go) so tests can inject a failing
// close to cover the defensive out.Close() error branch in extractZipRegular.
var closeZipEntry = func(f *os.File) error { return f.Close() }

// chmodZipEntry applies the entry's mode to the extracted file. It is a
// package-level seam so tests can inject a failing chmod to cover the
// defensive os.Chmod error branch in extractZipRegular.
var chmodZipEntry = os.Chmod

// macAppBundleName is the bundle directory produced by unzipping the macOS
// release asset (CI runs `ditto -c -k --keepParent Javinizer.app`).
const macAppBundleName = "Javinizer.app"

// darwinSwapper implements Swapper for the macOS .app bundle. The running
// binary lives at <Bundle>.app/Contents/MacOS/<exec>; Target walks up to the
// .app, Stage unzips a ditto-style asset into a sibling temp dir on the same
// volume (so the swap rename never hits EXDEV), and SwapAndRelaunch spawns a
// detached sh helper that waits for the running PID to exit, swaps the
// bundles, strips com.apple.quarantine, and relaunches via `open -n`.
type darwinSwapper struct{}

// NewDarwinSwapper returns a macOS Swapper backed by the running .app bundle.
func NewDarwinSwapper() Swapper { return &darwinSwapper{} }

// Target resolves the .app bundle containing the running binary. The canonical
// layout is <Bundle>.app/Contents/MacOS/<exec>, reached by three filepath.Dir
// calls; a robust fallback walks parents to the nearest *.app directory for
// non-standard layouts.
func (s *darwinSwapper) Target() (string, error) {
	exe, err := executableFunc()
	if err != nil {
		return "", fmt.Errorf("locate running binary: %w", err)
	}
	return appBundlePath(exe)
}

// CanSwap reports whether the bundle's parent directory is writable, which is
// the requirement for the rename-based swap. /Applications is admin-writable;
// non-admin users must install to ~/Applications or re-run with sudo.
func (s *darwinSwapper) CanSwap() error {
	app, err := s.Target()
	if err != nil {
		return err
	}
	parent := filepath.Dir(app)
	if err := unix.Access(parent, unix.W_OK); err != nil {
		return fmt.Errorf("no write permission on %s (move the app to ~/Applications or re-run with sudo): %w", parent, err)
	}
	return nil
}

// Stage unzips the downloaded macOS asset into a temp directory adjacent to
// the target .app (same volume, so the swap rename never hits EXDEV) and
// returns the path to the staged Javinizer.app bundle. The asset is a
// `ditto -c -k --keepParent` zip whose top-level entry is Javinizer.app.
func (s *darwinSwapper) Stage(ctx context.Context, downloadedPath, assetName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	app, err := s.Target()
	if err != nil {
		return "", err
	}
	base := filepath.Dir(app)
	stageDir, err := os.MkdirTemp(base, ".javinizer-stage-*")
	if err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	if err := unzipTo(downloadedPath, stageDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return "", err
	}
	staged := filepath.Join(stageDir, macAppBundleName)
	if _, err := os.Stat(staged); err != nil {
		_ = os.RemoveAll(stageDir)
		return "", fmt.Errorf("staged %s missing after unzip: %w", macAppBundleName, err)
	}
	_ = os.Remove(downloadedPath)
	return staged, nil
}

// SwapAndRelaunch spawns a detached sh helper that completes the bundle swap
// after the current process exits. The helper waits for oldPID to disappear
// (capped), renames target -> target.old, moves staged -> target, strips
// com.apple.quarantine, relaunches the new bundle with `open -n`, and removes
// the old bundle best-effort. The helper is placed in its own process group
// (Setpgid) so it survives the parent exit and is reaped by launchd; this
// returns immediately after Start without waiting.
func (s *darwinSwapper) SwapAndRelaunch(ctx context.Context, stagedPath string, oldPID int) error {
	if oldPID <= 0 {
		return fmt.Errorf("invalid oldPID: %d", oldPID)
	}
	app, err := s.Target()
	if err != nil {
		return err
	}
	script := darwinSwapScript(app, stagedPath, oldPID)
	cmd := newSwapHelperCmd(context.Background(), script)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start swap helper: %w", err)
	}
	return nil
}

// appBundlePath walks from an executable inside an .app bundle
// (.../Foo.app/Contents/MacOS/<exec>) up to the .app directory. It tries the
// canonical three-level walk-up first, then falls back to the nearest *.app
// ancestor for non-standard layouts.
func appBundlePath(execPath string) (string, error) {
	if candidate := filepath.Dir(filepath.Dir(filepath.Dir(execPath))); isAppBundle(candidate) {
		return candidate, nil
	}
	dir := filepath.Dir(execPath)
	for i := 0; i < maxAppWalkUp; i++ {
		if isAppBundle(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not locate .app bundle from executable path %s", execPath)
}

const maxAppWalkUp = 16

func isAppBundle(path string) bool {
	return strings.HasSuffix(filepath.Base(path), ".app")
}

// darwinSwapScript builds the detached helper script. Each post-wait step is
// best-effort: the caller has already arranged to exit, so the helper cannot
// report failures back. The wait loop is capped so a stuck PID does not leave
// the helper running indefinitely. Paths are single-quoted to survive spaces.
func darwinSwapScript(app, staged string, oldPID int) string {
	appOld := app + ".old"
	return strings.Join([]string{
		fmt.Sprintf("i=0; while kill -0 %d 2>/dev/null; do i=$((i+1)); if [ \"$i\" -ge %d ]; then echo 'javinizer: timed out waiting for process exit' >&2; exit 1; fi; sleep 0.2; done", oldPID, swapWaitMaxIters),
		"rm -rf " + shellQuote(appOld) + " 2>/dev/null || true",
		"if mv " + shellQuote(app) + " " + shellQuote(appOld) + "; then",
		"  if mv " + shellQuote(staged) + " " + shellQuote(app) + "; then",
		"    xattr -cr " + shellQuote(app),
		"    open -n " + shellQuote(app),
		"    rm -rf " + shellQuote(appOld) + " 2>/dev/null || true",
		"    rmdir " + shellQuote(filepath.Dir(staged)) + " 2>/dev/null || true",
		"  else",
		"    rm -rf " + shellQuote(app) + " 2>/dev/null || true",
		"    mv " + shellQuote(appOld) + " " + shellQuote(app),
		"    echo 'javinizer: swap failed; restored previous bundle' >&2",
		"  fi",
		"else",
		"  echo 'javinizer: could not move current bundle aside; aborting swap' >&2",
		"fi",
	}, "\n")
}

// unzipTo extracts the zip at zipPath into destDir, preserving modes and
// symlinks (the latter matter for signed .app bundles with Frameworks). The
// top-level entry is preserved: a ditto zip of Javinizer.app yields
// destDir/Javinizer.app.
func unzipTo(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", zipPath, err)
	}
	defer func() { _ = r.Close() }()
	for _, f := range r.File {
		if err := extractZipFile(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, destDir string) error {
	name := filepath.Join(destDir, f.Name) //nolint:gosec // traversal guarded by isWithinDir check below
	if !isWithinDir(name, destDir) {
		return fmt.Errorf("zip entry escapes dest dir: %s", f.Name)
	}
	mode := f.Mode()
	if f.FileInfo().IsDir() {
		return os.MkdirAll(name, mode.Perm()|0o700)
	}
	if mode&os.ModeSymlink != 0 {
		return extractZipSymlink(f, name)
	}
	return extractZipRegular(f, name, mode)
}

func extractZipRegular(f *zip.File, name string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	out, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	if _, err := io.Copy(out, rc); err != nil { //nolint:gosec // bundle is size-capped and SHA256-verified before extraction
		_ = out.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := closeZipEntry(out); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := chmodZipEntry(name, mode.Perm()); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

func extractZipSymlink(f *zip.File, name string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open symlink entry %s: %w", f.Name, err)
	}
	target, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return fmt.Errorf("read symlink target %s: %w", f.Name, err)
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return err
	}
	_ = os.Remove(name)
	if err := os.Symlink(string(target), name); err != nil {
		return fmt.Errorf("create symlink %s: %w", f.Name, err)
	}
	return nil
}

// isWithinDir reports whether path is inside dir after cleaning, guarding
// against zip-slip (entries containing .. that escape the dest dir).
func isWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
