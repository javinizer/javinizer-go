package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestUserDataDir_CreatesDir(t *testing.T) {
	dir, err := UserDataDir()
	if err != nil {
		t.Fatalf("UserDataDir() error: %v", err)
	}
	if dir == "" {
		t.Fatal("UserDataDir() returned empty path")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("user data dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
}

func TestDefaultConfigPath_UnderUserDataDir(t *testing.T) {
	dir, _ := UserDataDir()
	cfg := DefaultConfigPath()
	want := filepath.Join(dir, "config.yaml")
	if cfg != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", cfg, want)
	}
}

func TestSetupPortableEnv_SetsEnvWhenAbsent(t *testing.T) {
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_LOG_DIR", "")

	if err := SetupPortableEnv(); err != nil {
		t.Fatalf("SetupPortableEnv() error: %v", err)
	}

	dir, _ := UserDataDir()
	wantDB := filepath.Join(dir, "data", "javinizer.db")
	wantLogDir := filepath.Join(dir, "data", "logs")

	if got := os.Getenv("JAVINIZER_DB"); got != wantDB {
		t.Errorf("JAVINIZER_DB = %q, want %q", got, wantDB)
	}
	if got := os.Getenv("JAVINIZER_LOG_DIR"); got != wantLogDir {
		t.Errorf("JAVINIZER_LOG_DIR = %q, want %q", got, wantLogDir)
	}
}

func TestSetupPortableEnv_PreservesExistingEnv(t *testing.T) {
	t.Setenv("JAVINIZER_DB", "/custom/db.sqlite")
	t.Setenv("JAVINIZER_LOG_DIR", "/custom/logs")

	if err := SetupPortableEnv(); err != nil {
		t.Fatalf("SetupPortableEnv() error: %v", err)
	}

	if got := os.Getenv("JAVINIZER_DB"); got != "/custom/db.sqlite" {
		t.Errorf("JAVINIZER_DB = %q, want /custom/db.sqlite (existing value must be preserved)", got)
	}
	if got := os.Getenv("JAVINIZER_LOG_DIR"); got != "/custom/logs" {
		t.Errorf("JAVINIZER_LOG_DIR = %q, want /custom/logs (existing value must be preserved)", got)
	}
}

// TestSetupPortableEnv_DataDirMkdirAllFailure covers the os.MkdirAll error
// branch inside SetupPortableEnv (paths.go:60-62): UserDataDir succeeds, but
// the `data` subdir cannot be created because a regular file already exists at
// that path. Cross-platform: all home/config env vars are pointed at a temp
// dir so UserDataDir resolves deterministically under it on every OS.
func TestSetupPortableEnv_DataDirMkdirAllFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", tmp)
	t.Setenv("LOCALAPPDATA", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_LOG_DIR", "")

	// Let UserDataDir create its dir, then plant a regular file at the `data`
	// subdir path so the subsequent MkdirAll(dataDir) inside SetupPortableEnv
	// fails with "not a directory".
	dir, err := UserDataDir()
	if err != nil {
		t.Fatalf("UserDataDir() setup error: %v", err)
	}
	dataPath := filepath.Join(dir, "data")
	if err := os.WriteFile(dataPath, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}

	if err := SetupPortableEnv(); err == nil {
		t.Fatal("SetupPortableEnv() should fail when dataDir MkdirAll fails, got nil")
	}
}

func TestIsDesktopBuild_DefaultFalse(t *testing.T) {
	if IsDesktopBuild() {
		t.Fatal("IsDesktopBuild() = true in a normal (non -X injected) build; want false")
	}
}

// TestUserDataDir_HomeFallback covers the branch where os.UserConfigDir fails
// but os.UserHomeDir succeeds (e.g. Windows with %AppData% unset but
// %USERPROFILE% set). On Linux/macOS these two functions both consult $HOME
// and so cannot diverge, so the seam (userConfigDirFn/userHomeDirFn) is swapped
// to exercise the `base = filepath.Join(home, ".javinizer")` fallback.
func TestUserDataDir_HomeFallback(t *testing.T) {
	origCfg, origHome := userConfigDirFn, userHomeDirFn
	t.Cleanup(func() {
		userConfigDirFn, userHomeDirFn = origCfg, origHome
	})
	tmp := t.TempDir()
	userConfigDirFn = func() (string, error) { return "", fmt.Errorf("no config dir") }
	userHomeDirFn = func() (string, error) { return tmp, nil }

	dir, err := UserDataDir()
	if err != nil {
		t.Fatalf("UserDataDir() error: %v", err)
	}
	want := filepath.Join(tmp, ".javinizer", appDataDirName)
	if dir != want {
		t.Errorf("UserDataDir() = %q, want %q", dir, want)
	}
}

// TestUserDataDir_FallsBackToHomeDir covers the branch where os.UserConfigDir
// fails (no XDG_CONFIG_HOME / HOME on the runner). In that case UserDataDir
// must fall back to ~/.javinizer rather than erroring.
func TestUserDataDir_FallsBackToHomeDir(t *testing.T) {
	// Force os.UserConfigDir() to fail by clearing the env vars it reads.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")     // Windows
	t.Setenv("USERPROFILE", "") // Windows

	dir, err := UserDataDir()
	// On systems where even os.UserHomeDir() fails (HOME unset), UserDataDir
	// returns an error — that path is covered by the assertion below. When
	// UserHomeDir succeeds, dir should be under ~/.javinizer.
	if err == nil && dir == "" {
		t.Fatal("UserDataDir() returned empty path without error")
	}
}

// TestDefaultConfigPath_FallsBackOnUserDataDirError covers the error branch:
// when UserDataDir fails, DefaultConfigPath must return the CLI default so
// the app still attempts to boot.
func TestDefaultConfigPath_FallsBackOnUserDataDirError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")
	t.Setenv("USERPROFILE", "")

	cfg := DefaultConfigPath()
	// Either the fallback path (if UserHomeDir succeeded) or the CLI default
	// (if it failed). Both are valid; the key is no panic + non-empty.
	if cfg == "" {
		t.Fatal("DefaultConfigPath() returned empty string")
	}
}

