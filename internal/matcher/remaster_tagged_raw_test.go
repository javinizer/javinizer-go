package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaggedRawContentIDs(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id string
		part     int
	}{
		{"[site]1rct00156h.mkv", "1RCT00156H", 0},
		{"9t28123h.mkv", "9T28123H", 0},
		{"5750360vrg00123h.mkv", "5750360VRG00123H", 0},
		{"[site]5755360vrpg00123hd-pt2.mkv", "5755360VRPG00123HD", 2},
		{"5750360vrg00123ai.mkv", "5750360VRG00123AI", 0},
		{"44a00123h.mkv", "44A00123H", 0},
		{"a00123h.mkv", "A00123H", 0},
		{"[site]44a00123hd-pt2.mkv", "44A00123HD", 2},
		{"[site]a00123hd-pt2.mkv", "A00123HD", 2},
		{"a00123ai.mkv", "A00123AI", 0},
		{"h_328a00123hd.mkv", "H_328A00123HD", 0},
		{"[site]9t28123hd-pt2.mkv", "9T28123HD", 2},
		{"[site]1rct00156hd.mkv", "1RCT00156HD", 0},
		{"[site]dv00899ai.mkv", "DV00899AI", 0},
		{"pt9 [site]1rct00156h-pt2.mkv", "1RCT00156H", 2},
		{"[site]1rct00156h-1080p.mkv", "1RCT00156H", 0},
		{"[site]h_003abc00123hd-pt2.mkv", "H_003ABC00123HD", 2},
		{"[site]n_600abc00123ai.mkv", "N_600ABC00123AI", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
			assert.Equal(t, tc.part, got.PartNumber)
			assert.Empty(t, got.RemasterMarker)
		})
	}
	assert.Empty(t, m.MatchString("[site]x1rct00156h.mkv"))
	assert.Equal(t, "IPX-535", m.MatchString("[site]1rct00156h IPX-535.mkv"))
}
