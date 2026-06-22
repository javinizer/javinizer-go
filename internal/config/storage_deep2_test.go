package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDeep2_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent.yaml")
	cfg, err := Load(configPath)
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	// Should return default config
	assert.NotEqual(t, 0, cfg.ConfigVersion)
}

func TestLoadDeep2_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte("config_version: 3\nscraper_rate_limit: 1000\n")
	require.NoError(t, os.WriteFile(path, content, 0644))

	cfg, err := Load(path)
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 3, cfg.ConfigVersion)
}

func TestLoadDeep2_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte("{{invalid yaml}}")
	require.NoError(t, os.WriteFile(path, content, 0644))

	cfg, err := Load(path)
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestDecodeConfigDeep2_ValidData(t *testing.T) {
	data := []byte("config_version: 3\nscraper_rate_limit: 2000\n")
	cfg, err := decodeConfig(data)
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 3, cfg.ConfigVersion)
}

func TestDecodeConfigDeep2_InvalidData(t *testing.T) {
	data := []byte("{{invalid}}")
	cfg, err := decodeConfig(data)
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestParseConfigLockMetadataDeep2_Valid(t *testing.T) {
	pid, created, ok := parseConfigLockMetadata("pid=123,time=1700000000000000000")
	assert.True(t, ok)
	assert.Equal(t, 123, pid)
	assert.Equal(t, int64(1700000000000000000), created)
}

func TestParseConfigLockMetadataDeep2_MissingPID(t *testing.T) {
	_, _, ok := parseConfigLockMetadata("time=1700000000000000000")
	assert.False(t, ok)
}

func TestParseConfigLockMetadataDeep2_MissingTime(t *testing.T) {
	_, _, ok := parseConfigLockMetadata("pid=123")
	assert.False(t, ok)
}

func TestParseConfigLockMetadataDeep2_InvalidPID(t *testing.T) {
	_, _, ok := parseConfigLockMetadata("pid=abc,time=1700000000000000000")
	assert.False(t, ok)
}

func TestParseConfigLockMetadataDeep2_Empty(t *testing.T) {
	_, _, ok := parseConfigLockMetadata("")
	assert.False(t, ok)
}

func TestMakeConfigLockTokenDeep2(t *testing.T) {
	token := makeConfigLockToken()
	assert.Contains(t, token, "pid=")
	assert.Contains(t, token, "time=")
}

func TestIsProcessAliveDeep2_InvalidPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	assert.False(t, isProcessAlive(0))
	assert.False(t, isProcessAlive(-1))
}

func TestIsProcessAliveDeep2_CurrentProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	assert.True(t, isProcessAlive(os.Getpid()))
}

func TestShouldReapConfigLockDeep2_CorruptLock(t *testing.T) {
	// Corrupt lock should be reaped when stale by mtime
	content := []byte("corrupt-lock-data")
	now := time.Now()
	modTime := now.Add(-configLockStaleAge - time.Duration(1e9))
	assert.True(t, shouldReapConfigLock(content, modTime, now))
}

func TestShouldReapConfigLockDeep2_RecentLock(t *testing.T) {
	// Recent lock should NOT be reaped
	content := []byte("corrupt-lock-data")
	now := time.Now()
	modTime := now
	// Recent lock by mtime, even with corrupt content, should not be reaped
	assert.False(t, shouldReapConfigLock(content, modTime, now))
}

func TestFindMappingValueIndexDeep2(t *testing.T) {
	// Test with nil
	assert.Equal(t, -1, findMappingValueIndex(nil, "key"))
}

func TestCloneYAMLNodeDeep2_Nil(t *testing.T) {
	assert.Nil(t, cloneYAMLNode(nil))
}

func TestMergeYAMLNodeDeep2_NilDst(t *testing.T) {
	// Should not panic with nil inputs
	mergeYAMLNode(nil, nil)
}

func TestEncodeYAMLDocumentDeep2(t *testing.T) {
	data := []byte("key: value\n")
	doc, err := parseYAMLDocument(data)
	require.NoError(t, err)

	encoded, err := encodeYAMLDocument(doc)
	assert.NoError(t, err)
	assert.Contains(t, string(encoded), "key")
}

func TestParseYAMLDocumentDeep2_Invalid(t *testing.T) {
	_, err := parseYAMLDocument([]byte("{invalid"))
	assert.Error(t, err)
}

func TestParseYAMLDocumentDeep2_Valid(t *testing.T) {
	doc, err := parseYAMLDocument([]byte("key: value\n"))
	assert.NoError(t, err)
	assert.NotNil(t, doc)
}