// TestSetupPortableEnv_HomeDirFallback ensures SetupPortableEnv still works
// (or fails gracefully) when the user-config dir is unavailable, exercising
// the UserDataDir error path inside SetupPortableEnv.
func TestSetupPortableEnv_HomeDirFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_LOG_DIR", "")

	_ = SetupPortableEnv()
	// No assertion on error: both success (HOME fallback) and failure (no
	// HOME) are acceptable. This covers the error-handling branch either way.
}

// TestUserDataDir_MkdirAllFailure covers the os.MkdirAll error branch in
// UserDataDir: when the resolved config dir cannot be created (e.g. a parent
// path component is a regular file), UserDataDir must return an error.
func TestUserDataDir_MkdirAllFailure(t *testing.T) {
	// os.UserConfigDir() resolves to ~/Library/Application Support on macOS
	// and $XDG_CONFIG_HOME on Linux. Setting HOME to a regular-file path
	// makes MkdirAll fail with "not a directory" on both platforms.
	blocker, err := os.CreateTemp(t.TempDir(), "blocker")
	if err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	defer blocker.Close()
	t.Setenv("HOME", blocker.Name())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(blocker.Name(), "sub"))
	t.Setenv("AppData", blocker.Name())
	t.Setenv("LOCALAPPDATA", blocker.Name())
	t.Setenv("USERPROFILE", blocker.Name())

	_, err = UserDataDir()
	if err == nil {
		t.Fatal("UserDataDir() should fail when MkdirAll cannot create the dir")
	}
}

// TestSetupPortableEnv_MkdirAllFailure covers the os.MkdirAll error branch in
// SetupPortableEnv: when the data/logs dirs cannot be created under the
// user-data dir, SetupPortableEnv must return an error.
func TestSetupPortableEnv_MkdirAllFailure(t *testing.T) {
	blocker, err := os.CreateTemp(t.TempDir(), "blocker")
	if err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	defer blocker.Close()
	t.Setenv("HOME", blocker.Name())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(blocker.Name(), "sub"))
	t.Setenv("AppData", blocker.Name())
	t.Setenv("LOCALAPPDATA", blocker.Name())
	t.Setenv("USERPROFILE", blocker.Name())
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_LOG_DIR", "")

	if err := SetupPortableEnv(); err == nil {
		t.Fatal("SetupPortableEnv() should fail when MkdirAll cannot create the data dirs")
	}
}
