package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fused resolution tag carrying the progressive/interlaced suffix
// behind a separated remaster id (FHD1080P, UHD2160P — the fhd/uhd
// head, the 2-4 digit resolution number, and the single trailing P or
// I) is display metadata, not a replacement catalog id: the bare
// fhd/uhd+digits alternatives stop at the word boundary before the
// suffix letter, so the suffixed token rode the content-id shape and
// the trailing catalog grammar as the whole id (round 44, extending
// the resolution vocabulary the way round 38 extended the codec
// class). Both entry points agree on every head, number, suffix, and
// casing, and the hyphenated sibling keeps its existing metadata
// parse via the trailing resolution grammar.
func TestFusedResolutionSuffixTagVetoed(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{
		// The finding's spellings: the fused head+resolution+P compound.
		"ABC.123.HD FHD1080P.mkv",
		"ABC.123.HD UHD2160P.mkv",
		// The probed variants: the interlaced suffix, the 3-4 digit
		// numbers, both heads, and the lowercase spellings.
		"ABC.123.HD FHD1080I.mkv",
		"ABC.123.HD UHD2160I.mkv",
		"ABC.123.HD FHD720P.mkv",
		"ABC.123.HD FHD576I.mkv",
		"ABC.123.HD FHD4320P.mkv",
		"ABC.123.HD UHD3840P.mkv",
		"ABC.123.HD fhd1080p.mkv",
		"ABC.123.HD uhd2160i.mkv",
		"abc.123.hd fhd1080p.mkv",
		// The hyphenated sibling already classified as metadata via
		// the trailing resolution grammar.
		"ABC.123.HD FHD-1080P.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
			assert.Equal(t, "ABC-123H", m.MatchString(name))
		})
	}
}

// The suffix veto stays bounded. The bare unsuffixed forms and the
// hyphenated sibling keep their existing metadata parse behind the
// separated id, the hd head's bare resolution spellings keep id
// grammar per the round-12 decision (hd is a real series in the
// r18.dev content-id prefix lookup; see the r33a pins), the
// fps-bearing and longer tails keep id grammar, and a bare suffixed
// token alone matches nothing — the tag is metadata, not an id, so the
// content-id tier no longer claims it unless a raw content id appears
// elsewhere in the name.
func TestFusedResolutionSuffixVetoBounds(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	// Controls: the bare fhd/uhd vocabulary and the hyphenated sibling
	// stay metadata behind the separated remaster id, and the
	// fps-bearing fused tail never offered the trailing catalog
	// grammar a candidate (P60 is not a letter run), so the separated
	// id stands.
	for _, name := range []string{
		"ABC.123.HD FHD1080.mkv",
		"ABC.123.HD UHD2160.mkv",
		"ABC.123.HD FHD-1080.mkv",
		"ABC.123.HD FHD1080P60.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
		})
	}
	// The hd head is a real series: its bare resolution spellings keep
	// matching as ids, exactly like the r33a HD1080 pin, and longer
	// tails fail the vocabulary's word boundary and keep id grammar.
	for _, tc := range []struct{ name, id string }{
		{"ABC.123.HD HD1080P.mkv", "HD1080P"},
		{"ABC.123.HD HD1080I.mkv", "HD1080I"},
		{"ABC.123.HD FHD1080PQ.mkv", "FHD1080PQ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
	// A bare suffixed token alone matches nothing on either entry point:
	// FHD1080P was id grammar via the content-id tier before round 44,
	// and the tag is metadata now — the same parse the separated phrase
	// (FHD 1080.mkv) already pins. A raw content id elsewhere in the name
	// still outranks the tag on both entry points.
	assert.Nil(t, matchOne(t, m, "FHD1080P.mkv"))
	assert.Equal(t, "", m.MatchString("FHD1080P.mkv"))
	raw := matchOne(t, m, "FHD1080P 1rct00156h.mkv")
	require.NotNil(t, raw)
	assert.Equal(t, "1RCT00156H", raw.ID)
	assert.Equal(t, "1RCT00156H", m.MatchString("FHD1080P 1rct00156h.mkv"))
}
