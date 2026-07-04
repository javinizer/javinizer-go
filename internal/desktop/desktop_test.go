package desktop

import (
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

func TestIsDesktopBuild_DefaultFalse(t *testing.T) {
	if IsDesktopBuild() {
		t.Fatal("IsDesktopBuild() = true in a normal (non -X injected) build; want false")
	}
}
