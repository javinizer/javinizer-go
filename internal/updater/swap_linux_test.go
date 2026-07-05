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
