package matcher

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFusedThreeDigitRemaster(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id, marker string
		part             int
	}{
		{"RCT156H.mkv", "RCT-156H", "H", 0},
		{"ABC12H.mkv", "ABC-12H", "H", 0},
		{"ABC12HD-pt2.mkv", "ABC-12H", "HD", 2},
		{"A12AI.mkv", "A-12AI", "AI", 0},
		{"T2812H.mkv", "T28-12H", "H", 0},
		{"ABEAUTY-123-HD.mkv", "ABEAUTY-123H", "HD", 0},
		{"ABEAUTY.123.AI-pt2.mkv", "ABEAUTY-123AI", "AI", 2},
		{"ABEAUTY123HD.mkv", "ABEAUTY-123H", "HD", 0},
		{"A123H.mkv", "A-123H", "H", 0},
		{"A-123-HD.mkv", "A-123H", "HD", 0},
		{"A.123.HD.mkv", "A-123H", "HD", 0},
		{"A_123_AI-pt2.mkv", "A-123AI", "AI", 2},
		{"A 123 HD.mkv", "A-123H", "HD", 0},
		{"T28123H.mkv", "T-28123H", "H", 0},
		{"[site]T28123HD-pt2.mkv", "T-28123H", "HD", 2},
		{"T28123AI.mkv", "T-28123AI", "AI", 0},
		{"RCT.156.HD.mkv", "RCT-156H", "HD", 0},
		{"ABC.12.HD.mkv", "ABC-12H", "HD", 0},
		{"ABC_12_AI-pt2.mkv", "ABC-12AI", "AI", 2},
		{"RCT_156_HD-pt2.mkv", "RCT-156H", "HD", 2},
		{"RCT 156 HD.mkv", "RCT-156H", "HD", 0},
		{"[site]DV.818.AI.part2.mkv", "DV-818AI", "AI", 2},
		{"T28.123.HD.mkv", "T28-123H", "HD", 0},
		{"RCT.00156.HD.mkv", "RCT-00156H", "HD", 0},
		{"RCT_156H.mkv", "RCT-156H", "H", 0},
		{"[site]RCT156HD.mkv", "RCT-156H", "HD", 0},
		{"[HD]DV818AI-pt2.mkv", "DV-818AI", "AI", 2},
		{"pt9 [site]RCT156H-pt2.mkv", "RCT-156H", "H", 2},
		{"[site]RCT156H-1080p.mkv", "RCT-156H", "H", 0},
		{"RCT156HD.mkv", "RCT-156H", "HD", 0},
		{"DV818AI.mkv", "DV-818AI", "AI", 0},
		{"RCT156HD-pt2.mkv", "RCT-156H", "HD", 2},
		{"DV818AI.part2.mkv", "DV-818AI", "AI", 2},
		{"RCT156H-1080p.mkv", "RCT-156H", "H", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, tc.part, got.PartNumber)
		})
	}
	for _, name := range []string{"ABC123Q.mkv", "RCT156HDrip.mkv", "DV818AIR.mkv", "T28123HDrip.mkv", "RCT.156.HDrip.mkv", "DV_818_AIR.mkv"} {
		assert.Nil(t, matchOne(t, m, name))
		assert.Empty(t, m.MatchString(name))
	}
}

// A fused part label directly after the marker (cd2/pt2/part3/disc1) is
// recognized in every spelling family: fused tier-1 (rct156hdcd2),
// hyphenated tier-1 (rct-156-hdcd2), and tier-2 raw content ids
// (1rct00156hcd2), consistently with the already-supported separated forms.
func TestFusedRemasterPartLabels(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id, marker, matchedBy string
		part                        int
	}{
		{"RCT156HDCD2.mkv", "RCT-156H", "HD", "builtin", 2},
		{"rct156hdcd2.mkv", "RCT-156H", "HD", "builtin", 2},
		{"RCT156HCD2.mkv", "RCT-156H", "H", "builtin", 2},
		{"ABC12HDPT2.mkv", "ABC-12H", "HD", "builtin", 2},
		{"RCT156HDPART3.mkv", "RCT-156H", "HD", "builtin", 3},
		{"T28123HDCD2.mkv", "T-28123H", "HD", "builtin", 2},
		{"rct-156-hdcd2.mkv", "RCT-156H", "HD", "builtin", 2},
		{"1rct00156hcd2.mkv", "1RCT00156H", "", "contentid", 2},
		{"1rct00156hdcd2.mkv", "1RCT00156HD", "", "contentid", 2},
		{"abeauty00123hdcd2.mkv", "ABEAUTY00123HD", "", "contentid", 2},
		{"118ipx00535cd2.mkv", "118IPX00535", "", "contentid", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, tc.part, got.PartNumber)
			assert.Equal(t, PatternExplicit, got.MultipartPattern)
			assert.True(t, got.IsMultiPart)
			assert.Equal(t, tc.matchedBy, got.MatchedBy)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
	// Separated forms keep working.
	assert.Equal(t, "RCT-156H", m.MatchString("rct156hd-cd2.mkv"))
	assert.Equal(t, "1RCT00156H", m.MatchString("1rct00156h-cd2.mkv"))
	// Bare digits after a fused marker stay unmatched: without a part label
	// they are indistinguishable from codec tails (h264) and stay rejected.
	assert.Nil(t, matchOne(t, m, "RCT156HD2.mkv"))
	assert.Empty(t, m.MatchString("RCT156HD2.mkv"))
}
