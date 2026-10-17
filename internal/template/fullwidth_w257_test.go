package template

// codex P2 (PR #257, round 46a): round 40a publishes the raw on-disk
// basename (RCT-156-HD．ｍｋｖ) in Movie.OriginalFileName — correct for
// metadata — which exposed the fullwidth extension to tag resolution:
// filepath.Ext is ASCII-only, so <FILENAME> expanded to the whole name
// and the organizer appended the folded .mkv on top
// (RCT-156-HD．ｍｋｖ.mkv). The filename tags now split fold-aware at tag
// resolution: <FILENAME> strips the fullwidth extension from the raw stem,
// <FILENAME_EXT> preserves the raw extension spelling, and ASCII input
// stays byte-identical. The metadata field
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

	t.Run("FILENAME_EXT preserves the raw spelling", func(t *testing.T) {
		got, err := e.Execute("<FILENAME_EXT>", &Context{OriginalFilename: "RCT-156-HD．ｍｋｖ"})
		require.NoError(t, err)
		assert.Equal(t, "RCT-156-HD．ｍｋｖ", got,
			"the ext-style tag must name the actual original file")
	})

	t.Run("FILENAMEEXT alias preserves the raw spelling", func(t *testing.T) {
		got, err := e.Execute("<FILENAMEEXT>", &Context{OriginalFilename: "RCT-156-HD．ｍｋｖ"})
		require.NoError(t, err)
		assert.Equal(t, "RCT-156-HD．ｍｋｖ", got)
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
		assert.Equal(t, "ＲＣＴ-156-ＨＤ．ｍｋｖ", got, "the original filename stays raw")
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

// Ideographic spaces participate in matching normalization, while filename
// tags must retain the actual source spelling on either side of the split.
func TestTemplateFilenameTags_IdeographicSpace(t *testing.T) {
	filename := "作品　ＲＣＴ．ｍｋｖ"
	assert.Equal(t, "作品 RCT.mkv", foldFullwidthASCII(filename))
	stem, ext := splitRawExtension(filename)
	assert.Equal(t, "作品　ＲＣＴ", stem)
	assert.Equal(t, "．ｍｋｖ", ext)

	ctx := &Context{OriginalFilename: filename}
	for _, tt := range []struct{ tag, want string }{
		{"<FILENAME>", "作品　ＲＣＴ"},
		{"<FILENAME>.nfo", "作品　ＲＣＴ.nfo"},
		{"<FILENAME_EXT>", filename},
		{"<FILENAMEEXT>", filename},
	} {
		got, err := NewEngine().Execute(tt.tag, ctx)
		require.NoError(t, err)
		assert.Equal(t, tt.want, got, tt.tag)
		assert.Equal(t, filename, ctx.OriginalFilename, "template evaluation must not normalize source metadata")
	}
}
