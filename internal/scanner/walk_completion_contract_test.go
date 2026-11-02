package scanner_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// A real filesystem can lose an admitted root before the native walk starts.
// The adapter performs that transition synchronously at the public metadata
// boundary, removing only the empty directory created specifically by this test.
type disappearingWalkRootFS struct {
	afero.Fs
	ownedRoot string
	removed   bool
}

func (fs *disappearingWalkRootFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	info, possible, err := fs.Fs.(afero.Lstater).LstatIfPossible(path)
	if err == nil && path == fs.ownedRoot && !fs.removed {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, possible, fmt.Errorf("owned walk fixture is not a non-link directory")
		}
		// Remove (not RemoveAll) refuses any unexpected contents.
		if err := os.Remove(fs.ownedRoot); err != nil {
			return nil, possible, err
		}
		fs.removed = true
	}
	return info, possible, err
}

func TestScanWalkDisappearedRootIsReportedWithoutLosingResult(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owned-empty-scan-root")
	foreign := filepath.Join(parent, "keep.mp4")
	require.NoError(t, os.Mkdir(root, 0700))
	require.NoError(t, os.WriteFile(foreign, []byte("outside scan boundary"), 0600))
	before, err := os.Stat(foreign)
	require.NoError(t, err)
	fs := &disappearingWalkRootFS{Fs: afero.NewOsFs(), ownedRoot: root}
	s := scanner.NewScanner(fs, &scanner.Config{Extensions: []string{".mp4"}})

	result, err := s.ScanWithFilter(context.Background(), root, 0, "")
	require.NoError(t, err, "walk failures belong to the partial result, unlike admission failures")
	require.True(t, fs.removed)
	require.NotNil(t, result)
	require.Len(t, result.Errors, 1)
	require.ErrorIs(t, result.Errors[0], os.ErrNotExist)
	var pathErr *os.PathError
	require.ErrorAs(t, result.Errors[0], &pathErr)
	require.Equal(t, root, pathErr.Path)
	require.Empty(t, result.Files)
	require.Empty(t, result.Skipped)
	require.Zero(t, result.SkippedCount)
	require.Zero(t, result.TotalScanned)
	require.False(t, result.LimitReached)
	require.False(t, result.TimedOut)

	// Once the owned root exists again, the same scanner remains usable.
	require.NoError(t, os.Mkdir(root, 0700))
	movie := filepath.Join(root, "retry.mp4")
	require.NoError(t, os.WriteFile(movie, []byte("retry video"), 0600))
	retry, err := s.Scan(root)
	require.NoError(t, err)
	require.Empty(t, retry.Errors)
	require.Len(t, retry.Files, 1)
	require.Equal(t, movie, retry.Files[0].Path)
	after, err := os.Stat(foreign)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	bytes, err := os.ReadFile(foreign)
	require.NoError(t, err)
	require.Equal(t, "outside scan boundary", string(bytes))
}

func TestScanWalkSkipControlsRetainPartialResults(t *testing.T) {
	root := t.TempDir()
	const count = 120
	for i := 0; i < count; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("match-%03d.mp4", i)), []byte("video"), 0600))
	}
	excluded := filepath.Join(root, "z-excluded")
	require.NoError(t, os.Mkdir(excluded, 0700))
	excludedMovie := filepath.Join(excluded, "match-hidden.mp4")
	require.NoError(t, os.WriteFile(excludedMovie, []byte("must not traverse filtered directory"), 0600))
	s := scanner.NewScanner(afero.NewOsFs(), &scanner.Config{Extensions: []string{".mp4"}})

	t.Run("directory filter continues siblings", func(t *testing.T) {
		result, err := s.ScanWithFilter(context.Background(), root, 0, "match")
		require.NoError(t, err)
		require.Len(t, result.Files, count)
		require.Equal(t, count, result.TotalScanned)
		require.Empty(t, result.Errors)
		require.False(t, result.LimitReached)
		require.False(t, result.TimedOut)
		for _, file := range result.Files {
			require.Equal(t, root, filepath.Dir(file.Path))
		}
	})
	t.Run("file limit returns selected prefix", func(t *testing.T) {
		result, err := s.ScanWithLimits(context.Background(), root, 3)
		require.NoError(t, err)
		require.Len(t, result.Files, 3)
		require.Equal(t, 3, result.TotalScanned)
		require.True(t, result.LimitReached)
		require.False(t, result.TimedOut)
		require.Empty(t, result.Errors)
		for i, file := range result.Files {
			require.Equal(t, filepath.Join(root, fmt.Sprintf("match-%03d.mp4", i)), file.Path)
		}
	})
	t.Run("cancellation retains bounded prefix", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := s.ScanWithLimits(ctx, root, 0)
		require.NoError(t, err)
		require.True(t, result.TimedOut)
		require.False(t, result.LimitReached)
		require.Less(t, len(result.Files), count)
		require.Equal(t, len(result.Files), result.TotalScanned)
		require.Empty(t, result.Errors)
		for i, file := range result.Files {
			require.Equal(t, filepath.Join(root, fmt.Sprintf("match-%03d.mp4", i)), file.Path)
		}
	})
	// Neither stopping nor filtering consumes any input.
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, count+1)
	bytes, err := os.ReadFile(excludedMovie)
	require.NoError(t, err)
	require.Equal(t, "must not traverse filtered directory", string(bytes))
}
