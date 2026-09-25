package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fullwidth ASCII spellings (common in JP-sourced filenames) must behave like
// their halfwidth counterparts in every matcher tier, while kana and kanji
// survive untouched and plain halfwidth input is unaffected.
func TestMatchFile_FullwidthSpellingsFoldToASCII(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		wantID string
		marker string
	}{
		{"RCT-156-ＨＤ.mkv", "RCT-156H", "HD"},
		{"RCT-156-ｈｄ.mkv", "RCT-156H", "HD"},
		{"RCT-156-Ｈ.mkv", "RCT-156H", "H"},
		{"DV-818-ＡＩ.mkv", "DV-818AI", "AI"},
		{"ＲＣＴ-156-HD.mkv", "RCT-156H", "HD"},
		{"ＲＣＴ-１５６-ＨＤ.mkv", "RCT-156H", "HD"},
		{"ＲＣＴ１５６ＨＤ.mkv", "RCT-156H", "HD"},
		{"ＲＣＴ-156-ＨＤ アイ・無修正.mkv", "RCT-156H", "HD"},
		// Control: legitimately halfwidth input is unaffected.
		{"RCT-156-HD.mkv", "RCT-156H", "HD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, 0, got.PartNumber)
			assert.Empty(t, got.MultipartPattern)
			assert.Equal(t, tc.wantID, m.MatchString(tc.name))
		})
	}

	// Tier-2 raw and plain catalog spellings fold too.
	assert.Equal(t, "1RCT00156H", m.MatchString("１ｒｃｔ００１５６ｈ.mkv"))
	assert.Equal(t, "IPX-535", m.MatchString("ＩＰＸ-５３５.mkv"))

	// Kana and kanji survive the fold untouched; ASCII-only input is
	// returned unchanged.
	assert.Equal(t, "RCT-156-HD アイ・無修正", foldFullwidthASCII("ＲＣＴ-156-ＨＤ アイ・無修正"))
	assert.Equal(t, "RCT-156-HD", foldFullwidthASCII("RCT-156-HD"))
}
