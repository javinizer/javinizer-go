//go:build linux

package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// osExecutable is a package-level seam over os.Executable so tests can inject
// a failing or synthetic executable path without monkeypatching the stdlib.
var osExecutable = os.Executable

// evalSymlinks is a package-level seam over filepath.EvalSymlinks so tests can
// inject a failure to cover the resolveAppImageTarget error branch.
var evalSymlinks = filepath.EvalSymlinks

// osCreateTemp is a package-level seam over os.CreateTemp so tests can inject
// a non-permission error to cover CanSwap's CreateTemp error branch.
var osCreateTemp = os.CreateTemp

// newSwapHelperCmd builds the detached sh helper command for
// SwapAndRelaunch. It is a package-level seam so tests can inject a failing
// exec (e.g. a nonexistent interpreter) to cover the cmd.Start error branch
// without spawning a real helper that outlives the test.
var newSwapHelperCmd = func(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", script) //nolint:gosec // script is generated from shellQuote-escaped paths derived from os.Executable/APPIMAGE, not user input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// linuxSwapper implements Swapper for a type-2 AppImage desktop build. The
// AppImage runtime exports APPIMAGE (the real bundle path) in both FUSE and
// extract-and-run modes; that env var is the canonical swap target because
// os.Executable returns a throwaway /tmp/.mount_* FUSE mountpoint.
type linuxSwapper struct{}

// NewLinuxSwapper returns a Swapper that replaces the running .AppImage in
// place via a detached setsid sh helper.
func NewLinuxSwapper() Swapper {
	return &linuxSwapper{}
}

// Target resolves the .AppImage to replace: APPIMAGE when set and executable,
// otherwise the symlink-resolved running executable (dev/test direct-binary
// runs). It validates the result exists and is executable.
func (s *linuxSwapper) Target() (string, error) {
	exePath, err := osExecutable()
	if err != nil {
		return "", fmt.Errorf("resolve AppImage target: locate executable: %w", err)
	}
	target, err := resolveAppImageTarget(os.Getenv("APPIMAGE"), exePath)
	if err != nil {
		return "", err
	}
	if !fileExistsAndExecutable(target) {
		return "", fmt.Errorf("AppImage target %q is not an executable file", target)
	}
	return target, nil
}

// CanSwap write-tests the target's containing directory. AppImages usually
// live in a user-writable dir (~/Downloads, ~/Applications, ~/.local/bin); a
// system path such as /opt owned by root fails here with a hint to relocate or
// re-run via pkexec.
func (s *linuxSwapper) CanSwap() error {
	target, err := s.Target()
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	probe, err := osCreateTemp(dir, ".javinizer-probe-*.tmp")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("no write permission to %s: move the AppImage to a user-writable dir (~/Applications, ~/.local/bin, or ~/Downloads), or re-run via pkexec for a system path such as /opt", dir)
		}
		return fmt.Errorf("write-test AppImage dir %s: %w", dir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return nil
}

// Stage is a pass-through: the downloaded .AppImage is already the final asset
// and lives in the target's directory (the engine downloads into a temp file
// adjacent to the target so the rename is atomic, same filesystem). It only
// ensures the staged file is executable before the swap helper moves it.
func (s *linuxSwapper) Stage(ctx context.Context, downloadedPath, assetName string) (string, error) {
	//nolint:gosec // G302: the downloaded AppImage must be executable (0o755) to run.
	if err := os.Chmod(downloadedPath, 0o755); err != nil {
		return "", fmt.Errorf("chmod staged AppImage: %w", err)
	}
	return downloadedPath, nil
}

// SwapAndRelaunch spawns a detached setsid sh helper that (1) waits for the
// current process to exit, (2) moves the staged .AppImage over the target,
// (3) marks it executable, (4) optionally re-exports APPIMAGE_EXTRACT_AND_RUN,
// and (5) relaunches it via nohup. It returns immediately after spawning; the
// helper runs independently in its own session and outlives this process.
//
// The ctx.Err() guard is covered by
// TestLinuxSwapper_SwapAndRelaunch_CancelledContext. The success spawn path
// (cmd.Start + cmd.Process.Release) is covered by
// TestLinuxSwapper_SwapAndRelaunch_SpawnSuccess, which uses a dead oldPID,
// a nonexistent staged path, and a throwaway target so the helper cannot
// mutate anything outside a temp dir. The cmd.Start error branch is covered
// by TestLinuxSwapper_SwapAndRelaunch_StartError via the newSwapHelperCmd
// seam pointing at a nonexistent interpreter.
//
// exec.CommandContext (with context.Background, not the request ctx) is
// intentional: the helper must survive the request context being cancelled
// when the app quits, since the swap only begins after this process exits.
func (s *linuxSwapper) SwapAndRelaunch(ctx context.Context, stagedPath string, oldPID int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("spawn Linux swap helper: %w", err)
	}
	target, err := s.Target()
	if err != nil {
		return err
	}
	_, preserveExtract := os.LookupEnv("APPIMAGE_EXTRACT_AND_RUN")

	cmd := newSwapHelperCmd(context.Background(), renderLinuxHelperScript(oldPID, target, stagedPath, preserveExtract))
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn Linux swap helper: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
