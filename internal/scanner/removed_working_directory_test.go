package scanner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestScannerRelativePathsAfterWorkingDirectoryRemoved(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux removed-working-directory contract; Windows cannot remove its active working directory")
	}
	// Keep this test and its subtests serial: t.Chdir changes process state and
	// retains the previous directory handle for cleanup even if an assertion fails.
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	originalPWD, hadPWD := os.LookupEnv("PWD")
	parent := t.TempDir()
	emptyChild := filepath.Join(parent, "empty-cwd")
	require.Equal(t, parent, filepath.Dir(emptyChild))
	require.NoError(t, os.Mkdir(emptyChild, 0700))
	media := filepath.Join(parent, "media")
	require.NoError(t, os.Mkdir(media, 0700))
	video := filepath.Join(media, "video.mp4")
	control := filepath.Join(media, "notes.txt")
	contents := map[string][]byte{video: []byte("owned video fixture"), control: []byte("non-video sentinel")}
	before := make(map[string]os.FileInfo)
	for path, data := range contents {
		require.NoError(t, os.WriteFile(path, data, 0600))
		before[path], err = os.Stat(path)
		require.NoError(t, err)
	}
	want := models.FileMatchInfo{Path: video, Name: "video.mp4", Extension: ".mp4", Size: before[video].Size(), ModTime: before[video].ModTime()}
	scanner := NewScanner(afero.NewOsFs(), &Config{Extensions: []string{".mp4"}})
	assertScans := func(t *testing.T, directory, file string) {
		t.Helper()
		result, err := scanner.ScanWithLimits(t.Context(), directory, 0)
		require.NoError(t, err)
		require.Equal(t, []models.FileMatchInfo{want}, result.Files)
		require.Equal(t, []string{control}, result.Skipped)
		require.Equal(t, 1, result.SkippedCount)
		require.Equal(t, 2, result.TotalScanned)
		require.Empty(t, result.Errors)
		require.False(t, result.TimedOut)
		require.False(t, result.LimitReached)
		result, err = scanner.ScanSingle(file)
		require.NoError(t, err)
		require.Equal(t, []models.FileMatchInfo{want}, result.Files)
		require.Empty(t, result.Skipped)
		require.Empty(t, result.Errors)
	}
	assertRestored := func() {
		t.Helper()
		wd, err := os.Getwd()
		require.NoError(t, err)
		require.Equal(t, originalWD, wd)
		pwd, present := os.LookupEnv("PWD")
		require.Equal(t, hadPWD, present)
		require.Equal(t, originalPWD, pwd)
	}
	t.Run("unavailable cwd rejects relative paths but accepts absolute paths", func(t *testing.T) {
		t.Chdir(emptyChild)
		require.NoError(t, os.Remove(emptyChild), "remove only the newly created empty child")
		wd, err := os.Getwd()
		require.Empty(t, wd)
		require.ErrorIs(t, err, os.ErrNotExist, "establish the real OS failure before calling the scanner")
		result, err := scanner.ScanWithLimits(t.Context(), "media", 0)
		require.Nil(t, result)
		require.ErrorIs(t, err, os.ErrNotExist)
		result, err = scanner.ScanSingle(filepath.Join("media", "video.mp4"))
		require.Nil(t, result)
		require.ErrorIs(t, err, os.ErrNotExist)
		assertScans(t, media, video)
	})
	assertRestored()
	t.Run("relative scans recover from a surviving directory", func(t *testing.T) {
		t.Chdir(parent)
		assertScans(t, "media", filepath.Join("media", "video.mp4"))
	})
	assertRestored()
	for path, data := range contents {
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, data, after)
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.True(t, os.SameFile(before[path], info))
		require.Equal(t, before[path].Size(), info.Size())
		require.Equal(t, before[path].Mode(), info.Mode())
		require.Equal(t, before[path].ModTime(), info.ModTime())
	}
	require.NoDirExists(t, emptyChild)
}
