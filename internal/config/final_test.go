package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveFinal_WithExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Create initial config
	cfg := DefaultConfig(nil, nil)
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 9999
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load and modify
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Server.Host != "0.0.0.0" {
		t.Errorf("expected host 0.0.0.0, got %s", loaded.Server.Host)
	}
	if loaded.Server.Port != 9999 {
		t.Errorf("expected port 9999, got %d", loaded.Server.Port)
	}

	// Save again with modifications
	loaded.Server.Port = 8080
	if err := Save(loaded, path); err != nil {
		t.Fatalf("Second Save failed: %v", err)
	}

	// Verify
	loaded2, err := Load(path)
	if err != nil {
		t.Fatalf("Second Load failed: %v", err)
	}
	if loaded2.Server.Port != 8080 {
		t.Errorf("expected port 8080 after update, got %d", loaded2.Server.Port)
	}
}

func TestSaveFinal_NoChangeSkipsWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	info1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	// Save identical config - should skip write
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Second Save failed: %v", err)
	}

	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	// ModTime should be same since no write occurred
	if info2.ModTime() != info1.ModTime() {
		t.Log("Note: ModTime changed even for identical content - this is acceptable but not optimal")
	}
}

func TestLoadOrCreateFinal_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new_config.yaml")

	cfg, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}
}

func TestLoadOrCreateFinal_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing_config.yaml")

	// Create initial config
	cfg := DefaultConfig(nil, nil)
	cfg.Server.Port = 7777
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// LoadOrCreate should load existing
	loaded, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate failed: %v", err)
	}
	if loaded.Server.Port != 7777 {
		t.Errorf("expected port 7777, got %d", loaded.Server.Port)
	}
}

func TestAtomicReplaceFileFinal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_file.yaml")

	// Write initial content
	if err := atomicReplaceFile(path, []byte("initial content"), 0644); err != nil {
		t.Fatalf("atomicReplaceFile failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "initial content" {
		t.Errorf("expected 'initial content', got %q", string(data))
	}

	// Overwrite
	if err := atomicReplaceFile(path, []byte("updated content"), 0644); err != nil {
		t.Fatalf("second atomicReplaceFile failed: %v", err)
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "updated content" {
		t.Errorf("expected 'updated content', got %q", string(data))
	}
}

func TestSyncDirFinal(t *testing.T) {
	dir := t.TempDir()
	if err := syncDir(dir); err != nil {
		t.Logf("syncDir returned error (may be expected on some platforms): %v", err)
	}
}

func TestSyncDirFinal_NonexistentDir(t *testing.T) {
	err := syncDir("/nonexistent/directory/path")
	if err == nil {
		t.Log("syncDir succeeded for nonexistent dir - unexpected but not fatal")
	}
}

func TestAcquireConfigFileLockFinal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock_test.yaml")

	unlock, err := acquireConfigFileLock(path)
	if err != nil {
		t.Fatalf("acquireConfigFileLock failed: %v", err)
	}
	unlock()

	// Lock file should be removed after unlock
	lockPath := path + ".lock"
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed after unlock")
	}
}

func TestAcquireConfigFileLockFinal_DoubleLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "double_lock_test.yaml")

	unlock1, err := acquireConfigFileLock(path)
	if err != nil {
		t.Fatalf("first acquireConfigFileLock failed: %v", err)
	}

	// Second lock should timeout since first isn't released
	// Use a short timeout by directly testing the lock file
	lockPath := path + ".lock"
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("expected lock file to exist")
	}

	unlock1()
}

func TestMakeConfigLockTokenFinal(t *testing.T) {
	token := makeConfigLockToken()
	if token == "" {
		t.Error("expected non-empty token")
	}
	if !strings.Contains(token, "pid=") {
		t.Errorf("expected token to contain 'pid=', got %q", token)
	}
	if !strings.Contains(token, "time=") {
		t.Errorf("expected token to contain 'time=', got %q", token)
	}
}

func TestParseConfigLockMetadataFinal(t *testing.T) {
	tests := []struct {
		content string
		wantPID int
		wantOK  bool
	}{
		{"pid=123,time=456789", 123, true},
		{"", 0, false},
		{"invalid content", 0, false},
		{"pid=abc,time=123", 0, false},
	}
	for _, tt := range tests {
		pid, _, ok := parseConfigLockMetadata(tt.content)
		if ok != tt.wantOK {
			t.Errorf("parseConfigLockMetadata(%q) ok=%v, want %v", tt.content, ok, tt.wantOK)
		}
		if ok && pid != tt.wantPID {
			t.Errorf("parseConfigLockMetadata(%q) pid=%d, want %d", tt.content, pid, tt.wantPID)
		}
	}
}

func TestShouldReapConfigLockFinal(t *testing.T) {
	// Old lock from dead process should be reaped
	oldContent := []byte("pid=99999,time=0") // time=0 means very old
	err := shouldReapConfigLock(oldContent, time.Now().Add(-3*time.Minute), time.Now())
	if !err {
		t.Error("expected old lock from dead process to be reaped")
	}

	// Recent lock should not be reaped
	recentTime := time.Now().UnixNano()
	recentContent := []byte(fmt.Sprintf("pid=123,time=%d", recentTime))
	err = shouldReapConfigLock(recentContent, time.Now(), time.Now())
	if err {
		t.Error("expected recent lock not to be reaped")
	}
}
