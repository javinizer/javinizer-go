package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Inject metadata failures at the existing filesystem boundary. Directory walks
// and all other operations still use real, disposable files on the host.
type scannerMetadataFailureFS struct {
	afero.Fs
	statFailurePath  string
	lstatFailurePath string
	failure          error
	calls            []string
}

func (fs *scannerMetadataFailureFS) Stat(path string) (os.FileInfo, error) {
	fs.calls = append(fs.calls, "stat:"+path)
	if path == fs.statFailurePath {
		return nil, fs.failure
	}
	return fs.Fs.Stat(path)
}

func (fs *scannerMetadataFailureFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	fs.calls = append(fs.calls, "lstat:"+path)
	if path == fs.lstatFailurePath {
		return nil, true, fs.failure
	}
	return fs.Fs.(afero.Lstater).LstatIfPossible(path)
}

func TestScannerFailureIsolation_RootMetadataFailure(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "movie.mp4")
	require.NoError(t, os.WriteFile(child, []byte("original movie"), 0600))
	cause := errors.New("root metadata unavailable")
	failure := &os.PathError{Op: "lstat", Path: root, Err: cause}
	fs := &scannerMetadataFailureFS{
		Fs:               afero.NewOsFs(),
		lstatFailurePath: root,
		failure:          failure,
	}
	s := NewScanner(fs, &Config{Extensions: []string{".mp4"}})

	result, err := s.ScanWithFilter(context.Background(), root, 0, "")
	require.ErrorIs(t, err, cause)
	assert.Same(t, failure, err)
	assert.Nil(t, result, "root failure must not look like a successful empty scan")
	assert.Equal(t, []string{"stat:" + root, "lstat:" + root}, fs.calls,
		"the initial Stat succeeds, but failed root Lstat stops before walking children")
	content, err := os.ReadFile(child)
	require.NoError(t, err)
	assert.Equal(t, "original movie", string(content))
}

func TestScannerFailureIsolation_EntryMetadataFailureContinues(t *testing.T) {
	for _, recursive := range []bool{true, false} {
		name := "single directory"
		if recursive {
			name = "recursive directory"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			before := filepath.Join(root, "a-healthy.mp4")
			failed := filepath.Join(root, "b-unreadable.mp4")
			after := filepath.Join(root, "c-healthy.mp4")
			for _, path := range []string{before, failed, after} {
				require.NoError(t, os.WriteFile(path, []byte(filepath.Base(path)), 0600))
			}
			cause := errors.New("entry metadata unavailable")
			failure := &os.PathError{Op: "lstat", Path: failed, Err: cause}
			fs := &scannerMetadataFailureFS{
				Fs:               afero.NewOsFs(),
				lstatFailurePath: failed,
				failure:          failure,
			}
			s := NewScanner(fs, &Config{Extensions: []string{".mp4"}})

			var result *ScanResult
			var err error
			if recursive {
				result, err = s.ScanWithFilter(context.Background(), root, 0, "")
			} else {
				result, err = s.ScanSingle(root)
			}
			require.NoError(t, err, "one entry failure must not abort the directory scan")
			require.NotNil(t, result)
			require.Len(t, result.Errors, 1)
			assert.ErrorIs(t, result.Errors[0], cause)
			assert.Same(t, failure, result.Errors[0])
			require.Len(t, result.Files, 2)
			for i, path := range []string{before, after} {
				info, err := os.Stat(path)
				require.NoError(t, err)
				assert.Equal(t, path, result.Files[i].Path)
				assert.Equal(t, info.Name(), result.Files[i].Name)
				assert.Equal(t, info.Size(), result.Files[i].Size)
				assert.Equal(t, info.ModTime(), result.Files[i].ModTime)
				assert.Equal(t, ".mp4", result.Files[i].Extension)
			}
			assert.Empty(t, result.Skipped, "metadata errors are not ordinary filtered files")
			assert.Zero(t, result.SkippedCount)
			assert.False(t, result.LimitReached)
			assert.False(t, result.TimedOut)
			if recursive {
				assert.Equal(t, 2, result.TotalScanned)
			}
			require.GreaterOrEqual(t, len(fs.calls), 3)
			assert.Equal(t, []string{"lstat:" + before, "lstat:" + failed, "lstat:" + after},
				fs.calls[len(fs.calls)-3:], "the sibling after the failed entry is still visited")
			content, err := os.ReadFile(failed)
			require.NoError(t, err)
			assert.Equal(t, filepath.Base(failed), string(content))
		})
	}
}

