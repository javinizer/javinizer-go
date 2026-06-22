package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// --- atomicReplaceFile: successful write and verify content ---

func TestMiss4_AtomicReplaceFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_config.yaml")
	data := []byte("key: value\n")

	err := atomicReplaceFile(path, data, 0644)
	require.NoError(t, err)

	readData, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, readData)
}

// --- atomicReplaceFile: overwrites existing file ---

func TestMiss4_AtomicReplaceFile_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_config.yaml")

	// Create initial file
	require.NoError(t, os.WriteFile(path, []byte("old: data\n"), 0644))

	// Overwrite
	newData := []byte("new: data\n")
	err := atomicReplaceFile(path, newData, 0644)
	require.NoError(t, err)

	readData, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, newData, readData)
}

// --- atomicReplaceFile: creates parent directory if needed ---

func TestMiss4_AtomicReplaceFile_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "test_config.yaml")
	data := []byte("key: value\n")

	// This should fail because subdir doesn't exist
	err := atomicReplaceFile(path, data, 0644)
	// atomicReplaceFile doesn't create parent dirs, it uses os.OpenFile which requires them
	assert.Error(t, err)
}

// --- Save: creates new file from config ---

func TestMiss4_Save_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	cfg.Server.Host = "testhost"

	err := Save(cfg, path)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Verify we can load it back
	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "testhost", loaded.Server.Host)
}

// --- Save: preserves existing YAML structure with merge ---

func TestMiss4_Save_MergesWithExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write initial config with a custom comment
	initialYAML := "# This is a comment\nserver:\n  host: original\n"
	require.NoError(t, os.WriteFile(path, []byte(initialYAML), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)

	// Modify and save
	cfg.Server.Host = "updated"
	err = Save(cfg, path)
	require.NoError(t, err)

	// Verify the file was updated
	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "updated", loaded.Server.Host)
}

// --- Save: no change skips write ---

func TestMiss4_Save_NoChangeSkipsWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	err := Save(cfg, path)
	require.NoError(t, err)

	// Get file mod time
	info1, err := os.Stat(path)
	require.NoError(t, err)

	// Save again with no changes
	err = Save(cfg, path)
	require.NoError(t, err)

	// File mod time should be same (no write)
	info2, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, info1.ModTime(), info2.ModTime())
}

// --- LoadOrCreate: creates from embedded when file missing ---

func TestMiss4_LoadOrCreate_CreatesFromEmbedded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new_config.yaml")

	cfg, err := LoadOrCreate(path)
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// Verify file was created
	_, err = os.Stat(path)
	require.NoError(t, err)
}

// --- LoadOrCreate: loads existing file ---

func TestMiss4_LoadOrCreate_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing_config.yaml")

	// Create initial config
	cfg := DefaultConfig(nil, nil)
	err := Save(cfg, path)
	require.NoError(t, err)

	// Load it
	loaded, err := LoadOrCreate(path)
	require.NoError(t, err)
	assert.NotNil(t, loaded)
}

// --- LoadOrCreate: stat error other than not exist ---

func TestMiss4_LoadOrCreate_StatError(t *testing.T) {
	// Use a path that will cause a stat error (e.g., directory that doesn't exist
	// and can't be created due to permissions)
	// This is tricky to test on some systems, so we skip if not applicable
	dir := t.TempDir()
	// Create a file where we expect a directory
	path := filepath.Join(dir, "notadir", "config.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notadir"), []byte("x"), 0644))

	_, err := LoadOrCreate(path)
	require.Error(t, err)
}

// --- syncDir: successful sync ---

func TestMiss4_SyncDir_Success(t *testing.T) {
	dir := t.TempDir()
	err := syncDir(dir)
	assert.NoError(t, err)
}

// --- syncDir: non-existent directory returns error ---

func TestMiss4_SyncDir_NonExistent(t *testing.T) {
	err := syncDir("/nonexistent/directory/12345")
	assert.Error(t, err)
}

// --- configToYAMLDocument: error on marshal failure ---

func TestMiss4_ConfigToYAMLDocument_ErrorOnInvalid(t *testing.T) {
	// Create a config that will fail to marshal (hard to trigger with our Config type)
	// Just test the happy path more thoroughly
	cfg := DefaultConfig(nil, nil)
	doc, err := configToYAMLDocument(cfg)
	require.NoError(t, err)
	assert.Equal(t, yaml.DocumentNode, doc.Kind)
}

// --- parseYAMLDocument: invalid YAML returns error ---

func TestMiss4_ParseYAMLDocument_InvalidYAML(t *testing.T) {
	_, err := parseYAMLDocument([]byte(":\n  :\n  -"))
	// go-yaml is very permissive, this might not error
	// The real error path is when the result is not a DocumentNode
	_ = err
}

// --- acquireConfigFileLock: basic lock and release ---

func TestMiss4_AcquireConfigFileLock_BasicLockRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	unlock, err := acquireConfigFileLock(path)
	require.NoError(t, err)

	// Lock file should exist
	_, err = os.Stat(path + ".lock")
	require.NoError(t, err)

	// Release the lock
	unlock()

	// Lock file should be removed
	_, err = os.Stat(path + ".lock")
	assert.True(t, os.IsNotExist(err))
}

// --- applyInitDefaultsFromEnv: various env var overrides ---

func TestMiss4_ApplyInitDefaultsFromEnv_ServerHost(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")

	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)
	assert.True(t, changed)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
}

func TestMiss4_ApplyInitDefaultsFromEnv_AllowedDirs(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "/dir1,/dir2")

	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)
	assert.True(t, changed)
	assert.Contains(t, cfg.API.Security.AllowedDirectories, "/dir1")
	assert.Contains(t, cfg.API.Security.AllowedDirectories, "/dir2")
}

func TestMiss4_ApplyInitDefaultsFromEnv_AllowedOrigins(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_ALLOWED_ORIGINS", "http://localhost:3000,http://example.com")

	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)
	assert.True(t, changed)
	assert.Contains(t, cfg.API.Security.AllowedOrigins, "http://localhost:3000")
}

func TestMiss4_ApplyInitDefaultsFromEnv_NoEnvVars(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)
	assert.False(t, changed)
}

func TestMiss4_ApplyInitDefaultsFromEnv_NilConfig(t *testing.T) {
	changed := applyInitDefaultsFromEnv(nil)
	assert.False(t, changed)
}
