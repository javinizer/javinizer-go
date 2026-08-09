package worker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSizedShardEntry(t *testing.T, fs afero.Fs, tempDir, shard, name string, size int, age time.Duration) string {
	t.Helper()
	shardDir := filepath.Join(tempDir, "image-cache", shard)
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	entry := filepath.Join(shardDir, name)
	require.NoError(t, afero.WriteFile(fs, entry, make([]byte, size), 0o644))
	if age > 0 {
		old := time.Now().Add(-age)
		require.NoError(t, fs.Chtimes(entry, old, old))
	}
	return entry
}

func TestEvict_NoopOnLimitZeroOrNilFs(t *testing.T) {
	total, removed, err := EvictImageCacheToSize(afero.NewMemMapFs(), t.TempDir(), 0)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Zero(t, removed)

	total, removed, err = EvictImageCacheToSize(nil, t.TempDir(), 1024)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Zero(t, removed)
}

func TestEvict_NoopOnMissingRoot(t *testing.T) {
	total, removed, err := EvictImageCacheToSize(afero.NewMemMapFs(), t.TempDir(), 1024)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Zero(t, removed)
}

func TestEvict_RootReadDirGenericError(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "image-cache")
	fs := &openFailForFs{Fs: afero.NewMemMapFs(), target: root, err: errors.New("io error")}
	_, _, err := EvictImageCacheToSize(fs, tempDir, 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read image-cache dir")
}

func TestEvict_UnderLimitKeepsEverything(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	entry := seedSizedShardEntry(t, mem, tempDir, "ab", "a.jpg", 100, 0)

	total, removed, err := EvictImageCacheToSize(mem, tempDir, 1024)
	require.NoError(t, err)
	assert.Equal(t, int64(100), total)
	assert.Zero(t, removed)
	exists, _ := afero.Exists(mem, entry)
	assert.True(t, exists)
}

func TestEvict_OverLimitEvictsOldestUntilUnder(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	oldest := seedSizedShardEntry(t, mem, tempDir, "ab", "old.jpg", 400, 3*time.Hour)
	middle := seedSizedShardEntry(t, mem, tempDir, "ab", "mid.jpg", 400, 2*time.Hour)
	newest := seedSizedShardEntry(t, mem, tempDir, "cd", "new.jpg", 400, 0)

	total, removed, err := EvictImageCacheToSize(mem, tempDir, 800)
	require.NoError(t, err)
	assert.Equal(t, int64(1200), total)
	assert.Equal(t, 1, removed, "only as many oldest entries as needed are evicted")

	exists, _ := afero.Exists(mem, oldest)
	assert.False(t, exists)
	for _, keep := range []string{middle, newest} {
		exists, _ = afero.Exists(mem, keep)
		assert.True(t, exists, keep)
	}
}

func TestEvict_TmpDirNeverCountedOrEvicted(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	tmpDir := filepath.Join(tempDir, "image-cache", ".tmp")
	require.NoError(t, mem.MkdirAll(tmpDir, 0o755))
	partial := filepath.Join(tmpDir, "inflight")
	require.NoError(t, afero.WriteFile(mem, partial, make([]byte, 10<<20), 0o644))
	tiny := seedSizedShardEntry(t, mem, tempDir, "ab", "tiny.jpg", 10, 0)

	total, removed, err := EvictImageCacheToSize(mem, tempDir, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(10), total, ".tmp orphans must not count toward the quota")
	assert.Equal(t, 1, removed)
	exists, _ := afero.Exists(mem, tiny)
	assert.False(t, exists)
	exists, _ = afero.Exists(mem, partial)
	assert.True(t, exists, "in-flight temp writes are never evicted")
}

func TestEvict_ShardReadDirFailuresSkipped(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	seedSizedShardEntry(t, mem, tempDir, "ab", "a.jpg", 100, 0)
	shardPath := filepath.Join(tempDir, "image-cache", "ab")
	fs := &openFailForFs{Fs: mem, target: shardPath, err: &os.PathError{Op: "open", Path: shardPath, Err: os.ErrNotExist}}

	total, removed, err := EvictImageCacheToSize(fs, tempDir, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Zero(t, removed)

	fs2 := &openFailForFs{Fs: mem, target: shardPath, err: errors.New("perm")}
	total, removed, err = EvictImageCacheToSize(fs2, tempDir, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Zero(t, removed)
}

func TestEvict_DirEntriesInsideShardSkipped(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	require.NoError(t, mem.MkdirAll(filepath.Join(tempDir, "image-cache", "ab", "nested"), 0o755))

	total, removed, err := EvictImageCacheToSize(mem, tempDir, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Zero(t, removed)
}

func TestEvict_RemovalFailureLoggedAndContinues(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	pinned := seedSizedShardEntry(t, mem, tempDir, "ab", "pinned.jpg", 400, 3*time.Hour)
	other := seedSizedShardEntry(t, mem, tempDir, "ab", "other.jpg", 400, 2*time.Hour)
	fs := &removeAllFailFs{Fs: mem, target: pinned}

	total, removed, err := EvictImageCacheToSize(fs, tempDir, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(800), total)
	assert.Equal(t, 1, removed, "sweep continues past the failed removal and evicts the next-oldest")
	exists, _ := afero.Exists(mem, pinned)
	assert.True(t, exists)
	exists, _ = afero.Exists(mem, other)
	assert.False(t, exists)
}

func TestEvict_KeepPathExcluded(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	oldest := seedSizedShardEntry(t, mem, tempDir, "ab", "old.jpg", 400, 3*time.Hour)
	middle := seedSizedShardEntry(t, mem, tempDir, "ab", "mid.jpg", 400, 2*time.Hour)
	newest := seedSizedShardEntry(t, mem, tempDir, "cd", "new.jpg", 400, 0)

	total, removed, err := EvictImageCacheToSize(mem, tempDir, 800, oldest)
	require.NoError(t, err)
	assert.Equal(t, int64(1200), total)
	assert.Equal(t, 1, removed)

	exists, _ := afero.Exists(mem, oldest)
	assert.True(t, exists, "the protected entry survives even though it is oldest")
	exists, _ = afero.Exists(mem, middle)
	assert.False(t, exists, "eviction proceeds to the next-oldest unprotected entry")
	exists, _ = afero.Exists(mem, newest)
	assert.True(t, exists)
}

func TestEvict_OnlyProtectedEntryStaysOverQuota(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	sole := seedSizedShardEntry(t, mem, tempDir, "ab", "sole.jpg", 400, 0)

	total, removed, err := EvictImageCacheToSize(mem, tempDir, 100, sole)
	require.NoError(t, err)
	assert.Equal(t, int64(400), total)
	assert.Zero(t, removed, "protected entries are never evicted; the next fill re-applies pressure")
	exists, _ := afero.Exists(mem, sole)
	assert.True(t, exists)
}
