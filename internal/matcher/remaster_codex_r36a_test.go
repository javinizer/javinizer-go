package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A real catalog series written in lowercase still canonicalizes (RC-3,
// fixed round 36a): the round-24 display-case axis was meant to separate
// prose words from catalog series, but a lowercase spelling of a real
// series — miaa is a series in the r18.dev content-id prefix lookup — is
// still a catalog release number, so the bypass discriminator is
// series-hood, not case. Both entry points canonicalize the compact,
// dotted, and spaced lowercase spellings exactly like the uppercase and
// hyphenated ones, while prose word-years stay excluded in every casing.
func TestLowercaseCatalogSeriesRemasterCanonicalizes(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id, marker string
		part             int
	}{
		{"miaa1234hd.mkv", "MIAA-1234H", "HD", 0},
		{"miaa.1234.hd.mkv", "MIAA-1234H", "HD", 0},
		{"miaa 1234 hd.mkv", "MIAA-1234H", "HD", 0},
		{"miaa1234h.mkv", "MIAA-1234H", "H", 0},
		{"Miaa1234HD.mkv", "MIAA-1234H", "HD", 0},
		{"miaa1234hd-pt2.mkv", "MIAA-1234H", "HD", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, tc.part, got.PartNumber)
			assert.Equal(t, "builtin", got.MatchedBy)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
	// Prose word-years stay excluded regardless of case: the words are
	// not catalog series, so the lowercase spelling keeps no tier at all,
	// and the display-case spelling joins BIRTHDAY2024HD's raw tier-2
	// word-year family instead of canonicalizing.
	for _, name := range []string{
		"birthday2024hd.mkv",
		"sample2024hd.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, matchOne(t, m, name))
			assert.Empty(t, m.MatchString(name))
		})
	}
	for _, tc := range []struct{ name, id string }{
		{"SAMPLE2024HD.mkv", "SAMPLE2024HD"},
		{"BIRTHDAY2024HD.mkv", "BIRTHDAY2024HD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Empty(t, got.RemasterMarker)
			assert.Equal(t, "contentid", got.MatchedBy)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
}

// A separated remaster id followed by a fused profile-bearing codec tag
// (DTSHD192, AACLC192 — the separator-free spellings of the round-33a
// family) must not hand its token to the catalog scan as a replacement
// id: the token never splits at a separator, so it arrives whole as an
// id-shaped candidate that satisfies the trailing catalog grammar and the
// builtin amateur pattern, and the quality vocabulary must decide it
// directly (round 36a, extending round 33a's separated compounds to the
// fused spellings). Both entry points agree on every head and marker.
func TestFusedCodecProfileTagVetoed(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{
		// The finding's spellings: the profile rides fused behind the
		// codec head with no separator before the digits.
		"ABC.123.HD DTSHD192.mkv",
		"ABC.123.HD AACLC192.mkv",
		// The probed profile variants behind the same codec heads.
		"ABC.123.HD DTSMA192.mkv",
		"ABC.123.HD DTSX192.mkv",
		"ABC.123.HD DTSHRA192.mkv",
		"ABC.123.HD DTSHDMA768.mkv",
		"ABC.123.HD AACHE192.mkv",
		"ABC.123.HD AACSBR192.mkv",
		"ABC.123.HD TRUEHDATMOS192.mkv",
		"ABC.123.HD EAC3ATMOS768.mkv",
		"ABC.123.HD DDPATMOS768.mkv",
		"ABC.123.HD AC3EX448.mkv",
		// The round-33a separated spellings stay vetoed beside the
		// fused ones.
		"ABC.123.HD DTS-HD192.mkv",
		"ABC.123.HD AAC-LC192.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
			assert.Equal(t, "ABC-123H", m.MatchString(name))
		})
	}
	// The AI marker keeps its spelling on both entry points.
	got := matchOne(t, m, "ABC.123.AI DTSHD192.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "ABC-123AI", got.ID)
	assert.Equal(t, "AI", got.RemasterMarker)
	assert.Equal(t, "ABC-123AI", m.MatchString("ABC.123.AI DTSHD192.mkv"))
}

// The fused codec-profile veto stays bounded: the digit bound keeps
// five-plus-digit id-shaped tokens and two-digit numerals on their
// existing paths, the near-miss heads keep the word-boundary protection,
// and the dd head carries no fused variant because its compounds collide
// with real series in the r18.dev content-id prefix lookup (ddex, ddma,
// ddxx) — their bare fused spellings keep id grammar while the separated
// DD-EX448 stays vetoed.
func TestFusedCodecProfileVetoBounds(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ name, id string }{
		// The digit bound: a five-plus-digit fused token stays outside
		// the compound (the r34b DVDRIP12345 precedent) and a
		// two-digit numeral already fails the amateur pattern's own
		// digit bound.
		{"ABC.123.HD DTSHD12345.mkv", "ABC-123H"},
		{"ABC.123.HD DTSHD24.mkv", "ABC-123H"},
		// Near-miss heads are boundary-protected: the leading letter
		// blocks the word boundary, so the fused token keeps id
		// grammar.
		{"ABC.123.HD XDTSHD192.mkv", "XDTSHD192"},
		// The dd head is excluded from the fused variant: ddex and
		// ddma are real series in the lookup, so their bare fused
		// spellings keep id grammar.
		{"ABC.123.HD DDEX192.mkv", "DDEX192"},
		{"ABC.123.HD DDMA192.mkv", "DDMA192"},
		// The separated dd compound stays vetoed (round 33a).
		{"ABC.123.HD DD-EX448.mkv", "ABC-123H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.id, m.MatchString(tc.name))
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
		})
	}
	// A standalone fused tag keeps the amateur tier's lenient id (the
	// WEB2160 precedent): the quality shadow only rejects a builtin
	// capture when a stronger raw id appears elsewhere in the name, so
	// the tag's job stays displacement suppression behind a remaster id.
	got := matchOne(t, m, "DTSHD192.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "DTSHD192", got.ID)
	assert.Equal(t, "DTSHD192", m.MatchString("DTSHD192.mkv"))
}
