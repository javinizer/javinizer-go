package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fused bit-depth codec tag behind a separated remaster id
// (H26510BIT, X26510BIT, H2648BIT, H2658BIT — the [hx]26x head directly
// followed by the spelled-out depth) is display metadata, not a
// replacement catalog id: the bare [hx]26[3-9] literal stops at the word
// boundary before the depth, so the compound rode the content-id shape
// and the trailing catalog grammar as the whole id (round 38, extending
// the codec class the way rounds 33a/36a extended the audio family).
// Both entry points agree on every head, depth, and casing, the
// hi-profile spellings (HI444 and its siblings), and the separated
// spellings, which already classified as metadata because their split
// tokens carry too few digits for any id tier to claim.
func TestFusedBitDepthCodecTagVetoed(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{
		// The finding's spellings: the fused codec/bit-depth compound.
		"ABC.123.HD H26510BIT.mkv",
		"ABC.123.HD X26510BIT.mkv",
		"ABC.123.HD H2648BIT.mkv",
		"ABC.123.HD H2658BIT.mkv",
		// The probed variants: every head, the 8/10/12 depths, and the
		// lowercase spellings.
		"ABC.123.HD H26410BIT.mkv",
		"ABC.123.HD H26610BIT.mkv",
		"ABC.123.HD x26610BIT.mkv",
		"ABC.123.HD X26410BIT.mkv",
		"ABC.123.HD X2648BIT.mkv",
		"ABC.123.HD X2668BIT.mkv",
		"ABC.123.HD H26412BIT.mkv",
		"ABC.123.HD H26512BIT.mkv",
		"ABC.123.HD X26512BIT.mkv",
		"ABC.123.HD H2658bit.mkv",
		"ABC.123.HD X2658BIT.mkv",
		"ABC.123.HD h26510bit.mkv",
		"ABC.123.HD x26510BIT.mkv",
		"ABC-123-HD H26510BIT.mkv",
		// The x264 hi-profile spellings: the bare HI444 satisfied the
		// builtin amateur pattern as a replacement id, and the profile
		// siblings ride the same family.
		"ABC.123.HD HI444.mkv",
		"ABC.123.HD HI444PP.mkv",
		"ABC.123.HD Hi444PP.mkv",
		"ABC.123.HD HI10.mkv",
		"ABC.123.HD Hi10.mkv",
		"ABC.123.HD HI10P.mkv",
		// The separated spellings stay metadata: no split token carries
		// the 4-5 digits the content-id shape and the trailing catalog
		// grammar require.
		"ABC.123.HD H265 10BIT.mkv",
		"ABC.123.HD H265 10bit.mkv",
		"ABC.123.HD X265 10BIT.mkv",
		"ABC.123.HD H265-10BIT.mkv",
		"ABC.123.HD H.26510BIT.mkv",
		"ABC.123.HD H265.10BIT.mkv",
		// The bare codec heads stay metadata exactly as before: the
		// optional suffix changes nothing when no depth rides behind.
		"ABC.123.HD H265.mkv",
		"ABC.123.HD H264.mkv",
		"ABC.123.HD X264.mkv",
		"ABC.123.HD X265.mkv",
		"ABC.123.HD H266.mkv",
		"ABC.123.HD X266.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
			assert.Equal(t, "builtin", got.MatchedBy)
			assert.Equal(t, "ABC-123H", m.MatchString(name))
		})
	}
	// The AI and H markers keep their spellings on both entry points.
	got := matchOne(t, m, "ABC.123.AI H26510BIT.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "ABC-123AI", got.ID)
	assert.Equal(t, "AI", got.RemasterMarker)
	assert.Equal(t, "ABC-123AI", m.MatchString("ABC.123.AI H26510BIT.mkv"))
	got = matchOne(t, m, "ABC.123.H H26510BIT.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "ABC-123H", got.ID)
	assert.Equal(t, "H", got.RemasterMarker)
	assert.Equal(t, "ABC-123H", m.MatchString("ABC.123.H H26510BIT.mkv"))
	// A strong raw content id displaces the tag on both entry points,
	// exactly as it displaces the bare codec literals.
	assert.Equal(t, "1RCT00156H", m.MatchString("H26510BIT 1rct00156h.mkv"))
	assert.Equal(t, "1RCT00156H", m.MatchString("HI444 1rct00156h.mkv"))
}

// The fused bit-depth veto stays bounded: the bit word is required and
// the depth enumerated, so the residual id-shaped spellings keep their
// id grammar, the word-boundary protections keep the near-miss
// spellings, and a lone tag matches nothing at all.
func TestFusedBitDepthVetoBounds(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ name, id string }{
		// The bit word is required: bare depth digits are the real h
		// series' fused ids, and the abbreviated depth keeps the same
		// shape.
		{"ABC.123.HD H26510.mkv", "H26510"},
		{"ABC.123.HD H26510b.mkv", "H26510B"},
		// The depth is enumerated: a junk depth fails the vocabulary
		// and keeps the content-id shape's claim.
		{"ABC.123.HD H26599BIT.mkv", "H26599BIT"},
		// The trailing word boundary: longer digit runs and longer
		// letter tails never satisfied any id tier and stay unclaimed
		// metadata behind the separated id.
		{"ABC.123.HD H2651080.mkv", "ABC-123H"},
		{"ABC.123.HD H26510BITS.mkv", "ABC-123H"},
		// The hi-profile shapes keep their amateur-tier spellings: the
		// hi head without profile digits is a short-prefix id shape,
		// and the trailing resolution run keeps the boundary failing.
		{"ABC.123.HD HI1080.mkv", "HI1080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
	// A leading letter never offers the vocabulary its word boundary,
	// so the near-miss compound keeps id grammar like XDTSHD192 does.
	assert.Equal(t, "XH26510BIT", m.MatchString("ABC.123.HD XH26510BIT.mkv"))
	// A numeric prefix keeps the raw content id: the compound never
	// displaces the marker-bearing raw id it rides inside.
	assert.Equal(t, "1H26510BIT", m.MatchString("1h26510bit.mkv"))
	// A lone fused tag is metadata, not an id: no tier claims it.
	assert.Nil(t, matchOne(t, m, "H26510BIT.mkv"))
	assert.Empty(t, m.MatchString("H26510BIT.mkv"))
	assert.Empty(t, m.MatchString("h26510bit.mkv"))
}
