package worker

import (
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupStaleImageCache_RemovesExpiredEntries(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	shardDir := tempDir + "/image-cache/ab"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))

	expired := shardDir + "/" + "abcd1234.jpg"
	require.NoError(t, afero.WriteFile(fs, expired, []byte("old"), 0o644))
	pastTime := time.Now().Add(-400 * time.Hour)
	require.NoError(t, fs.Chtimes(expired, pastTime, pastTime))

	fresh := shardDir + "/" + "efgh5678.png"
	require.NoError(t, afero.WriteFile(fs, fresh, []byte("new"), 0o644))

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	exists, _ := afero.Exists(fs, expired)
	assert.False(t, exists, "expired entry should be removed")

	existsFresh, _ := afero.Exists(fs, fresh)
	assert.True(t, existsFresh, "fresh entry should remain")
}

func TestCleanupStaleImageCache_RemovesEmptyShardDirs(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	shardDir := tempDir + "/image-cache/ab"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))

	expired := shardDir + "/deadbeef.jpg"
	require.NoError(t, afero.WriteFile(fs, expired, []byte("old"), 0o644))
	pastTime := time.Now().Add(-400 * time.Hour)
	require.NoError(t, fs.Chtimes(expired, pastTime, pastTime))

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	exists, _ := afero.DirExists(fs, shardDir)
	assert.False(t, exists, "empty shard dir should be removed")
}

func TestCleanupStaleImageCache_RemovesOrphanTemps(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	tmpDir := tempDir + "/image-cache/.tmp"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	orphan := tmpDir + "/orphan123"
	require.NoError(t, afero.WriteFile(fs, orphan, []byte("partial"), 0o644))
	pastTime := time.Now().Add(-200 * time.Hour)
	require.NoError(t, fs.Chtimes(orphan, pastTime, pastTime))

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	exists, _ := afero.Exists(fs, orphan)
	assert.False(t, exists, "orphan temp should be removed")
}

func TestCleanupStaleImageCache_NoopOnTTLZero(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	shardDir := tempDir + "/image-cache/ab"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, shardDir+"/deadbeef.jpg", []byte("data"), 0o644))

	removed, err := CleanupStaleImageCache(fs, tempDir, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestCleanupStaleImageCache_NoopOnMissingDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	removed, err := CleanupStaleImageCache(fs, t.TempDir(), 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}