func TestScannerFailureIsolation_SingleStatAfterLstat(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			path := t.TempDir()
			if !directory {
				path = filepath.Join(path, "movie.mp4")
				require.NoError(t, os.WriteFile(path, []byte("movie"), 0600))
			}
			cause := errors.New("metadata unavailable after lstat")
			failure := &os.PathError{Op: "stat", Path: path, Err: cause}
			fs := &scannerMetadataFailureFS{
				Fs:              afero.NewOsFs(),
				statFailurePath: path,
				failure:         failure,
			}
			s := NewScanner(fs, &Config{Extensions: []string{".mp4"}})

			result, err := s.ScanSingle(path)
			require.ErrorIs(t, err, cause)
			assert.Same(t, failure, err)
			assert.Nil(t, result)
			assert.Equal(t, []string{"lstat:" + path, "stat:" + path}, fs.calls)
			_, err = os.Lstat(path)
			require.NoError(t, err, "the failure must not remove or replace the input")
		})
	}
}

func TestScannerFailureIsolation_ClosedDirectoryHandle(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "movie.mp4")
	require.NoError(t, os.WriteFile(path, []byte("movie"), 0600))
	dir, err := os.Open(root)
	require.NoError(t, err)
	require.NoError(t, dir.Close())
	// ReadDir's closed-handle sentinel differs across platforms. Compare against
	// the real handle's error rather than assuming it is always os.ErrClosed.
	_, readErr := dir.ReadDir(-1)
	require.Error(t, readErr)
	var expected *os.PathError
	require.ErrorAs(t, readErr, &expected)
	s := NewScanner(afero.NewOsFs(), &Config{Extensions: []string{".mp4"}})

	result, err := s.ScanSingleFromHandle(dir, root)
	require.ErrorIs(t, err, expected.Err)
	var actual *os.PathError
	require.ErrorAs(t, err, &actual)
	assert.Equal(t, expected.Op, actual.Op)
	assert.Equal(t, root, actual.Path)
	assert.Nil(t, result, "a read failure must not be reported as an empty directory")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "movie", string(content))
}

func TestScannerFailureIsolation_NativeSymlinksStayOutsideScan(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	healthy := filepath.Join(root, "healthy.mp4")
	target := filepath.Join(outside, "target.mp4")
	targetDir := filepath.Join(outside, "target-directory")
	nestedTarget := filepath.Join(targetDir, "nested.mp4")
	require.NoError(t, os.Mkdir(targetDir, 0700))
	for _, path := range []string{healthy, target, nestedTarget} {
		require.NoError(t, os.WriteFile(path, []byte("unchanged "+filepath.Base(path)), 0600))
	}
	fileLink := filepath.Join(root, "linked-file.mp4")
	dirLink := filepath.Join(root, "linked-directory")
	for link, destination := range map[string]string{fileLink: target, dirLink: targetDir} {
		err := os.Symlink(destination, link)
		if err != nil && runtime.GOOS == "windows" {
			t.Skipf("native symlink creation is unavailable: %v", err)
		}
		require.NoError(t, err)
	}

	for _, mode := range []string{"recursive", "single", "handle"} {
		t.Run(mode, func(t *testing.T) {
			s := NewScanner(afero.NewOsFs(), &Config{Extensions: []string{".mp4"}})
			var result *ScanResult
			var err error
			switch mode {
			case "recursive":
				result, err = s.ScanWithFilter(context.Background(), root, 0, "")
			case "single":
				result, err = s.ScanSingle(root)
			case "handle":
				dir, openErr := os.Open(root)
				require.NoError(t, openErr)
				t.Cleanup(func() { assert.NoError(t, dir.Close()) })
				result, err = s.ScanSingleFromHandle(dir, root)
				_, statErr := dir.Stat()
				assert.NoError(t, statErr, "the caller retains ownership of the handle")
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, result.Files, 1, "neither the linked file nor linked directory may be traversed")
			assert.Equal(t, healthy, result.Files[0].Path)
			assert.Empty(t, result.Errors)
			assert.Equal(t, 2, result.SkippedCount)
			assert.ElementsMatch(t, []string{fileLink, dirLink}, result.Skipped)
			for _, path := range []string{healthy, target, nestedTarget} {
				content, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				assert.Equal(t, "unchanged "+filepath.Base(path), string(content))
			}
			for link, destination := range map[string]string{fileLink: target, dirLink: targetDir} {
				actual, readErr := os.Readlink(link)
				require.NoError(t, readErr)
				assert.Equal(t, destination, actual)
			}
			entries, readErr := os.ReadDir(targetDir)
			require.NoError(t, readErr)
			require.Len(t, entries, 1)
			assert.Equal(t, "nested.mp4", entries[0].Name())
		})
	}
}
