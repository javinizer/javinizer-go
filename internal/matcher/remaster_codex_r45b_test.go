package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A compound source tag carrying the progressive/interlaced suffix
// behind a separated remaster id (WEB-DL1080P, WEB1080P, BDRIP1080P,
// REMUX2160P, BR-RIP1080P — the web/remux/bluray/bd-br-dvd+rip source
// head, the 3-4 digit resolution number, and the single trailing P or
// I) is display metadata, not a replacement catalog id: the bare
// compound alternatives stop at the word boundary before the suffix
// letter, so the suffixed spelling — the DL1080P/RIP1080P fragment the
// catalog scan splits off, or the whole WEB1080P token — rode the
// content-id tier and the trailing catalog grammar as the whole id
// (round 45b, extending the source vocabulary the way round 44 extended
// the fhd/uhd resolution heads). The 4+-letter heads (bdrip, remux)
// only survived the miss through the weak word-year bail, so the
// round pins the vocabulary's decision rather than that incidental
// guard. Both entry points agree on every head, qualifier, number,
// suffix, and casing.
func TestCompoundSourceSuffixTagVetoed(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{
		// The finding's spelling: the standard WEB-DL compound plus P.
		"ABC.123.HD WEB-DL1080P.mkv",
		// The probed variants: the compact web head, the rip
		// qualifiers, and the remux/bluray heads.
		"ABC.123.HD WEB1080P.mkv",
		"ABC.123.HD BDRIP1080P.mkv",
		"ABC.123.HD REMUX2160P.mkv",
		"ABC.123.HD BR-RIP1080P.mkv",
		// The interlaced suffix and the remaining heads/qualifiers,
		// including the hyphenated, dotted, and spaced siblings.
		"ABC.123.HD WEB-DL1080I.mkv",
		"ABC.123.HD WEBRIP2160P.mkv",
		"ABC.123.HD WEBDL2160P.mkv",
		"ABC.123.HD WEB-RIP2160P.mkv",
		"ABC.123.HD WEB.RIP1080P.mkv",
		"ABC.123.HD WEB RIP1080P.mkv",
		"ABC.123.HD BLURAY1080P.mkv",
		"ABC.123.HD BRRIP720P.mkv",
		"ABC.123.HD DVDRIP480P.mkv",
		"ABC.123.HD BD-RIP2160I.mkv",
		"abc.123.hd web-dl1080p.mkv",
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

// The suffix veto stays bounded. The unsuffixed compounds keep their
// round-13 metadata parse behind the separated id, the hd head's bare
// resolution spellings keep id grammar per the round-12 decision (hd
// is a real series in the r18.dev content-id prefix lookup; see the
// r44 pins), the bd head keeps its required rip qualifier, the
// fps-bearing and longer tails keep id grammar exactly as the fhd/uhd
// heads' do, and the leading-boundary and bare-fragment protections
// ride the class unchanged.
func TestCompoundSourceSuffixVetoBounds(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	// Controls: the unsuffixed compounds and the fps-bearing tail stay
	// metadata behind the separated remaster id (the round-13 pins),
	// and the double-hyphen sibling keeps its existing metadata parse.
	for _, name := range []string{
		"ABC.123.HD WEB-DL1080.mkv",
		"ABC.123.HD WEB-DL2160.mkv",
		"ABC.123.HD WEB1080.mkv",
		"ABC.123.HD REMUX1080.mkv",
		"ABC.123.HD BDRIP1080.mkv",
		"ABC.123.HD WEB-DL1080P60.mkv",
		"ABC.123.HD WEB-DL-1080P.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
		})
	}
	// The hd head is a real series and the bd head requires its rip
	// qualifier, so their bare suffixed spellings keep matching as ids;
	// the longer (PQ) and five-plus-digit tails fail the vocabulary's
	// word boundary and digit bound and keep id grammar; and the
	// leading-letter and bare-fragment protections keep the near-miss
	// heads and the split-off fragment in id grammar.
	for _, tc := range []struct{ name, id string }{
		{"ABC.123.HD HD1080P.mkv", "HD1080P"},
		{"ABC.123.HD BD1080P.mkv", "BD1080P"},
		{"ABC.123.HD WEB-DL1080PQ.mkv", "DL1080PQ"},
		{"ABC.123.HD WEB-DL12345P.mkv", "DL12345P"},
		{"ABC.123.HD XBD-RIP1080P.mkv", "RIP1080P"},
		{"ABC.123.HD FWEB-DL1080P.mkv", "DL1080P"},
		{"ABC.123.HD DL1080P.mkv", "DL1080P"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
	// A bare fused suffixed compound token alone matches nothing on
	// either entry point: the tag is metadata, not an id, the same
	// parse the round-44 fhd sibling pins. A raw content id elsewhere
	// in the name still outranks the tag on both entry points, and a
	// hyphenated WEB-DL1080P alone keeps the DL fragment's id grammar —
	// the DL2160 precedent, since the veto span only extends behind a
	// separated remaster id's candidate scan.
	assert.Nil(t, matchOne(t, m, "WEBDL1080P.mkv"))
	assert.Equal(t, "", m.MatchString("WEBDL1080P.mkv"))
	for _, name := range []string{"WEB-DL1080P 1rct00156h.mkv", "BDRIP1080P 1rct00156h.mkv"} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "1RCT00156H", got.ID)
			assert.Equal(t, "1RCT00156H", m.MatchString(name))
		})
	}
	hyphenated := matchOne(t, m, "WEB-DL1080P.mkv")
	require.NotNil(t, hyphenated)
	assert.Equal(t, "DL1080P", hyphenated.ID)
	assert.Equal(t, "DL1080P", m.MatchString("WEB-DL1080P.mkv"))
}
