//go:build windows

package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// windowsSwapper swaps the desktop .exe bundle on Windows. The running exe is
// locked (cannot be overwritten in place) but CAN be renamed, so a detached
// .bat helper waits for the process to exit, renames the old exe to .old,
// moves the new one into place, relaunches it, and cleans up. See
// swap_windows_helpers.go for the (OS-agnostic, unit-tested) .bat generation.
type windowsSwapper struct {
	exePath string
}

// NewWindowsSwapper returns a Swapper for the Windows desktop build.
func NewWindowsSwapper() Swapper {
	return &windowsSwapper{}
}

func (w *windowsSwapper) Target() (string, error) {
	p := w.exePath
	if p == "" {
		var err error
		p, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("locate running exe: %w", err)
		}
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	return p, nil
}

func (w *windowsSwapper) CanSwap() error {
	target, err := w.Target()
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	probe, err := os.CreateTemp(dir, ".javinizer-probe-*.tmp")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied writing to %s — re-run as admin or move the app to a user-writable location", dir)
		}
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return nil
}

func (w *windowsSwapper) Stage(ctx context.Context, downloadedPath, assetName string) (string, error) {
	_ = os.Chmod(downloadedPath, 0o755)
	return downloadedPath, nil
}

func (w *windowsSwapper) SwapAndRelaunch(ctx context.Context, stagedPath string, oldPID int) error {
	exePath, err := w.Target()
	if err != nil {
		return err
	}
	script := renderWindowsBatScript(oldPID, exePath, stagedPath)
	batchPath := filepath.Join(os.TempDir(), fmt.Sprintf("javinizer-update-%d.bat", oldPID))
	if err := os.WriteFile(batchPath, []byte(script), 0o644); err != nil {
		return fmt.Errorf("write batch helper: %w", err)
	}
	cmd := newWindowsHelperCmd(oldPID, exePath, batchPath, stagedPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start detached helper: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// newWindowsHelperCmd builds the detached cmd.exe helper that runs the swap
// .bat after the current process exits. Extracted from SwapAndRelaunch so the
// process-creation attributes (CreationFlags, HideWindow, nil stdio) are
// unit-testable without spawning cmd.exe. The returned Cmd is NOT started;
// the caller starts it and releases the process handle so the helper
// outlives the parent.
func newWindowsHelperCmd(oldPID int, exePath, batchPath, stagedPath string) *exec.Cmd {
	cmd := exec.Command("cmd.exe", "/c", batchPath, strconv.Itoa(oldPID), exePath, stagedPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windowsHelperCreationFlags,
		HideWindow:    true,
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd
}
