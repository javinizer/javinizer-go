package fscase

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSCaseCache_ResetUncovered(t *testing.T) {
	cache := NewFSCaseCache(nil)
	cache.cache["/test"] = true
	cache.Reset()
	assert.Empty(t, cache.cache)
}

func TestFSCaseCache_AcquireProbeLockUncovered(t *testing.T) {
	cache := NewFSCaseCache(nil)
	mu := cache.acquireProbeLock("/test")
	require.NotNil(t, mu)
	mu2 := cache.acquireProbeLock("/test")
	assert.Same(t, mu, mu2, "same path should return same mutex")
}

func TestFSCaseCache_IsCaseInsensitive_CachedUncovered(t *testing.T) {
	cache := NewFSCaseCache(nil)
	cache.cache["/cached_path"] = true
	result := cache.IsCaseInsensitive("/cached_path")
	assert.True(t, result)
}

func TestFSCaseCache_IsCaseInsensitiveFS_MemMapFs(t *testing.T) {
	t.Run("MemMapFs is case-sensitive (returns false)", func(t *testing.T) {
		// MemMapFs is case-sensitive by default
		cache := NewFSCaseCache(afero.NewMemMapFs())
		tmpDir := t.TempDir()

		// Create the temp dir in the MemMapFs
		cache.fs.MkdirAll(tmpDir, 0755)

		result := cache.isCaseInsensitiveFS(tmpDir)
		assert.False(t, result, "MemMapFs should be case-sensitive")
	})
}

func TestFSCaseCache_IsCaseInsensitive_NilFS(t *testing.T) {
	cache := NewFSCaseCache(nil)
	tmpDir := t.TempDir()

	// This will use the OS filesystem
	result := cache.IsCaseInsensitive(tmpDir)
	_ = result // just verifying it doesn't panic
}
