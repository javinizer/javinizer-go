package template

// codex P2 (PR #257, round 46a): round 40a publishes the raw on-disk
// basename (RCT-156-HD．ｍｋｖ) in Movie.OriginalFileName — correct for
// metadata — which exposed the fullwidth extension to tag resolution:
// filepath.Ext is ASCII-only, so <FILENAME> expanded to the whole name
// and the organizer appended the folded .mkv on top
// (RCT-156-HD．ｍｋｖ.mkv). The filename tags now split fold-aware at tag
// resolution: <FILENAME> strips the fullwidth extension from the raw stem,
// <FILENAME_EXT> folds the extension spelling (round-30 classification
// contract), and ASCII input stays byte-identical. The metadata field
// itself is untouched (round 40a keeps it raw).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateFilenameTags_FullwidthExtension(t *testing.T) {
	e := NewEngine()

	t.Run("FILENAME strips the fullwidth extension", func(t *testing.T) {
		got, err := e.Execute("<FILENAME>", &Context{OriginalFilename: "RCT-156-HD．ｍｋｖ"})
		require.NoError(t, err)
		assert.Equal(t, "RCT-156-HD", got,
			"the stem must not carry the fullwidth extension the organizer would re-append")
	})

	t.Run("FILENAME in an NFO-style template renders the stem", func(t *testing.T) {
		got, err := e.Execute("<FILENAME>.nfo", &Context{OriginalFilename: "RCT-156-HD．ｍｋｖ"})
		require.NoError(t, err)
		assert.Equal(t, "RCT-156-HD.nfo", got)
	})

	t.Run("FILENAME_EXT folds the extension spelling", func(t *testing.T) {
		got, err := e.Execute("<FILENAME_EXT>", &Context{OriginalFilename: "RCT-156-HD．ｍｋｖ"})
		require.NoError(t, err)
		assert.Equal(t, "RCT-156-HD.mkv", got,
			"the ext-style tag yields the folded .mkv spelling, never the fullwidth one")
	})

	t.Run("FILENAMEEXT alias folds the extension spelling", func(t *testing.T) {
		got, err := e.Execute("<FILENAMEEXT>", &Context{OriginalFilename: "RCT-156-HD．ｍｋｖ"})
		require.NoError(t, err)
		assert.Equal(t, "RCT-156-HD.mkv", got)
	})

	t.Run("ASCII input is byte-identical", func(t *testing.T) {
		got, err := e.Execute("<FILENAME>", &Context{OriginalFilename: "IPX-535.mp4"})
		require.NoError(t, err)
		assert.Equal(t, "IPX-535", got)

		got, err = e.Execute("<FILENAME_EXT>", &Context{OriginalFilename: "IPX-535.mp4"})
		require.NoError(t, err)
		assert.Equal(t, "IPX-535.mp4", got)
	})

	t.Run("fullwidth stem spelling is preserved", func(t *testing.T) {
		got, err := e.Execute("<FILENAME>", &Context{OriginalFilename: "ＲＣＴ-156-ＨＤ．ｍｋｖ"})
		require.NoError(t, err)
		assert.Equal(t, "ＲＣＴ-156-ＨＤ", got,
			"only the extension boundary is fold-aware; the stem keeps its raw spelling")

		got, err = e.Execute("<FILENAME_EXT>", &Context{OriginalFilename: "ＲＣＴ-156-ＨＤ．ｍｋｖ"})
		require.NoError(t, err)
		assert.Equal(t, "ＲＣＴ-156-ＨＤ.mkv", got, "only the extension spelling folds")
	})

	t.Run("no extension, empty, and dotfiles unchanged", func(t *testing.T) {
		for _, tt := range []struct{ tag, filename, want string }{
			{"<FILENAME>", "STARS-136", "STARS-136"},
			{"<FILENAME>", "", ""},
			{"<FILENAME>", ".hidden", ".hidden"},
			{"<FILENAME_EXT>", "STARS-136", "STARS-136"},
			{"<FILENAME_EXT>", "", ""},
			{"<FILENAME_EXT>", ".hidden", ".hidden"},
			{"<FILENAME>", "videos/STARS-136.mp4", "videos/STARS-136"},
			{"<FILENAME>", "a..mkv", "a."},
		} {
			got, err := e.Execute(tt.tag, &Context{OriginalFilename: tt.filename})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got, "%s with %q", tt.tag, tt.filename)
		}
	})
}

