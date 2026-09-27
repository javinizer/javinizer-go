package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A compact lowercase word/year/quality phrase (vacation2024hd) is rejected
// by the fused normalization's word-year guard, and the raw-cid surfaces —
// the marker-tail fallback in contentIDCandidate and the strong-token scan
// in rawTokenCandidateEnd — must not re-accept the same token as a content
// id: an ordinary video filename would otherwise ride the scrape and
// organization phases as a movie id. Both entry points agree, every marker
// spelling of the quality phrase is covered, and a title-case prose word is
// prose too (the round-23b guard already rejects its separated spelling).
func TestProseWordYearMarkerTailRejected(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{
		"vacation2024hd.mkv",
		"birthday2024hd.mkv",
		"sample2024hd.mkv",
		"vacation2024ai.mkv",
		"vacation2024h.mkv",
		"vacation2024ezh.mkv",
		"Vacation2024hd.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, matchOne(t, m, name))
			assert.Empty(t, m.MatchString(name))
		})
	}
	// A fused part label behind the phrase does not revive it: the trimmed
	// spelling is the same prose word-year, so the label path hands the
	// token to the explicit-marker debris check and no candidate backs it.
	for _, name := range []string{
		"vacation2024hdcd2.mkv",
		"vacation2024hdvol2.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, matchOne(t, m, name))
			assert.Empty(t, m.MatchString(name))
		})
	}
}

// A genuine raw id later in the name still corroborates the phrase, exactly
// like the marker-less weak form: the scan skips the word-year token and the
// later raw id wins on both entry points.
func TestProseWordYearMarkerTailYieldsToLaterRawID(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "birthday2024hd 1rct00156h.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "1RCT00156H", got.ID)
	assert.Equal(t, "1RCT00156H", m.MatchString("birthday2024hd 1rct00156h.mkv"))

	got = matchOne(t, m, "sample2024hd dv00899ai.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "DV00899AI", got.ID)
	assert.Equal(t, "DV00899AI", m.MatchString("sample2024hd dv00899ai.mkv"))
}

// The word-year rejection stays bounded to prose: zero padding is raw-cid
// evidence (the round-11 classifier), so a zero-padded word-year-shaped
// token keeps its raw id, and the genuine raw-cid tokens keep their paths.
// The display-case and catalog-series bounds keep their own pins in
// remaster_fused_test.go (BIRTHDAY2024HD, MIAA12345HD raw; MIAA1234HD
// canonicalized by the round-24 bypass before the raw tier ever runs).
func TestProseWordYearBoundsKeepRawIDs(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ name, id string }{
		{"mide00968h.mkv", "MIDE00968H"},
		{"1rct00156h.mkv", "1RCT00156H"},
		{"abc01234.mkv", "ABC01234"},
		{"abc01234h.mkv", "ABC01234H"},
		{"MIAA1234HD.mkv", "MIAA-1234H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
}
