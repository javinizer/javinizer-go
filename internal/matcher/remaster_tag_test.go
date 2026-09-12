package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemasterTrailingTags(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id, marker, pattern string
		part                      int
	}{
		{"RCT-156-HD-pt2.mkv", "RCT-156H", "HD", PatternExplicit, 2},
		{"DV-818-AI.part2.mkv", "DV-818AI", "AI", PatternExplicit, 2},
		{"RCT-156H-pt2.mkv", "RCT-156H", "H", PatternExplicit, 2},
		{"RCT-156-HD-2.mkv", "RCT-156H", "HD", PatternExplicit, 2},
		{"RCT-156-HD-1080p.mkv", "RCT-156H", "HD", PatternNone, 0},
		{"RCT-156H-1080p.mkv", "RCT-156H", "H", PatternNone, 0},
		{"DV-818-AI_4k.mkv", "DV-818AI", "AI", PatternNone, 0},
		{"RCT-156-HD-pt2-1080p.mkv", "RCT-156H", "HD", PatternExplicit, 2},
		{"pt9-RCT-156-HD-pt2.mkv", "RCT-156H", "HD", PatternExplicit, 2},
		{"RCT-156-HD[4K].mkv", "RCT-156H", "HD", PatternNone, 0},
		{"DV-818AI[1080p].mkv", "DV-818AI", "AI", PatternNone, 0},
		{"RCT-156-HD(4K).mkv", "RCT-156H", "HD", PatternNone, 0},
		{"ABC12H[4K].mkv", "ABC-12H", "H", PatternNone, 0},
		{"RCT-156-HDrip[4K].mkv", "RCT-156", "", PatternNone, 0},
		{"RCT-156 HDrip 1080p.mkv", "RCT-156", "", PatternNone, 0},
		{"RCT-156HDTV.mkv", "RCT-156", "", PatternNone, 0},
		{"h_003abc00123hd.pt2.mkv", "H_003ABC00123HD", "", PatternExplicit, 2},
		{"IPX535ZH.mkv", "IPX-535ZH", "H", PatternNone, 0},
		{"IPX.535Z.HD.mkv", "IPX-535ZH", "HD", PatternNone, 0},
		{"ABC-1-HD.mkv", "ABC-1H", "HD", PatternNone, 0},
		{"ABC-123456-HD.mkv", "ABC-123456H", "HD", PatternNone, 0},
		{"[RCT-156-HD].mkv", "RCT-156H", "HD", PatternNone, 0},
		{"(DV-818AI).mkv", "DV-818AI", "AI", PatternNone, 0},
		{"[IPX535ZH].mkv", "IPX-535ZH", "H", PatternNone, 0},
		{"1abc123456h.mkv", "1ABC123456H", "", PatternNone, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, tc.part, got.PartNumber)
			assert.Equal(t, tc.pattern, got.MultipartPattern)
			assert.Equal(t, tc.pattern == PatternExplicit, got.IsMultiPart)
		})
	}
	group := []MatchResult{*matchOne(t, m, "ABC-123-A.mkv"), *matchOne(t, m, "ABC-123-B.mkv"), *matchOne(t, m, "ABC-123H-pt2.mkv")}
	validated := ValidateMultipartInDirectory(group)
	assert.Equal(t, "ABC-123H", validated[2].ID)
	assert.Equal(t, 2, validated[2].PartNumber)
	assert.True(t, validated[2].IsMultiPart)
}
