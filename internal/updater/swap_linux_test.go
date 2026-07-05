//go:build linux

package updater

import (
	"context"
	"os"
	"path/filepath"
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