// TestSplitRawExtension_TemplateMirror pins the template-local mirror of
// the organizer's round-32a split contract (internal/organizer
// strategy_inplace.go splitRawExtension): the extension boundary follows
// the last dot of the FOLDED spelling while the returned stem and
// extension keep their raw spellings.
func TestSplitRawExtension_TemplateMirror(t *testing.T) {
	stem, ext := splitRawExtension("RCT-156-HD．ｍｋｖ")
	assert.Equal(t, "RCT-156-HD", stem, "fullwidth extension is stripped from the raw spelling")
	assert.Equal(t, "．ｍｋｖ", ext, "the raw extension spelling is returned")

	stem, ext = splitRawExtension("RCT-156-HD.mkv")
	assert.Equal(t, "RCT-156-HD", stem, "ASCII input reduces to TrimSuffix/Ext semantics")
	assert.Equal(t, ".mkv", ext)

	stem, ext = splitRawExtension("no-extension")
	assert.Equal(t, "no-extension", stem, "no extension stays whole")
	assert.Equal(t, "", ext)

	stem, ext = splitRawExtension("a..mkv")
	assert.Equal(t, "a.", stem, "last-dot parity with filepath.Ext")
	assert.Equal(t, ".mkv", ext)

	stem, ext = splitRawExtension("ＲＣＴ-156-ＨＤ．ｍｋｖ")
	assert.Equal(t, "ＲＣＴ-156-ＨＤ", stem, "the stem keeps its fullwidth spelling")
	assert.Equal(t, "．ｍｋｖ", ext)
}

// TestFoldNameExtension_TemplateMirror pins the template-local mirror of
// the scanner's/worker's round-30 fold contract: only the extension's
// spelling folds; the stem and ASCII-only input are returned unchanged.
func TestFoldNameExtension_TemplateMirror(t *testing.T) {
	assert.Equal(t, "RCT-156-HD.mkv", foldNameExtension("RCT-156-HD．ｍｋｖ"),
		"the extension spelling folds to halfwidth")
	assert.Equal(t, "ＲＣＴ-156-ＨＤ.mkv", foldNameExtension("ＲＣＴ-156-ＨＤ．ｍｋｖ"),
		"only the extension folds, the stem keeps its raw spelling")
	assert.Equal(t, "RCT-156-HD.mkv", foldNameExtension("RCT-156-HD.mkv"),
		"ASCII-only input is returned unchanged")
	assert.Equal(t, "STARS-136", foldNameExtension("STARS-136"),
		"no extension stays whole")
	assert.Equal(t, ".hidden", foldNameExtension(".hidden"),
		"dotfiles keep their leading dot")
	assert.Equal(t, "", foldNameExtension(""))
}

// TestFoldFullwidthASCII_TemplateMirror pins the template-local fold
// shared with matcher/organizer/scanner/worker.
func TestFoldFullwidthASCII_TemplateMirror(t *testing.T) {
	assert.Equal(t, "RCT-156-HD", foldFullwidthASCII("ＲＣＴ-156-ＨＤ"))
	assert.Equal(t, ".mkv", foldFullwidthASCII("．ｍｋｖ"))
	assert.Equal(t, "RCT-156-HD", foldFullwidthASCII("RCT-156-HD"),
		"ASCII-only input is returned unchanged")
	assert.Equal(t, "RCT 156 アイ", foldFullwidthASCII("ＲＣＴ　１５６　アイ"),
		"kana survives while the ideographic space folds")
}
