package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicReplaceFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile.yaml")

	err := atomicReplaceFile(path, []byte("hello world"), 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func TestAtomicReplaceFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile.yaml")

	// Write initial content
	require.NoError(t, atomicReplaceFile(path, []byte("initial"), 0644))

	// Overwrite
	require.NoError(t, atomicReplaceFile(path, []byte("updated"), 0644))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "updated", string(data))
}

func TestAtomicReplaceFile_InvalidPath(t *testing.T) {
	configPath := unreachableConfigPath(t)
	err := atomicReplaceFile(configPath, []byte("data"), 0644)
	assert.Error(t, err)
}

func TestSyncDir_Success(t *testing.T) {
	dir := t.TempDir()
	err := syncDir(dir)
	assert.NoError(t, err)
}

func TestSyncDir_NonExistent(t *testing.T) {
	err := syncDir("/nonexistent/dir/path/that/does/not/exist")
	assert.Error(t, err)
}

func TestIsProcessAlive_Current(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	alive := isProcessAlive(os.Getpid())
	assert.True(t, alive)
}

func TestIsProcessAlive_NonExistent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	alive := isProcessAlive(999999)
	assert.False(t, alive)
}
