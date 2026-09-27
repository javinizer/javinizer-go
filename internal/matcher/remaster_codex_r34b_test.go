package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A separated remaster id followed by a compact DVD-rip resolution tag
// must not hand its token to the catalog scan as a replacement id:
// DVDRIP480 satisfies both the trailing-catalog grammar (six letters plus
// three digits) and the builtin amateur pattern, so both entry points
// returned the tag instead of the remastered id. The round-21 rip family
// extends to the dvd head — the compound dvd+rip is a longer token than
// every real dvd-family head in the r18.dev content-id prefix lookup,
// exactly the BDRIP reasoning, while the bare dvd resolution spellings
// stay id grammar because the qualifier is required (round 34b, extending
// round 21's Blu-ray rip class).
func TestDVDRipResolutionSpellingVetoed(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{
		// The finding's spellings: the compact fused tag.
		"ABC.123.HD DVDRIP480.mkv",
		"ABC.123.HD DVDRIP1080.mkv",
		// The probed resolution variants behind the same dvd head.
		"ABC.123.HD DVDRIP720.mkv",
		"ABC.123.HD DVDRIP2160.mkv",
		"ABC.123.HD DVDRIP0720.mkv",
		"ABC.123.HD DVDrip480.mkv",
		// The hyphenated, dotted, and spaced siblings: the catalog scan
		// splits them at the separator, so the veto span extends back
		// over the dvd prefix (see compoundSourceTagPrefixRegex).
		"ABC.123.HD DVD-RIP480.mkv",
		"ABC.123.HD DVD.RIP480.mkv",
		"ABC.123.HD DVD RIP480.mkv",
		// The underscore sibling keeps its word-boundary protection (the
		// underscore is a word character, so the fragment never becomes a
		// candidate) and rides the class anyway.
		"ABC.123.HD DVD_RIP480.mkv",
		// The tag rides other separators against the remaster id too.
		"ABC.123.HD.DVDRIP480.mkv",
		"ABC 123 HD DVD-RIP2160.mkv",
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
	got := matchOne(t, m, "ABC.123.AI DVDRIP480.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "ABC-123AI", got.ID)
	assert.Equal(t, "AI", got.RemasterMarker)
	assert.Equal(t, "ABC-123AI", m.MatchString("ABC.123.AI DVDRIP480.mkv"))
}

// The DVD-rip veto stays bounded. The qualifier is required, so the bare
// dvd resolution spellings stay id grammar (the round-12 controls) and so
// do the real dvd-series ids — the numerically prefixed content ids and
// the hyphenated display ids of the near-miss heads never carry a rip
// fragment. The near-miss heads keep their leading-letter protection
// (dvdp and dvdes are real series, so DVDP-RIP1080 keeps its RIP1080
// fragment, the XBD precedent, and DVDROP480 never spells the qualifier),
// the digit bound keeps five-plus-digit fragments and two-digit numerals
// on their existing paths, a leading rip tag does not shadow a stronger
// raw id, and the round-21 Blu-ray rip pins stay green beside the new
// head.
func TestDVDRipVetoBounds(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id string
	}{
		// The qualifier is required: the bare dvd resolution spellings
		// stay id grammar, like the round-12 BD controls.
		{"ABC.123.HD DVD480.mkv", "DVD480"},
		{"ABC.123.HD DVD1080.mkv", "DVD1080"},
		{"DVD480.mkv", "DVD480"},
		{"DVD-480.mkv", "DVD-480"},
		{"DVD-123.mkv", "DVD-123"},
		// Real dvd-series ids keep their id grammar on both entry points.
		{"150dvd00123.mkv", "150DVD00123"},
		{"n_600dvd00123.mkv", "N_600DVD00123"},
		{"DVDB-123.mkv", "DVDB-123"},
		{"DVDES-617.mkv", "DVDES-617"},
		// Near-miss heads are boundary-protected.
		{"ABC.123.HD DVDP-RIP1080.mkv", "RIP1080"},
		{"ABC.123.HD XDVD-RIP1080.mkv", "RIP1080"},
		{"ABC.123.HD DVDROP480.mkv", "DVDROP480"},
		// The qualifier carries the class's digit bound: five-plus-digit
		// fragments stay catalog-id grammar and two-digit numerals
		// already fail the amateur pattern's own digit bound.
		{"ABC.123.HD DVDRIP12345.mkv", "ABC-123H"},
		{"ABC.123.HD DVD-RIP12345.mkv", "RIP12345"},
		{"ABC.123.HD DVDRIP24.mkv", "ABC-123H"},
		{"ABC.123.HD DVD-RIP24.mkv", "ABC-123H"},
		// A leading DVD-rip tag does not shadow a stronger raw id.
		{"DVDRIP1080 1rct00156h.mkv", "1RCT00156H"},
		{"DVD-RIP1080 1rct00156h.mkv", "1RCT00156H"},
		// The round-21 Blu-ray rip pins stay green beside the new head.
		{"ABC.123.HD BDRIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BRRIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BD-RIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BD1080.mkv", "BD1080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.id, m.MatchString(tc.name))
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
		})
	}
}
