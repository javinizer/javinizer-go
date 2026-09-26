package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A streaming-platform abbreviation fused directly onto the WEB-DL head
// (NFWEB-DL1080P, AMZNWEB-DL1080P, DSNPWEB-DL1080P — round 47) is
// display metadata, not a replacement catalog id: the fused platform
// compound defeats the bare web head's leading word boundary — the
// boundary sits immediately before web, and inside NFWEB the f blocks
// it — so the prefix recognizer never extended the veto span and the
// split-off DL1080P fragment rode the catalog grammar as the whole id
// on both entry points. The enumerated platform abbreviation (nf, amzn,
// atvp, disney, dsnp, dspn, hbo, hmax, hulu, max, pmax) now carries the
// boundary itself and directly abuts the web head, so the veto span
// extends over the platform-fused compound and the vocabulary decides
// it as a whole. No nfweb, amznweb, dsnpweb, dspnweb, huluweb, hboweb,
// or maxweb series exists in the r18.dev content-id prefix lookup, so no real id
// shape is displaced, and every enumerated platform is pinned on both
// entry points behind the separated remaster id.
func TestStreamingPlatformWebCompoundVetoed(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{
		// The finding's spellings.
		"ABC.123.HD NFWEB-DL1080P.mkv",
		"ABC.123.HD AMZNWEB-DL1080P.mkv",
		"ABC.123.HD DSNPWEB-DL1080P.mkv",
		"ABC.123.HD DSPNWEB-DL1080P.mkv",
		// The probed platform variants.
		"ABC.123.HD HULUWEB-DL1080P.mkv",
		"ABC.123.HD HBOWEB-DL1080P.mkv",
		"ABC.123.HD HMAXWEB-DL1080P.mkv",
		"ABC.123.HD MAXWEB-DL1080P.mkv",
		"ABC.123.HD PMAXWEB-DL1080P.mkv",
		"ABC.123.HD ATVPWEB-DL1080P.mkv",
		"ABC.123.HD DISNEYWEB-DL1080P.mkv",
		// The rip qualifier, the interlaced suffix, the bare web head
		// without a qualifier, and a lowercase spelling.
		"ABC.123.HD NFWEBRIP1080P.mkv",
		"ABC.123.HD AMZNWEB-DL2160I.mkv",
		"ABC.123.HD NFWEB1080P.mkv",
		"abc.123.hd nfweb-dl1080p.mkv",
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

// The platform extension stays bounded. The leading-letter protections
// ride the enumeration: fweb is a real series (the round-13 pin), a
// leading letter before the platform run (XNFWEB) blocks the boundary,
// and imax — a real series ending in max — never starts the platform
// abbreviation at the boundary, so all three keep their DL fragments in
// id grammar exactly as XBD-RIP1080P does. The web head stays required
// behind the platform, so the abbreviation alone (NF1080P) keeps id
// grammar, and the platform-fused compound's own bounds mirror the web
// class's: the longer (PQ) and five-plus-digit tails keep id grammar.
// The bare web head (the round-45b pin) and the spaced platform sibling
// were already metadata, and a standalone platform-fused compound
// behaves like its bare sibling: the fused token alone matches nothing,
// the hyphenated compound alone keeps the DL fragment's id grammar
// (the veto span only extends behind a separated remaster id), and a
// raw content id elsewhere still outranks the compound.
func TestStreamingPlatformWebCompoundBounds(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ name, id string }{
		{"ABC.123.HD FWEB-DL1080P.mkv", "DL1080P"},
		{"ABC.123.HD XNFWEB-DL1080P.mkv", "DL1080P"},
		{"ABC.123.HD IMAXWEB-DL1080P.mkv", "DL1080P"},
		{"ABC.123.HD NF1080P.mkv", "NF1080P"},
		{"ABC.123.HD NFWEB-DL1080PQ.mkv", "DL1080PQ"},
		{"ABC.123.HD NFWEB-DL12345P.mkv", "DL12345P"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
	for _, name := range []string{
		"ABC.123.HD WEB-DL1080P.mkv",
		"ABC.123.HD NF WEB-DL1080P.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
		})
	}
	assert.Nil(t, matchOne(t, m, "NFWEBDL1080P.mkv"))
	assert.Equal(t, "", m.MatchString("NFWEBDL1080P.mkv"))
	hyphenated := matchOne(t, m, "NFWEB-DL1080P.mkv")
	require.NotNil(t, hyphenated)
	assert.Equal(t, "DL1080P", hyphenated.ID)
	assert.Equal(t, "DL1080P", m.MatchString("NFWEB-DL1080P.mkv"))
	for _, name := range []string{"NFWEB-DL1080P 1rct00156h.mkv", "AMZNWEB-DL1080P 1rct00156h.mkv"} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "1RCT00156H", got.ID)
			assert.Equal(t, "1RCT00156H", m.MatchString(name))
		})
	}
}
