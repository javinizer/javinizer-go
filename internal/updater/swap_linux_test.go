//go:build linux

package updater

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Compile-time assertion that NewLinuxSwapper satisfies Swapper.
var _ Swapper = NewLinuxSwapper()

func TestNewLinuxSwapper_NonNil(t *testing.T) {
	s := NewLinuxSwapper()
	if s == nil {
		t.Fatal("NewLinuxSwapper returned nil")
	}
	if _, ok := s.(*linuxSwapper); !ok {
		t.Fatalf("NewLinuxSwapper returned %T, want *linuxSwapper", s)
	}
}

func TestLinuxSwapper_Stage_PassthroughAndChmod(t *testing.T) {
	s := &linuxSwapper{}
	dir := t.TempDir()
	p := filepath.Join(dir, "staged.AppImage")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("write staged file: %v", err)
	}

	got, err := s.Stage(context.Background(), p, "Javinizer-linux-x86_64.AppImage")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if got != p {
		t.Errorf("Stage returned %q, want %q (pass-through)", got, p)
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat staged file: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("staged file mode = %o, want 0o755", info.Mode().Perm())
	}
}

func TestLinuxSwapper_Target_APPIMAGEEnv(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(bundle, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	t.Setenv("APPIMAGE", bundle)
	s := &linuxSwapper{}
	got, err := s.Target()
	if err != nil {
		t.Fatalf("Target: %v", err)
	}
	if got != bundle {
		t.Errorf("Target = %q, want %q", got, bundle)
	}
}

func TestLinuxSwapper_Target_FallbackToExecutable(t *testing.T) {
	// APPIMAGE unset: Target falls back to the symlink-resolved running
	// executable (the test binary), which must exist and be executable.
	t.Setenv("APPIMAGE", "")
	s := &linuxSwapper{}
	got, err := s.Target()
	if err != nil {
		t.Fatalf("Target: %v", err)
	}
	if got == "" {
		t.Error("Target returned empty path")
	}
	if !fileExistsAndExecutable(got) {
		t.Errorf("Target %q is not an executable file", got)
	}
}

func TestLinuxSwapper_CanSwap_WritableDir(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(bundle, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	t.Setenv("APPIMAGE", bundle)

	s := &linuxSwapper{}
	if err := s.CanSwap(); err != nil {
		t.Errorf("CanSwap on user-writable dir: %v", err)
	}
}

func TestLinuxSwapper_CanSwap_ReadOnlyDir(t *testing.T) {
	// A read-only dir must fail CanSwap with a permission hint. Skipped when
	// running as root (CI containers), since root bypasses file permission
	// checks and CanSwap would succeed.
	if os.Geteuid() == 0 {
		t.Skip("CanSwap permission check is bypassed by root")
	}
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(bundle, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	t.Setenv("APPIMAGE", bundle)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	s := &linuxSwapper{}
	if err := s.CanSwap(); err == nil {
		t.Fatal("CanSwap on read-only dir should fail")
	}
}

func TestLinuxSwapper_Target_NotExecutableFallback(t *testing.T) {
	// APPIMAGE points at a non-executable file: Target falls back to the
	// symlink-resolved running executable (the test binary) rather than erroring.
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(bundle, []byte("not executable"), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	t.Setenv("APPIMAGE", bundle)

	s := &linuxSwapper{}
	got, err := s.Target()
	if err != nil {
		t.Fatalf("Target should fall back to executable, not error: %v", err)
	}
	if got == bundle {
		t.Error("Target should not return the non-executable APPIMAGE; should fall back to exe")
	}
	if !fileExistsAndExecutable(got) {
		t.Errorf("fallback target %q is not executable", got)
	}
}

func TestLinuxSwapper_SwapAndRelaunch_CancelledContext(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(bundle, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	t.Setenv("APPIMAGE", bundle)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &linuxSwapper{}
	if err := s.SwapAndRelaunch(ctx, bundle, 1); err == nil {
		t.Fatal("SwapAndRelaunch with cancelled context should fail")
	}
}

func TestLinuxSwapper_SwapAndRelaunch_SpawnSuccess(t *testing.T) {
	// Cover the cmd.Start() SUCCESS path of SwapAndRelaunch (the lines that
	// spawn the detached helper and release its process), without performing
	// a real bundle swap or leaving a relaunched app running.
	//
	// Safety analysis of renderLinuxHelperScript under this test's inputs:
	//   1. wait loop: oldPID is a child process we started and already reaped,
	//      so `kill -0 <oldPID>` fails on the first iteration and the loop
	//      exits immediately (no waiting).
	//   2. `mv -f <staged> <target>`: staged is a NONEXISTENT path, so mv
	//      fails with ENOENT and leaves <target> untouched. The script has
	//      no `set -e`, so it continues past the failed mv.
	//   3. `chmod +x <target>`: target is our dummy bundle in t.TempDir();
	//      making it executable is harmless.
	//   4. `nohup <target> &`: target is a `#!/bin/sh\nexit 0` script, so the
	//      relaunch exits immediately and leaves no background process.
	// Net effect: the helper spawns, immediately exits its wait loop, fails
	// the mv harmlessly, and the relaunch is a no-op. The real bundle is
	// never touched (APPIMAGE points at our dummy, not the test binary).
	dir := t.TempDir()
	target := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatalf("write dummy target: %v", err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatalf("chmod dummy target: %v", err)
	}
	t.Setenv("APPIMAGE", target)

	// Spawn and reap a throwaway child to obtain a PID that is guaranteed
	// dead before the helper examines it, so the helper's wait loop is a
	// no-op. Using os.Getpid() would risk the helper waiting on the test
	// process itself; PID 1 may be alive in some containers.
	deadPID := func(t *testing.T) int {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		cmd := exec.Command("sh", "-c", "exit 0")
		if err := cmd.Start(); err != nil {
			t.Fatalf("start throwaway child: %v", err)
		}
		pid := cmd.Process.Pid
		if err := cmd.Wait(); err != nil {
			t.Fatalf("wait throwaway child: %v", err)
		}
		_ = r.Close()
		_ = w.Close()
		return pid
	}(t)

	// staged is intentionally nonexistent: mv -f fails harmlessly and the
	// target is left untouched.
	staged := filepath.Join(dir, "no-such-staged-AppImage")

	s := &linuxSwapper{}
	if err := s.SwapAndRelaunch(context.Background(), staged, deadPID); err != nil {
		t.Fatalf("SwapAndRelaunch success path: %v", err)
	}

	// The target file must still exist and be untouched (mv failed).
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("target disappeared after spawn: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("target lost executable bit; perm=%o", info.Mode().Perm())
	}
}

// TestLinuxSwapper_Target_ExecutableError covers the osExecutable() error
// branch of Target (the seam injects a failure, avoiding any real
// os.Executable behavior). Mirrors darwin's withExecutableFuncErr helper.
func TestLinuxSwapper_Target_ExecutableError(t *testing.T) {
	orig := osExecutable
	osExecutable = func() (string, error) {
		return "", errors.New("osExecutable seam failed")
	}
	t.Cleanup(func() { osExecutable = orig })

	s := &linuxSwapper{}
	if _, err := s.Target(); err == nil {
		t.Fatal("expected Target to fail when osExecutable errors")
	}
}

// TestLinuxSwapper_Target_ResolveSymlinkError covers the resolveAppImageTarget
// error branch of Target by injecting an evalSymlinks seam that always fails.
// APPIMAGE is unset so the fallback path runs and the seam error surfaces.
func TestLinuxSwapper_Target_ResolveSymlinkError(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	orig := evalSymlinks
	evalSymlinks = func(string) (string, error) {
		return "", errors.New("evalSymlinks seam failed")
	}
	t.Cleanup(func() { evalSymlinks = orig })

	s := &linuxSwapper{}
	if _, err := s.Target(); err == nil {
		t.Fatal("expected Target to fail when evalSymlinks errors")
	}
}

// TestLinuxSwapper_Target_NotExecutableAfterFallback covers the
// "target not executable" branch of Target after the APPIMAGE fallback
// resolves to a non-executable file. evalSymlinks is injected to return a
// non-executable file we control, so the final fileExistsAndExecutable check
// fails without relying on the test binary's real path.
func TestLinuxSwapper_Target_NotExecutableAfterFallback(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	dir := t.TempDir()
	nonExec := filepath.Join(dir, "not-executable")
	if err := os.WriteFile(nonExec, []byte("no exec bit"), 0o600); err != nil {
		t.Fatalf("write non-exec file: %v", err)
	}
	orig := evalSymlinks
	evalSymlinks = func(string) (string, error) { return nonExec, nil }
	t.Cleanup(func() { evalSymlinks = orig })

	s := &linuxSwapper{}
	if _, err := s.Target(); err == nil {
		t.Fatal("expected Target to fail when fallback target is not executable")
	}
}

// TestLinuxSwapper_CanSwap_TargetError covers the CanSwap early-return branch
// that propagates a Target() failure (osExecutable err) before the probe runs.
func TestLinuxSwapper_CanSwap_TargetError(t *testing.T) {
	orig := osExecutable
	osExecutable = func() (string, error) {
		return "", errors.New("osExecutable seam failed")
	}
	t.Cleanup(func() { osExecutable = orig })

	s := &linuxSwapper{}
	if err := s.CanSwap(); err == nil {
		t.Fatal("expected CanSwap to fail when Target fails")
	}
}

// TestLinuxSwapper_CanSwap_CreateTempNonPermissionError covers the CanSwap
// CreateTemp error branch that is NOT a permission error (the generic
// "write-test AppImage dir" wrap). The osCreateTemp seam injects a non-
// permission error so the permission-hint branch is skipped.
func TestLinuxSwapper_CanSwap_CreateTempNonPermissionError(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(bundle, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	t.Setenv("APPIMAGE", bundle)

	orig := osCreateTemp
	osCreateTemp = func(string, string) (*os.File, error) {
		return nil, errors.New("CreateTemp seam failed: ENOSPC")
	}
	t.Cleanup(func() { osCreateTemp = orig })

	s := &linuxSwapper{}
	err := s.CanSwap()
	if err == nil {
		t.Fatal("expected CanSwap to fail when CreateTemp errors")
	}
	// Must NOT include the permission-hint wording (that's the other branch).
	if strings.Contains(err.Error(), "no write permission") {
		t.Fatalf("CreateTemp non-permission error must not surface the permission hint: %v", err)
	}
}

// TestLinuxSwapper_CanSwap_CreateTempPermissionError covers the CanSwap
// permission branch (os.IsPermission) by injecting an osCreateTemp seam that
// returns syscall.EACCES, the exact errno a real read-only dir would yield.
// This exercises the permission-hint return path deterministically, even
// under root (CI containers) where real file permission checks are bypassed
// by CAP_DAC_OVERRIDE. The companion TestLinuxSwapper_CanSwap_ReadOnlyDir
// proves the branch against a real read-only filesystem as a non-root user
// (verifiable via `docker run -u 1000:1000 ...`).
func TestLinuxSwapper_CanSwap_CreateTempPermissionError(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(bundle, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	t.Setenv("APPIMAGE", bundle)

	orig := osCreateTemp
	osCreateTemp = func(string, string) (*os.File, error) {
		return nil, syscall.EACCES
	}
	t.Cleanup(func() { osCreateTemp = orig })

	s := &linuxSwapper{}
	err := s.CanSwap()
	if err == nil {
		t.Fatal("expected CanSwap to fail with a permission error")
	}
	if !strings.Contains(err.Error(), "no write permission") {
		t.Fatalf("expected permission hint in error, got: %v", err)
	}
}

// TestLinuxSwapper_Stage_ChmodError covers the os.Chmod error branch of Stage
// by pointing at a nonexistent downloaded path so Chmod fails with ENOENT.
func TestLinuxSwapper_Stage_ChmodError(t *testing.T) {
	s := &linuxSwapper{}
	missing := filepath.Join(t.TempDir(), "no-such-staged-AppImage")
	if _, err := s.Stage(context.Background(), missing, "x.AppImage"); err == nil {
		t.Fatal("expected Stage to fail when Chmod errors on a missing file")
	}
}

// TestLinuxSwapper_SwapAndRelaunch_TargetError covers the Target() failure
// branch of SwapAndRelaunch that runs after the ctx.Err() guard. The
// osExecutable seam injects a failure so Target errors before the helper is
// spawned (no real process is started).
func TestLinuxSwapper_SwapAndRelaunch_TargetError(t *testing.T) {
	orig := osExecutable
	osExecutable = func() (string, error) {
		return "", errors.New("osExecutable seam failed")
	}
	t.Cleanup(func() { osExecutable = orig })

	s := &linuxSwapper{}
	if err := s.SwapAndRelaunch(context.Background(), "/tmp/staged", 1); err == nil {
		t.Fatal("expected SwapAndRelaunch to fail when Target errors")
	}
}

// TestLinuxSwapper_SwapAndRelaunch_StartError covers the cmd.Start failure
// branch by injecting a newSwapHelperCmd seam that returns a command pointing
// at a nonexistent interpreter, so Start fails with ENOENT. Mirrors the
// darwin StartError test. No real helper is spawned.
func TestLinuxSwapper_SwapAndRelaunch_StartError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Javinizer.AppImage")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	t.Setenv("APPIMAGE", target)

	orig := newSwapHelperCmd
	newSwapHelperCmd = func(ctx context.Context, script string) *exec.Cmd {
		// Nonexistent interpreter path -> Start fails with ENOENT.
		return exec.CommandContext(ctx, "/this/interpreter/does/not/exist", "-c", script)
	}
	t.Cleanup(func() { newSwapHelperCmd = orig })

	s := &linuxSwapper{}
	if err := s.SwapAndRelaunch(context.Background(), "/tmp/staged", 1); err == nil {
		t.Fatal("expected cmd.Start error to be returned")
	}
}
