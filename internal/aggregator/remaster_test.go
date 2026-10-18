package aggregator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

// TestFoldRemasterDisplayID pins the display-ID fold to the matcher's
// canonical spelling (PR #257 structural fix 8 / contract decision 1):
// every marker-bearing display id a scraper returns — separated, compact,
// suffix-carrying, t28, AI, cid-shaped — canonicalizes to the same
// SERIES-NUMBER[SUFFIX][MARKER] spelling the matcher produces for the
// equivalent filename, so the aggregated movie shares the matched
// release's database/organized identity. Marker-free ids and
// marker-bearing strings the matcher itself rejects pass through
// unchanged.
//
// The compact-spelling pins changed DELIBERATELY with this decision: the
// fold previously folded only the marker (RCT156HD -> RCT156H, DV899AI ->
// DV899AI, RCT156H -> RCT156H), leaving the aggregated movie keyed apart
// from the matched RCT-156H / DV-899AI releases. Lowercase spellings now
// canonicalize to the matcher's uppercase output (rct-156-h -> RCT-156H)
// and cid-shaped marker-bearing spellings keep the matcher's tier-2 raw
// canonical (1rct00156hd -> 1RCT00156HD) instead of a locally folded
// spelling no pipeline tier produces.
func TestFoldRemasterDisplayID(t *testing.T) {
	cases := map[string]string{
		// Separated spellings fold marker and separator alike; JavDB
		// preserves the detail page's single-letter H spelling verbatim, so
		// the lone H folds like -HD.
		"RCT-156-HD": "RCT-156H",
		"RCT-156_HD": "RCT-156H",
		"RCT-156 HD": "RCT-156H",
		"RCT-156-H":  "RCT-156H",
		"RCT-156_H":  "RCT-156H",
		"RCT-156 H":  "RCT-156H",
		"rct-156-h":  "RCT-156H",
		"RCT-156H":   "RCT-156H", // already canonical — idempotent
		"DV-818-AI":  "DV-818AI",
		"DV-899-AI":  "DV-899AI",
		"DV.818.AI":  "DV-818AI",

		// Compact spellings fold the series-number separator too (the pin
		// change this decision approves): the matcher hyphenates them, so
		// the fold emits the hyphenated canonical.
		"RCT156H":    "RCT-156H",
		"RCT156HD":   "RCT-156H",
		"rct156hd":   "RCT-156H",
		"RCT156AI":   "RCT-156AI",
		"DV818AI":    "DV-818AI",
		"DV899AI":    "DV-899AI",
		"dv899ai":    "DV-899AI",
		"RCT.156.HD": "RCT-156H",
		"RCT_156_HD": "RCT-156H",
		"RCT 156 HD": "RCT-156H",
		"RCT_156H":   "RCT-156H",
		// Zero-padded numbers stay verbatim — the matcher's canonical keeps
		// the padding on display spellings.
		"RCT.00156.HD": "RCT-00156H",

		// The E/Z catalog suffix fuses to the number in the canonical
		// spelling, whatever separators surround it.
		"RCT-156-Z-H":  "RCT-156ZH",
		"RCT-156-ZH":   "RCT-156ZH",
		"RCT-156Z-HD":  "RCT-156ZH",
		"RCT-156-ZHD":  "RCT-156ZH",
		"RCT.156.Z.HD": "RCT-156ZH",
		"RCT_156_Z_HD": "RCT-156ZH",
		"RCT 156 Z HD": "RCT-156ZH",
		"RCT-156ZH":    "RCT-156ZH", // already canonical — idempotent
		"RCT156ZH":     "RCT-156ZH",
		"RCT-156-Z-AI": "RCT-156ZAI",

		// t28 convention: separator-pinned spellings stay T28-NUM, the
		// prefix-free compact three-digit tail reads as the T-series release
		// T-28xxx, and a four-digit tail rides the matcher's 6-digit T-series
		// grammar.
		"T28-123-HD": "T28-123H",
		"T28.123.HD": "T28-123H",
		"T28-123H":   "T28-123H", // already canonical — idempotent
		"T28123H":    "T-28123H",
		"t28123h":    "T-28123H",
		"T28123HD":   "T-28123H",
		"T28123AI":   "T-28123AI",
		"T2812H":     "T28-12H",
		"T281234H":   "T-281234H",
		"T-28123-HD": "T-28123H",
		"T-28123H":   "T-28123H", // already canonical — idempotent

		// Cid-shaped marker-bearing spellings keep the matcher's tier-2 raw
		// canonical (uppercase, marker unfused) — the spelling the file
		// pipeline produces for the same text.
		"1rct00156h":  "1RCT00156H",
		"1rct00156hd": "1RCT00156HD",
		"dv00899ai":   "DV00899AI",
		"abc01234h":   "ABC01234H",
		// The short-prefix family keeps its raw spelling.
		"AC3640H": "AC3640H",

		// Marker-bearing strings the matcher itself rejects keep their
		// verbatim spelling: the prose word-year matches no release, and a
		// quality phrase is not an id (the old fold mangled 1080P-HD into
		// 1080P-H).
		"vacation2024hd": "vacation2024hd",
		"1080P-HD":       "1080P-HD",
		"1080H":          "1080H",
		// Display-cased word-years keep the matcher's tier-2 raw canonical.
		"BIRTHDAY2024HD": "BIRTHDAY2024HD",

		// Stacked markers keep the FIRST marker, like the matcher.
		"RCT-156-HD-AI": "RCT-156H",
		"DV-818-AI-HD":  "DV-818AI",

		// Marker-free ids pass through unchanged.
		"RCT-156":    "RCT-156",
		"IPX-535":    "IPX-535",
		"IPX-00535Z": "IPX-00535Z",
		"MIDV123":    "MIDV123",
		"IPX535":     "IPX535",
		"ABCHD":      "ABCHD", // marker glued to a series word is ambiguous
		"HD-123":     "HD-123",
		"53dv899":    "53dv899",

		// Leading/trailing whitespace: folded when a marker tail is
		// recognized, verbatim otherwise.
		" RCT-156-HD ": "RCT-156H",
		" IPX-535 ":    " IPX-535 ",
	}
	for in, want := range cases {
		assert.Equal(t, want, foldRemasterDisplayID(in), in)
	}
}

