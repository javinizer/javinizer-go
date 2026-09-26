package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/matcher"
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

// foldNameExtension folds only the extension's spelling: the stem keeps
// its raw form (for the matcher's raw-first custom-regex tier), names
// without an extension are returned unchanged, and a fullwidth slash (／)
// inside a filename stays a filename character.
func TestFoldNameExtension(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"RCT-156-HD-2．ｍｋｖ", "RCT-156-HD-2.mkv"},
		{"ＲＣＴ-156-ＨＤ-２．ｍｋｖ", "ＲＣＴ-156-ＨＤ-２.mkv"},     // stem keeps the raw spelling
		{"ＲＣＴ-156．ＭＫＶ", "ＲＣＴ-156.MKV"},               // folded case is preserved
		{"ＲＣＴ-156-ＨＤ.mkv", "ＲＣＴ-156-ＨＤ.mkv"},         // ASCII extension: nothing to fold
		{"plain.mp4", "plain.mp4"},                   // ASCII unchanged
		{"ＲＣＴ-156", "ＲＣＴ-156"},                       // no extension: raw name kept
		{"IPX-535／sample．ｍｋｖ", "IPX-535／sample.mkv"}, // fullwidth slash stays in the stem
		{"clip．", "clip."},                           // lone fullwidth dot folds
	} {
		assert.Equal(t, tc.want, foldNameExtension(tc.name), tc.name)
		assert.True(t, strings.HasSuffix(foldNameExtension(tc.name), filepath.Ext(foldNameExtension(tc.name))),
			"foldNameExtension(%q): the derived extension must be a literal suffix of the emitted name", tc.name)
	}
}

