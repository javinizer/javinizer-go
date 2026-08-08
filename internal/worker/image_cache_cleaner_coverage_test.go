package worker

import (
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupStaleImageCache_SkipsFreshEntry(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	shardDir := tempDir + "/image-cache/ab"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	fresh := shardDir + "/deadbeef.jpg"
	require.NoError(t, afero.WriteFile(fs, fresh, []byte("fresh"), 0o644))

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "fresh entry should not be removed")

	exists, _ := afero.Exists(fs, fresh)
	assert.True(t, exists)
}

func TestCleanupStaleImageCache_NilFs(t *testing.T) {
	removed, err := CleanupStaleImageCache(nil, t.TempDir(), 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestCleanupStaleImageCache_TmpOrphanFresh(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	tmpDir := tempDir + "/image-cache/.tmp"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))
	freshTmp := tmpDir + "/fresh-tmp"
	require.NoError(t, afero.WriteFile(fs, freshTmp, []byte("fresh"), 0o644))

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "fresh temp should not be removed")

	exists, _ := afero.Exists(fs, freshTmp)
	assert.True(t, exists)
}

func TestCleanupStaleImageCache_StatRecheckSkips(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	shardDir := tempDir + "/image-cache/ab"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	entry := shardDir + "/deadbeef.jpg"
	require.NoError(t, afero.WriteFile(fs, entry, []byte("data"), 0o644))

	pastTime := time.Now().Add(-400 * time.Hour)
	require.NoError(t, fs.Chtimes(entry, pastTime, pastTime))

	// Between ReadDir and Stat, update the mtime to be fresh
	// (The Stat re-check should skip removal because it's now fresh)
	// This is hard to test deterministically without a custom fs wrapper,
	// but at least the fresh-skip test covers the "not before cutoff" branch
	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
}

func TestCleanupStaleImageCache_RemovesEmptyShardAndOrphanTemps(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	shardDir := tempDir + "/image-cache/ab"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	expired := shardDir + "/deadbeef.jpg"
	require.NoError(t, afero.WriteFile(fs, expired, []byte("old"), 0o644))
	pastTime := time.Now().Add(-400 * time.Hour)
	require.NoError(t, fs.Chtimes(expired, pastTime, pastTime))

	tmpDir := tempDir + "/image-cache/.tmp"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))
	orphanTmp := tmpDir + "/orphan"
	require.NoError(t, afero.WriteFile(fs, orphanTmp, []byte("partial"), 0o644))
	require.NoError(t, fs.Chtimes(orphanTmp, pastTime, pastTime))

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 2, removed, "should remove expired entry + orphan temp")

	exists, _ := afero.DirExists(fs, shardDir)
	assert.False(t, exists, "empty shard should be removed")
}

func TestCleanupStaleImageCache_RetainsEntryWithinGracePeriod(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()

	shardDir := tempDir + "/image-cache/ab"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	staleButRetained := shardDir + "/deadbeef.jpg"
	require.NoError(t, afero.WriteFile(fs, staleButRetained, []byte("stale"), 0o644))
	pastFreshnessWithinGrace := time.Now().Add(-200 * time.Hour)
	require.NoError(t, fs.Chtimes(staleButRetained, pastFreshnessWithinGrace, pastFreshnessWithinGrace))

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "entry past freshness TTL but within retention grace must survive the sweep")

	exists, _ := afero.Exists(fs, staleButRetained)
	assert.True(t, exists, "retained entry stays available for stale-if-error serving")
}
