package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Fullwidth ASCII spellings in JP-sourced filenames fold to halfwidth for
// extension derivation: filepath.Ext is ASCII-only, so a fullwidth extension
// (．ｍｋｖ) would otherwise derive an empty Extension and the video filter
// would reject the file as a non-video.
func TestFoldFullwidthASCII(t *testing.T) {
	assert.Equal(t, "RCT-156-HD", foldFullwidthASCII("ＲＣＴ-156-ＨＤ"))
	assert.Equal(t, ".mkv", foldFullwidthASCII("．ｍｋｖ"))
	assert.Equal(t, "RCT-156-HD", foldFullwidthASCII("RCT-156-HD"), "ASCII-only input is returned unchanged")
	assert.Equal(t, "RCT 156 アイ", foldFullwidthASCII("ＲＣＴ　１５６　アイ"), "kana survives while the ideographic space folds")
}

func TestFileExtension(t *testing.T) {
	assert.Equal(t, ".mkv", fileExtension("/v/RCT-156-HD-2．ｍｋｖ"))
	assert.Equal(t, ".mkv", fileExtension("RCT-156-HD-2．ｍｋｖ"))
	assert.Equal(t, ".mp4", fileExtension("/v/plain.mp4"))
	assert.Equal(t, "", fileExtension("/v/noext"))
}

// A file with a fullwidth extension is a video file: every scanner entry
// point includes it, and FileMatchInfo.Extension carries the folded ASCII
// extension while Name keeps the raw spelling — the matcher folds the name
// itself, and its custom regex sees the raw name first.
func TestScanner_FullwidthExtensionFoldsToASCII(t *testing.T) {
	dir := t.TempDir()
	const fwName = "RCT-156-HD-2．ｍｋｖ"
	require.NoError(t, os.WriteFile(filepath.Join(dir, fwName), []byte("data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plain.mp4"), []byte("data"), 0644))
	cfg := &Config{Extensions: []string{".mp4", ".mkv"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	requireFullwidthFile := func(t *testing.T, files []models.FileMatchInfo) {
		t.Helper()
		var found *models.FileMatchInfo
		for i := range files {
			if files[i].Name == fwName {
				found = &files[i]
			}
		}
		require.NotNil(t, found, "the fullwidth-ext file must be scanned as a video")
		assert.Equal(t, ".mkv", found.Extension, "the folded ASCII extension lands in Extension")
		assert.Equal(t, fwName, found.Name, "Name keeps the raw spelling for the matcher")
	}

	t.Run("Scan", func(t *testing.T) {
		res, err := s.Scan(dir)
		require.NoError(t, err)
		require.Len(t, res.Files, 2)
		requireFullwidthFile(t, res.Files)
	})

	t.Run("ScanSingle", func(t *testing.T) {
		res, err := s.ScanSingle(dir)
		require.NoError(t, err)
		require.Len(t, res.Files, 2)
		requireFullwidthFile(t, res.Files)
	})

	t.Run("ScanSingleFromHandle", func(t *testing.T) {
		handle, err := os.Open(dir)
		require.NoError(t, err)
		defer handle.Close()
		res, err := s.ScanSingleFromHandle(handle, dir)
		require.NoError(t, err)
		require.Len(t, res.Files, 2)
		requireFullwidthFile(t, res.Files)
	})

	t.Run("Filter", func(t *testing.T) {
		filtered := s.Filter([]string{filepath.Join(dir, fwName)})
		require.Len(t, filtered, 1)
		requireFullwidthFile(t, filtered)
	})

	// A fullwidth extension outside the configured video set stays
	// excluded, exactly like its ASCII counterpart.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "note．ｔｘｔ"), []byte("data"), 0644))
	res, err := s.Scan(dir)
	require.NoError(t, err)
	require.Len(t, res.Files, 2, "a non-video fullwidth extension is still rejected")
}
