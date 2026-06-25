package fsutil

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyFileFs_Basic(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/source.txt", []byte("copy fs test content"), 0644)
	assert.NoError(t, err)

	err = CopyFileFs(fs, "/source.txt", "/dest.txt")
	assert.NoError(t, err)

	content, err := afero.ReadFile(fs, "/dest.txt")
	assert.NoError(t, err)
	assert.Equal(t, []byte("copy fs test content"), content)
}

func TestCopyFileFs_SamePath(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/source.txt", []byte("same path"), 0644)
	assert.NoError(t, err)

	err = CopyFileFs(fs, "/source.txt", "/source.txt")
	assert.NoError(t, err)
}

func TestCopyFileFs_CreatesDir(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/source.txt", []byte("nested"), 0644)
	assert.NoError(t, err)

	err = CopyFileFs(fs, "/source.txt", "/a/b/c/dest.txt")
	assert.NoError(t, err)

	content, err := afero.ReadFile(fs, "/a/b/c/dest.txt")
	assert.NoError(t, err)
	assert.Equal(t, []byte("nested"), content)
}

func TestCopyFileFs_SourceNotFound(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := CopyFileFs(fs, "/nonexistent.txt", "/dest.txt")
	assert.Error(t, err)
}

func TestMoveFileFs_MemMapFs(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/source.txt", []byte("move fs test"), 0644)
	assert.NoError(t, err)

	err = MoveFileFs(fs, "/source.txt", "/subdir/dest.txt")
	assert.NoError(t, err)

	content, err := afero.ReadFile(fs, "/subdir/dest.txt")
	assert.NoError(t, err)
	assert.Equal(t, []byte("move fs test"), content)
}

func TestMoveFileFs_SamePath(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/source.txt", []byte("same"), 0644)
	assert.NoError(t, err)

	err = MoveFileFs(fs, "/source.txt", "/source.txt")
	assert.NoError(t, err)
}

func TestCrossDeviceMoveFs(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/source.txt", []byte("cross device fs"), 0644)
	assert.NoError(t, err)

	err = crossDeviceMoveFs(fs, "/source.txt", "/dest.txt")
	assert.NoError(t, err)

	content, err := afero.ReadFile(fs, "/dest.txt")
	assert.NoError(t, err)
	assert.Equal(t, []byte("cross device fs"), content)

	_, err = fs.Stat("/source.txt")
	assert.True(t, os.IsNotExist(err))
}

func TestCrossDeviceMoveFs_SourceRemoveFailure(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/source.txt", []byte("data"), 0644)
	assert.NoError(t, err)

	readonlyFs := afero.NewReadOnlyFs(fs)
	err = crossDeviceMoveFs(readonlyFs, "/source.txt", "/dest.txt")
	assert.Error(t, err)
}

func TestCopyFileDataFs_BasicMemMap(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/source.txt", []byte("memmap copy"), 0644)
	assert.NoError(t, err)

	err = copyFileDataFs(fs, "/source.txt", "/dest.txt")
	assert.NoError(t, err)

	content, err := afero.ReadFile(fs, "/dest.txt")
	assert.NoError(t, err)
	assert.Equal(t, []byte("memmap copy"), content)
}

func TestCopyFileDataFs_SourceNotFound(t *testing.T) {
	fs := afero.NewMemMapFs()

	err := copyFileDataFs(fs, "/nonexistent.txt", "/dest.txt")
	assert.Error(t, err)
}

// failingReaderFile wraps an afero.File and makes Read return an error once a
// configurable number of bytes have been consumed, simulating a mid-stream
// read failure during a copy.
type failingReaderFile struct {
	afero.File
	limit    int64
	consumed int64
}

func (f *failingReaderFile) Read(p []byte) (int, error) {
	if f.consumed >= f.limit {
		return 0, fmt.Errorf("simulated mid-copy read failure")
	}
	remaining := f.limit - f.consumed
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := f.File.Read(p)
	f.consumed += int64(n)
	if f.consumed >= f.limit {
		return n, fmt.Errorf("simulated mid-copy read failure")
	}
	return n, err
}

// failingReadFs wraps an afero.Fs and returns a failing reader for a specific
// path so a copy can be interrupted partway through reading the source.
type failingReadFs struct {
	afero.Fs
	failPath string
	limit    int64
}

func (f *failingReadFs) Open(name string) (afero.File, error) {
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	if name == f.failPath {
		return &failingReaderFile{File: file, limit: f.limit}, nil
	}
	return file, nil
}

// TestCopyFileDataFs_MidCopyFailureLeavesNoPartialDst asserts the atomic-copy
// invariant: when a copy fails partway through, no partial destination file is
// left behind and no temp file leaks.
func TestCopyFileDataFs_MidCopyFailureLeavesNoPartialDst(t *testing.T) {
	memFs := afero.NewMemMapFs()
	src := "/src.txt"
	dst := "/sub/dst.txt"
	require.NoError(t, memFs.MkdirAll(filepath.Dir(dst), 0755))
	content := bytes.Repeat([]byte("a"), 1024)
	require.NoError(t, afero.WriteFile(memFs, src, content, 0644))

	// Source read fails after 64 bytes, mid-copy.
	fs := &failingReadFs{Fs: memFs, failPath: src, limit: 64}
	err := copyFileDataFs(fs, src, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy data")

	// No partial destination should be visible.
	exists, err := afero.Exists(memFs, dst)
	require.NoError(t, err)
	assert.False(t, exists, "destination must not exist after mid-copy failure")

	// No leftover temp file in the destination directory.
	entries, err := afero.ReadDir(memFs, filepath.Dir(dst))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no temp file should remain after failure")
	}
}

// TestCopyFileFs_MidCopyFailureLeavesNoPartialDst exercises the public entry
// point and asserts the destination directory ends up empty of dst and temps.
func TestCopyFileFs_MidCopyFailureLeavesNoPartialDst(t *testing.T) {
	memFs := afero.NewMemMapFs()
	src := "/src.txt"
	dst := "/nested/dir/dst.txt"
	content := bytes.Repeat([]byte("abcdefgh"), 128) // 1 KiB
	require.NoError(t, afero.WriteFile(memFs, src, content, 0644))

	fs := &failingReadFs{Fs: memFs, failPath: src, limit: 16}
	err := CopyFileFs(fs, src, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy data")

	exists, err := afero.Exists(memFs, dst)
	require.NoError(t, err)
	assert.False(t, exists, "destination must not exist after mid-copy failure")

	entries, err := afero.ReadDir(memFs, filepath.Dir(dst))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no temp file should remain after failure")
	}
}

// TestCopyFileDataFs_HappyPathContentEquality confirms a successful copy still
// produces byte-identical destination contents via the temp+rename path.
func TestCopyFileDataFs_HappyPathContentEquality(t *testing.T) {
	fs := afero.NewMemMapFs()
	content := bytes.Repeat([]byte("copy-me-"), 64) // 512 bytes
	require.NoError(t, afero.WriteFile(fs, "/src.txt", content, 0644))

	require.NoError(t, copyFileDataFs(fs, "/src.txt", "/dst.txt"))

	got, err := afero.ReadFile(fs, "/dst.txt")
	require.NoError(t, err)
	assert.Equal(t, content, got)

	// Source should be untouched and still readable.
	srcContent, err := afero.ReadFile(fs, "/src.txt")
	require.NoError(t, err)
	assert.Equal(t, content, srcContent)
}
