package fscase

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestIsCaseInsensitiveFS(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	tmpDir := t.TempDir()

	result := cache.IsCaseInsensitive(tmpDir)

	t.Logf("Filesystem case-insensitive: %v", result)
}

func TestFSCaseCache_IsCaseInsensitive_CachesResult(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())

	tmpDir := t.TempDir()

	result1 := cache.IsCaseInsensitive(tmpDir)
	result2 := cache.IsCaseInsensitive(tmpDir)

	assert.Equal(t, result1, result2, "cached results should match for same directory")

	cache.mu.RLock()
	cached, exists := cache.cache[tmpDir]
	cache.mu.RUnlock()
	assert.True(t, exists, "result should be cached under exact directory path")
	assert.Equal(t, result1, cached, "cached value should match returned value")
}

func TestIsCaseInsensitiveFS_HandlesNonexistentDir(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	result := cache.IsCaseInsensitive("/nonexistent/path/that/does/not/exist")

	assert.False(t, result, "nonexistent directory should default to case-sensitive (safe)")
}

func TestFSCaseCache_Reset(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	tmpDir := t.TempDir()

	_ = cache.IsCaseInsensitive(tmpDir)

	cache.mu.RLock()
	_, exists := cache.cache[tmpDir]
	cache.mu.RUnlock()
	assert.True(t, exists, "result should be cached before reset")

	cache.Reset()

	cache.mu.RLock()
	_, existsAfter := cache.cache[tmpDir]
	cache.mu.RUnlock()
	assert.False(t, existsAfter, "cache should be empty after reset")

	result := cache.IsCaseInsensitive(tmpDir)
	assert.NotNil(t, result)
}
