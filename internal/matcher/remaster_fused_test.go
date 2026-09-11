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
	for _, name := range []string{"ABC123Q.mkv", "RCT156HDrip.mkv", "DV818AIR.mkv"} {
		assert.Nil(t, matchOne(t, m, name))
		assert.Empty(t, m.MatchString(name))
	}
}
