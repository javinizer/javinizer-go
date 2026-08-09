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

type openFailForFs struct {
	afero.Fs
	target string
	err    error
}

func (f *openFailForFs) Open(name string) (afero.File, error) {
	if name == f.target {
		return nil, f.err
	}
	return f.Fs.Open(name)
}

type statFailFs struct {
	afero.Fs
	target string
}

func (f *statFailFs) Stat(name string) (os.FileInfo, error) {
	if name == f.target {
		return nil, errors.New("stat denied")
	}
	return f.Fs.Stat(name)
}

type removeAllFailFs struct {
	afero.Fs
	target string
}

func (f *removeAllFailFs) Remove(name string) error {
	if name == f.target {
		return errors.New("remove denied")
	}
	return f.Fs.Remove(name)
}

func seedExpiredShardEntry(t *testing.T, fs afero.Fs, tempDir, shard, name string) string {
	t.Helper()
	shardDir := filepath.Join(tempDir, "image-cache", shard)
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	entry := filepath.Join(shardDir, name)
	require.NoError(t, afero.WriteFile(fs, entry, []byte("old"), 0o644))
	past := time.Now().Add(-400 * time.Hour)
	require.NoError(t, fs.Chtimes(entry, past, past))
	return entry
}

func TestCleanupBranch_RootReadDirGenericError(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "image-cache")
	fs := &openFailForFs{Fs: afero.NewMemMapFs(), target: root, err: errors.New("io error")}

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.Error(t, err)
	assert.Equal(t, 0, removed)
	assert.Contains(t, err.Error(), "read image-cache dir")
}

func TestCleanupBranch_ShardReadDirNotExistSkipped(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	seedExpiredShardEntry(t, mem, tempDir, "ab", "deadbeef.jpg")
	shardPath := filepath.Join(tempDir, "image-cache", "ab")
	fs := &openFailForFs{Fs: mem, target: shardPath, err: &os.PathError{Op: "open", Path: shardPath, Err: os.ErrNotExist}}

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestCleanupBranch_ShardReadDirGenericErrorWarned(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	seedExpiredShardEntry(t, mem, tempDir, "cd", "deadbeef.jpg")
	shardPath := filepath.Join(tempDir, "image-cache", "cd")
	fs := &openFailForFs{Fs: mem, target: shardPath, err: errors.New("perm denied")}

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestCleanupBranch_DirEntriesInsideShardSkipped(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	shardDir := filepath.Join(tempDir, "image-cache", "ef")
	require.NoError(t, mem.MkdirAll(filepath.Join(shardDir, "nested"), 0o755))

	removed, err := CleanupStaleImageCache(mem, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	exists, _ := afero.DirExists(mem, filepath.Join(shardDir, "nested"))
	assert.True(t, exists)
}

func TestCleanupBranch_StatFailureSkipsRemoval(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	entry := seedExpiredShardEntry(t, mem, tempDir, "ab", "deadbeef.jpg")
	fs := &statFailFs{Fs: mem, target: entry}

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	exists, _ := afero.Exists(mem, entry)
	assert.True(t, exists)
}

func TestCleanupBranch_RemoveFailureWarned(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	entry := seedExpiredShardEntry(t, mem, tempDir, "ab", "deadbeef.jpg")
	fs := &removeAllFailFs{Fs: mem, target: entry}

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	exists, _ := afero.Exists(mem, entry)
	assert.True(t, exists)
}

func seedExpiredTmpEntry(t *testing.T, fs afero.Fs, tempDir, name string) string {
	t.Helper()
	tmpDir := filepath.Join(tempDir, "image-cache", ".tmp")
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))
	entry := filepath.Join(tmpDir, name)
	require.NoError(t, afero.WriteFile(fs, entry, []byte("partial"), 0o644))
	past := time.Now().Add(-400 * time.Hour)
	require.NoError(t, fs.Chtimes(entry, past, past))
	return entry
}

func TestCleanupBranch_TmpStatFailureSkipsRemoval(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	entry := seedExpiredTmpEntry(t, mem, tempDir, "orphan")
	fs := &statFailFs{Fs: mem, target: entry}

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	exists, _ := afero.Exists(mem, entry)
	assert.True(t, exists)
}

func TestCleanupBranch_TmpRemoveFailureWarned(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	entry := seedExpiredTmpEntry(t, mem, tempDir, "orphan")
	fs := &removeAllFailFs{Fs: mem, target: entry}

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	exists, _ := afero.Exists(mem, entry)
	assert.True(t, exists)
}

type plantOnShardRemoveFs struct {
	afero.Fs
	shardDir string
	planted  bool
}

func (f *plantOnShardRemoveFs) Remove(name string) error {
	if name == f.shardDir && !f.planted {
		f.planted = true
		fresher := filepath.Join(f.shardDir, "just-landed.jpg")
		if err := afero.WriteFile(f.Fs, fresher, []byte("fresh"), 0o644); err != nil {
			return err
		}
	}
	return f.Fs.Remove(name)
}

func TestCleanupBranch_ShardDirRemovalSurvivesConcurrentFill(t *testing.T) {
	mem := afero.NewMemMapFs()
	tempDir := t.TempDir()
	expired := seedExpiredShardEntry(t, mem, tempDir, "ab", "deadbeef.jpg")
	shardDir := filepath.Join(tempDir, "image-cache", "ab")
	fs := &plantOnShardRemoveFs{Fs: mem, shardDir: shardDir}

	removed, err := CleanupStaleImageCache(fs, tempDir, 168*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "expired entry still removed")

	planted := filepath.Join(shardDir, "just-landed.jpg")
	exists, _ := afero.Exists(mem, planted)
	assert.True(t, exists, "fresh entry renamed into the shard mid-sweep must survive (fs.Remove refuses non-empty dirs)")
	assert.True(t, fs.planted)
	exists, _ = afero.Exists(mem, expired)
	assert.False(t, exists)
}
