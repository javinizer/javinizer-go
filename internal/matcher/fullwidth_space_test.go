package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ideographic space (U+3000) is the fullwidth counterpart of ' ': it must
// fold to a plain halfwidth space so JP-sourced spellings that separate the
// series, number and marker with fullwidth spaces behave exactly like their
// ASCII spellings in every matcher tier. Kana and kanji around the space
// survive the fold untouched.
func TestFoldFullwidthASCII_IdeographicSpace(t *testing.T) {
	assert.Equal(t, "RCT 156 HD", foldFullwidthASCII("ＲＣＴ　１５６　ＨＤ"))
	assert.Equal(t, " ", foldFullwidthASCII("　"), "a lone ideographic space folds to a plain space")
	assert.Equal(t, "RCT 156 アイ", foldFullwidthASCII("ＲＣＴ　１５６　アイ"), "kana survives while the ideographic space folds")
}

// A filename whose series/number/marker separators are fullwidth spaces
// matches the same release the halfwidth spelling does.
func TestMatchFile_FullwidthSpaceSeparators(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "ＲＣＴ　１５６　ＨＤ.mkv")
	require.NotNil(t, got, "fullwidth-space-separated spelling must match like its halfwidth counterpart")
	assert.Equal(t, "RCT-156H", got.ID)
	assert.Equal(t, "HD", got.RemasterMarker)
	assert.Equal(t, 0, got.PartNumber)
	assert.Empty(t, got.MultipartPattern)
	assert.Equal(t, "RCT-156H", m.MatchString("ＲＣＴ　１５６　ＨＤ.mkv"))
}