// TestFoldRemasterDisplayID_MatcherCanonicalParity confirms the matcher's
// own canonicalization agrees with the fold for every marker-bearing
// spelling the fold recognizes: the fold output equals the matcher's
// canonical for the same string AND for the equivalent filename, so the
// aggregated movie and the matched release can never disagree. This is
// the RC-10 parity matrix (510/660 cells) reduced to the fold's input
// family — the delegation design keeps it green by construction, and the
// test guards the wiring against future regressions of the marker-tail
// gate or the canonicalizer.
func TestFoldRemasterDisplayID_MatcherCanonicalParity(t *testing.T) {
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err, "the default config cannot fail construction — the fold's shared canonicalizer relies on this")
	spellings := []string{
		"RCT-156-HD", "RCT-156_HD", "RCT-156 HD", "RCT-156-H", "rct-156-h", "RCT-156H",
		"RCT156H", "RCT156HD", "rct156hd", "RCT156AI", "DV818AI", "DV899AI", "dv899ai",
		"RCT.156.HD", "RCT_156_HD", "RCT 156 HD", "RCT_156H", "RCT.00156.HD",
		"DV-818-AI", "DV-899-AI", "DV.818.AI",
		"RCT-156-Z-H", "RCT-156-ZH", "RCT-156Z-HD", "RCT-156-ZHD", "RCT.156.Z.HD",
		"RCT_156_Z_HD", "RCT 156 Z HD", "RCT-156ZH", "RCT156ZH", "RCT-156-Z-AI",
		"T28-123-HD", "T28.123.HD", "T28-123H", "T28123H", "t28123h", "T28123HD",
		"T28123AI", "T2812H", "T281234H", "T-28123-HD", "T-28123H",
		"1rct00156h", "1rct00156hd", "dv00899ai", "abc01234h", "AC3640H",
		"BIRTHDAY2024HD", "RCT-156-HD-AI", "DV-818-AI-HD",
	}
	for _, spelling := range spellings {
		canonical := m.MatchString(spelling)
		require.NotEmpty(t, canonical, spelling)
		filename := spelling + ".mkv"
		file := m.MatchFile(models.FileMatchInfo{Path: "/v/" + filename, Name: filename, Extension: ".mkv"})
		require.NotNil(t, file, spelling)
		assert.Equal(t, canonical, file.ID, spelling+" — MatchString and MatchFile agree")
		assert.Equal(t, canonical, foldRemasterDisplayID(spelling), spelling)
	}
}

func TestAggregate_FoldsCompactRemasterDisplayID(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev", "dmm"}},
		Metadata: config.MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"r18dev", "dmm"}}},
	}
	a := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	require.NotNil(t, a)
	results := []*models.ScraperResult{
		{
			Source:    "r18dev",
			ID:        "RCT156HD",
			ContentID: "1rct00156h",
			Title:     "Remaster",
		},
	}
	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)
	// The compact display ID canonicalizes to the matcher's spelling
	// (pin updated deliberately per the contract decision: previously
	// RCT156H).
	assert.Equal(t, "RCT-156H", movie.ID)
	assert.Equal(t, "1rct00156h", movie.ContentID)
}

func TestAggregate_FoldsRemasterDisplayID(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev", "dmm"}},
		Metadata: config.MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"r18dev", "dmm"}}},
	}
	a := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	require.NotNil(t, a)
	// Every spelling family of the HD/AI remaster markers — the two-letter
	// -HD, the lone -H JavDB preserves verbatim from the detail page, the
	// compact marker-glued forms, and the E/Z-catalog-suffixed and t28
	// spellings — folds to the matcher's canonical identity.
	for _, tc := range []struct{ id, want string }{
		{"RCT-156-HD", "RCT-156H"},
		{"RCT-156-H", "RCT-156H"},
		{"RCT156HD", "RCT-156H"},
		{"RCT156H", "RCT-156H"},
		{"DV-899-AI", "DV-899AI"},
		{"DV899AI", "DV-899AI"},
		{"RCT-156-Z-H", "RCT-156ZH"},
		{"T28-123-HD", "T28-123H"},
		{"T28123H", "T-28123H"},
	} {
		results := []*models.ScraperResult{
			{
				Source:    "r18dev",
				ID:        tc.id,
				ContentID: "1rct00156h",
				Title:     "Remaster",
			},
		}
		movie, _, err := a.Aggregate(results)
		require.NoError(t, err, tc.id)
		require.NotNil(t, movie, tc.id)
		assert.Equal(t, tc.want, movie.ID, tc.id)
		assert.Equal(t, "1rct00156h", movie.ContentID, tc.id)
	}
}