// A file with a fullwidth extension is a video file: every scanner entry
// point includes it, and the emitted FileMatchInfo satisfies the
// Name/Extension contract — Extension is a literal suffix of Name, whose
// extension spelling is folded — while Path keeps the raw on-disk spelling
// as the source of truth for file I/O. Downstream base-name derivation
// (resolveBaseFileName, metadataArtworkStrategy.Plan) TrimSuffixes Name by
// Extension, so sidecar base names carry no ．ｍｋｖ remnant.
func TestScanner_FullwidthExtensionFoldsToASCII(t *testing.T) {
	dir := t.TempDir()
	const fwName = "RCT-156-HD-2．ｍｋｖ"
	const fwStem = "ＲＣＴ-156-ＨＤ-２．ｍｋｖ"
	for _, name := range []string{fwName, fwStem, "plain.mp4"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("data"), 0644))
	}
	cfg := &Config{Extensions: []string{".mp4", ".mkv"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	requireFile := func(t *testing.T, files []models.FileMatchInfo, rawName, wantName, wantExt string) *models.FileMatchInfo {
		t.Helper()
		for i := range files {
			if files[i].Path == filepath.Join(dir, rawName) {
				f := &files[i]
				assert.Equal(t, wantExt, f.Extension, "the folded ASCII extension lands in Extension")
				assert.Equal(t, wantName, f.Name, "Name ends in the folded extension spelling")
				assert.True(t, strings.HasSuffix(f.Name, f.Extension), "Extension must be a literal suffix of Name")
				assert.Equal(t, filepath.Join(dir, rawName), f.Path, "Path keeps the raw on-disk spelling")
				return f
			}
		}
		t.Fatalf("file %s must be scanned as a video", rawName)
		return nil
	}

	requireFullwidthFiles := func(t *testing.T, files []models.FileMatchInfo) {
		t.Helper()
		// resolveBaseFileName-style consumer probe: the sidecar base keeps
		// no fullwidth-extension remnant.
		f := requireFile(t, files, fwName, "RCT-156-HD-2.mkv", ".mkv")
		assert.Equal(t, "RCT-156-HD-2", strings.TrimSuffix(f.Name, f.Extension), "sidecar base names keep no ．ｍｋｖ remnant")
		// A fullwidth stem survives for the matcher's raw-first custom
		// regex tier; only the extension spelling folds.
		stem := requireFile(t, files, fwStem, "ＲＣＴ-156-ＨＤ-２.mkv", ".mkv")
		assert.Equal(t, "ＲＣＴ-156-ＨＤ-２", strings.TrimSuffix(stem.Name, stem.Extension))
	}

	requirePlainFile := func(t *testing.T, files []models.FileMatchInfo) {
		t.Helper()
		f := requireFile(t, files, "plain.mp4", "plain.mp4", ".mp4")
		assert.Equal(t, "plain", strings.TrimSuffix(f.Name, f.Extension), "plain ASCII files trim exactly as before")
	}

	t.Run("Scan", func(t *testing.T) {
		res, err := s.Scan(dir)
		require.NoError(t, err)
		require.Len(t, res.Files, 3)
		requireFullwidthFiles(t, res.Files)
		requirePlainFile(t, res.Files)
	})

	t.Run("ScanSingle", func(t *testing.T) {
		res, err := s.ScanSingle(dir)
		require.NoError(t, err)
		require.Len(t, res.Files, 3)
		requireFullwidthFiles(t, res.Files)
		requirePlainFile(t, res.Files)
	})

	t.Run("ScanSingleFromHandle", func(t *testing.T) {
		handle, err := os.Open(dir)
		require.NoError(t, err)
		defer handle.Close()
		res, err := s.ScanSingleFromHandle(handle, dir)
		require.NoError(t, err)
		require.Len(t, res.Files, 3)
		requireFullwidthFiles(t, res.Files)
		requirePlainFile(t, res.Files)
	})

	t.Run("Filter", func(t *testing.T) {
		filtered := s.Filter([]string{
			filepath.Join(dir, fwName),
			filepath.Join(dir, fwStem),
			filepath.Join(dir, "plain.mp4"),
		})
		require.Len(t, filtered, 3)
		requireFullwidthFiles(t, filtered)
		requirePlainFile(t, filtered)
	})

	// A fullwidth extension outside the configured video set stays
	// excluded, exactly like its ASCII counterpart.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "note．ｔｘｔ"), []byte("data"), 0644))
	res, err := s.Scan(dir)
	require.NoError(t, err)
	require.Len(t, res.Files, 3, "a non-video fullwidth extension is still rejected")
}

// The contract change must not disturb matching: MatchFile derives its stem
// with the same TrimSuffix the organizer consumers use, and the matcher
// folds internally — the emitted FileMatchInfo still resolves RCT-156H
// part 2.
func TestScanner_FullwidthFileMatchesThroughMatcher(t *testing.T) {
	dir := t.TempDir()
	const fwName = "RCT-156-HD-2．ｍｋｖ"
	require.NoError(t, os.WriteFile(filepath.Join(dir, fwName), []byte("data"), 0644))
	s := NewScanner(afero.NewOsFs(), &Config{Extensions: []string{".mkv"}})
	res, err := s.ScanSingle(dir)
	require.NoError(t, err)
	require.Len(t, res.Files, 1)
	require.Equal(t, "RCT-156-HD-2.mkv", res.Files[0].Name)
	require.Equal(t, ".mkv", res.Files[0].Extension)

	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	results := m.Match(res.Files)
	require.Len(t, results, 1, "the folded-name FileMatchInfo must still match")
	assert.Equal(t, "RCT-156H", results[0].ID)
	assert.Equal(t, "HD", results[0].RemasterMarker)
	assert.Equal(t, 2, results[0].PartNumber, "the part number behind the folded extension survives")
}

// Exclusion globs are applied to the folded basename as well as the raw
// one: the extension check admits ＡＢＣ－１２３－ｓａｍｐｌｅ．ｍｋｖ as a video
// by folding its extension, so the ASCII pattern *-sample* must skip it
// exactly as it skips its ASCII twin, while a fullwidth name that matches
// no exclusion proceeds to matching. Raw matching is preserved, so a
// fullwidth exclusion pattern still hits the raw fullwidth name where it
// always did. The emitted Name carries the folded extension spelling, so
// a fullwidth file surfaces in Files under its folded name.
func TestScanner_FullwidthExclusionFoldsToASCII(t *testing.T) {
	dir := t.TempDir()
	const fwSample = "ＡＢＣ－１２３－ｓａｍｐｌｅ．ｍｋｖ"
	const fwKeep = "ＡＢＣ－７７７．ｍｋｖ"
	const asciiSample = "abc-123-sample.mkv"
	const asciiKeep = "abc-777.mkv"
	// foldNameExtension(fwSample) / foldNameExtension(fwKeep): the names
	// the scanner emits for the fullwidth files — the raw stem plus the
	// folded extension spelling.
	const fwSampleName = "ＡＢＣ－１２３－ｓａｍｐｌｅ.mkv"
	const fwKeepName = "ＡＢＣ－７７７.mkv"
	for _, name := range []string{fwSample, fwKeep, asciiSample, asciiKeep} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("data"), 0644))
	}

	nameSet := func(t *testing.T, files []models.FileMatchInfo) map[string]bool {
		t.Helper()
		got := make(map[string]bool, len(files))
		for _, f := range files {
			got[f.Name] = true
		}
		return got
	}

	cfg := &Config{Extensions: []string{".mkv"}, ExcludePatterns: []string{"*-sample*"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	assertSampleSkipped := func(t *testing.T, files []models.FileMatchInfo) {
		t.Helper()
		got := nameSet(t, files)
		assert.False(t, got[fwSampleName], "the folded basename must trigger the ASCII exclusion glob")
		assert.False(t, got[asciiSample], "raw ASCII exclusion behavior is unchanged")
		assert.True(t, got[fwKeepName], "a fullwidth name matching no exclusion proceeds to matching")
		assert.True(t, got[asciiKeep])
	}

	t.Run("Scan", func(t *testing.T) {
		res, err := s.Scan(dir)
		require.NoError(t, err)
		assertSampleSkipped(t, res.Files)
	})

	t.Run("ScanSingle", func(t *testing.T) {
		res, err := s.ScanSingle(dir)
		require.NoError(t, err)
		assertSampleSkipped(t, res.Files)
	})

	t.Run("ScanSingleFromHandle", func(t *testing.T) {
		handle, err := os.Open(dir)
		require.NoError(t, err)
		defer handle.Close()
		res, err := s.ScanSingleFromHandle(handle, dir)
		require.NoError(t, err)
		assertSampleSkipped(t, res.Files)
	})

	t.Run("Filter", func(t *testing.T) {
		filtered := s.Filter([]string{
			filepath.Join(dir, fwSample),
			filepath.Join(dir, fwKeep),
			filepath.Join(dir, asciiSample),
			filepath.Join(dir, asciiKeep),
		})
		assertSampleSkipped(t, filtered)
	})

	t.Run("Fullwidth pattern still matches the raw name", func(t *testing.T) {
		fwScanner := NewScanner(afero.NewOsFs(), &Config{
			Extensions:      []string{".mkv"},
			ExcludePatterns: []string{"ＡＢＣ－７７７*"},
		})
		res, err := fwScanner.ScanSingle(dir)
		require.NoError(t, err)
		got := nameSet(t, res.Files)
		assert.False(t, got[fwKeepName], "a fullwidth pattern must still match the raw fullwidth name")
		assert.True(t, got[asciiKeep], "a fullwidth pattern does not start matching ASCII names")
	})
}

// ScanWithFilter applies the filter to the folded entry name as well as
// the raw one: the extension check admits ＲＣＴ－１５６－ＨＤ．ｍｋｖ as a
// video, so an ASCII filter (rct) must discover that file — and a
// fullwidth-named subdirectory containing it — exactly as it discovers the
// ASCII twin rct-777.mkv, mirroring the dual-form matching excludedByName
// applies to exclusion patterns. A filter matching neither spelling still
// excludes the file, and a fullwidth filter still matches raw fullwidth
// names without starting to match ASCII ones.
func TestScanner_FullwidthFilterFoldsToASCII(t *testing.T) {
	dir := t.TempDir()
	const fwFile = "ＲＣＴ－１５６－ＨＤ．ｍｋｖ"
	const asciiTwin = "rct-777.mkv"
	const neither = "abc-999.mkv"
	const fwDir = "ＲＣＴ　Ｓｅｒｉｅｓ"
	const plainDir = "Other Series"

	for _, name := range []string{fwFile, asciiTwin, neither} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("data"), 0644))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, fwDir), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, fwDir, fwFile), []byte("data"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, plainDir), 0755))
	// A fullwidth file under a subdirectory matching no filter spelling:
	// SkipDir fires before the file's own filter check, so the file stays
	// hidden — pre-existing semantics, unchanged by the fold.
	require.NoError(t, os.WriteFile(filepath.Join(dir, plainDir, fwFile), []byte("data"), 0644))

	s := NewScanner(afero.NewOsFs(), &Config{Extensions: []string{".mkv"}})

	scanned := func(t *testing.T, filter string) map[string]bool {
		t.Helper()
		res, err := s.ScanWithFilter(context.Background(), dir, 0, filter)
		require.NoError(t, err)
		got := make(map[string]bool, len(res.Files))
		for _, f := range res.Files {
			got[f.Path] = true
		}
		return got
	}

	t.Run("ASCII filter matches both spellings", func(t *testing.T) {
		got := scanned(t, "rct")
		assert.True(t, got[filepath.Join(dir, fwFile)], "the folded name must admit the fullwidth file to an ASCII filter")
		assert.True(t, got[filepath.Join(dir, fwDir, fwFile)], "a fullwidth-named subdirectory is admitted by its folded spelling")
		assert.True(t, got[filepath.Join(dir, asciiTwin)], "an ASCII filter still matches ASCII names")
		assert.Len(t, got, 3, "exactly the rct-spelled files are found")
	})

	t.Run("filter matching neither spelling still excludes", func(t *testing.T) {
		got := scanned(t, "xyzzy")
		assert.Empty(t, got, "a filter matching neither raw nor folded spelling excludes the file")
	})

	t.Run("fullwidth filter still matches the raw name", func(t *testing.T) {
		got := scanned(t, "ＲＣＴ")
		assert.True(t, got[filepath.Join(dir, fwFile)], "a fullwidth filter keeps matching the raw fullwidth name")
		assert.True(t, got[filepath.Join(dir, fwDir, fwFile)])
		assert.Len(t, got, 2, "a fullwidth filter does not start matching ASCII names")
	})
}
