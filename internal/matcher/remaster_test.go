package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func matchOne(t *testing.T, m *Matcher, name string) *MatchResult {
	t.Helper()
	return m.MatchFile(models.FileMatchInfo{Path: "/v/" + name, Name: name, Extension: ".mkv"})
}

func TestMatchFile_RemasterMarkers(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	cases := []struct {
		name       string
		wantID     string
		wantMarker string
	}{
		{"RCT-156H.mkv", "RCT-156H", "H"},
		{"rct-156h.mkv", "RCT-156H", "H"},
		{"RCT-156-HD.mkv", "RCT-156H", "HD"},
		{"RCT-156_HD.mkv", "RCT-156H", "HD"},
		{"RCT-156 HD.mkv", "RCT-156H", "HD"},
		{"RCT-156HD.mkv", "RCT-156H", "HD"},
		{"DV-818AI.mkv", "DV-818AI", "AI"},
		{"DV-818-AI.mkv", "DV-818AI", "AI"},
		{"IPX-535Z-HD.mkv", "IPX-535ZH", "HD"},
		{"IPX-535-Z-HD.mkv", "IPX-535ZH", "HD"},
		{"IPX.535.Z.HD.mkv", "IPX-535ZH", "HD"},
		{"IPX-535-H.mkv", "IPX-535H", "H"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.wantMarker, got.RemasterMarker)
			assert.Equal(t, 0, got.PartNumber)
			assert.Equal(t, "", got.MultipartPattern)
		})
	}
}

// The E/Z catalog suffix separated from the number resolves to the same
// canonical identity the scraper-side parsers (displayIdentityTuple /
// parseRemasterTail) read from the same spellings: the separated remaster
// grammar (the dot/underscore/space series-number separators) and the
// marker-tail remainder path (the hyphenated family the built-in tier
// owns) both accept the suffix with a separator on either side of it, so
// every separator family collapses to SERIES-NUMBER<suffix><marker>
// instead of the base id.
func TestMatchFile_SeparatedCatalogSuffixRemaster(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, tc := range []struct {
		name       string
		wantID     string
		wantMarker string
	}{
		// Hyphenated family: parsed by the marker-tail remainder path.
		{"IPX-535-Z-HD.mkv", "IPX-535ZH", "HD"},
		{"IPX-535-ZH.mkv", "IPX-535ZH", "H"},
		{"IPX-535-ZHD.mkv", "IPX-535ZH", "HD"},
		{"IPX-535-E-HD.mkv", "IPX-535EH", "HD"},
		{"IPX-535-Z-AI.mkv", "IPX-535ZAI", "AI"},
		{"ipx-535-z-hd.mkv", "IPX-535ZH", "HD"},
		// Dot/underscore/space families: canonicalized by the separated
		// remaster grammar's normalization.
		{"IPX.535.Z.HD.mkv", "IPX-535ZH", "HD"},
		{"IPX_535_Z_HD.mkv", "IPX-535ZH", "HD"},
		{"IPX 535 Z HD.mkv", "IPX-535ZH", "HD"},
		// Unseparated suffix spellings stay on their existing paths.
		{"IPX-535Z-HD.mkv", "IPX-535ZH", "HD"},
		{"IPX-535ZH.mkv", "IPX-535ZH", "H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.wantMarker, got.RemasterMarker)
			assert.Equal(t, 0, got.PartNumber)
			assert.Equal(t, "", got.MultipartPattern)
			assert.Equal(t, tc.wantID, m.MatchString(tc.name), "MatchString keeps parity with MatchFile")
		})
	}
}

// The separated catalog suffix keeps the marker-tail path's existing
// guardrails: a codec spelling after the marker still vetoes it (the base
// id survives), AI still outranks the codec veto, and part labels and fps
// shorthands after the marker still parse as parts and quality metadata
// respectively.
func TestMatchFile_SeparatedCatalogSuffixMarkerTailGuards(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, tc := range []struct {
		name       string
		wantID     string
		wantMarker string
		part       int
		pattern    string
	}{
		{"IPX-535-Z-H.264.mkv", "IPX-535", "", 0, ""},
		{"IPX-535-Z-HD.265.mkv", "IPX-535", "", 0, ""},
		{"IPX-535-Z-AI.264.mkv", "IPX-535ZAI", "AI", 0, ""},
		{"IPX-535-Z-HD-cd2.mkv", "IPX-535ZH", "HD", 2, PatternExplicit},
		{"IPX-535-Z-HD-60.mkv", "IPX-535ZH", "HD", 0, PatternNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.wantMarker, got.RemasterMarker)
			assert.Equal(t, tc.part, got.PartNumber)
			assert.Equal(t, tc.pattern, got.MultipartPattern)
		})
	}
}

func TestMatchFile_YearWordYieldsToStrongerRawID(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "birthday2024 1rct00156h.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "1RCT00156H", got.ID, "a word+year prefixless shape must not outrank a later numeric-prefixed raw id")

	got = matchOne(t, m, "sample2024 1rct00156h.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "1RCT00156H", got.ID, "MatchFile must choose the later raw ID over a year-shaped prefix")

	got = matchOne(t, m, "documentary2024 dv00899ai.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "DV00899AI", got.ID, "a word+year prefixless shape must not outrank a later marker-bearing raw id")

	got = matchOne(t, m, "birthday2024 1920x1080 1rct00156h.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "1RCT00156H", got.ID, "resolution-shaped tokens are skipped during the stronger-candidate scan")

	assert.Equal(t, "1RCT00156H", m.MatchString("sample2024 1rct00156h.mkv"), "MatchString must choose the later raw ID over a year-shaped prefix")
}

func TestMatchFile_QualityPrefixBeforeRawID(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "FHD 1080 HD 1rct00156h.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "1RCT00156H", got.ID, "a trailing raw content ID outranks the leading quality label")
}

func TestMatchFile_AIMarkerBeforeCodecTag(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, tc := range []struct{ name, wantID, wantMarker string }{
		{"DV-818-AI.264.mkv", "DV-818AI", "AI"},
		{"DV-818AI.265.mkv", "DV-818AI", "AI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.wantMarker, got.RemasterMarker)
		})
	}
}

func TestMatchFile_CodecTokensAreNotMarkers(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, name := range []string{"IPX-535-H.264.mkv", "IPX-535 H.265.mkv", "IPX-535-H.266.mkv", "IPX-535-HD.263.mkv", "IPX-535-H-264.mkv", "IPX-535-HD.265.mkv"} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "IPX-535", got.ID)
			assert.Empty(t, got.RemasterMarker)
			assert.Equal(t, "IPX-535", m.MatchString(name))
		})
	}
}

func TestMatchFile_AltRemasterNumberWidths(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	cases := []struct{ name, wantID, wantMarker string }{
		{"ABC1H.mkv", "ABC-1H", "H"},
		{"ABC.1.HD.mkv", "ABC-1H", "HD"},
		{"ABC.123456.HD.mkv", "ABC-123456H", "HD"},
		{"ABC123456H.mkv", "ABC-123456H", "H"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.wantMarker, got.RemasterMarker)
		})
	}
}

func TestMatchFile_ContentIDPartSuffixes(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "1rct00156h-pt2.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "1RCT00156H", got.ID)
	assert.Equal(t, 2, got.PartNumber)
	assert.Equal(t, PatternExplicit, got.MultipartPattern)

	got = matchOne(t, m, "53dv899-2.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "53DV899", got.ID)
	assert.Equal(t, 2, got.PartNumber)
	assert.Equal(t, PatternExplicit, got.MultipartPattern)

	assert.Equal(t, "1RCT00156H", m.MatchString("1rct00156h-pt2"))
}

func TestMatchFile_RemasterPartsPreserveRelease(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "RCT-156-HD-2.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT-156H", got.ID)
	assert.Equal(t, 2, got.PartNumber)
	assert.Equal(t, PatternExplicit, got.MultipartPattern)
	assert.Equal(t, "HD", got.RemasterMarker)

	got = matchOne(t, m, "RCT-156H-pt2.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT-156H", got.ID)
	assert.Equal(t, 2, got.PartNumber)
	assert.Equal(t, PatternExplicit, got.MultipartPattern)
	assert.Equal(t, "H", got.RemasterMarker)

	got = matchOne(t, m, "pt2-RCT-156-HD.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT-156H", got.ID)
	assert.Equal(t, "HD", got.RemasterMarker)
	assert.Equal(t, 0, got.PartNumber)
}

func TestMatchFile_UnknownCodecTagsAfterRemaster(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, name := range []string{"ABC.123.HD VVC1.mkv", "ABC.123.HD AVS3.mkv", "FHD 1080 HD x265 ABC-123-HD.mkv"} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
		})
	}
}

func TestMatchFile_QualityTagHDIsNotRemaster(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "RCT-156 HDrip 1080p.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT-156", got.ID)
	assert.Equal(t, "", got.RemasterMarker)
}

func TestMatchFile_ContentIDTier2(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	cases := []struct {
		name   string
		wantID string
	}{
		{"1rct00156h.mkv", "1RCT00156H"},
		{"53dv899.mkv", "53DV899"},
		{"dv00899ai.mkv", "DV00899AI"},
		{"ABC1234A.mkv", "ABC1234A"},
		{"[1rct00156h].mkv", "1RCT00156H"},
		{"(1rct00156h).mkv", "1RCT00156H"},
		{"[dv00899ai].mkv", "DV00899AI"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, 0, got.PartNumber)
			assert.Equal(t, "", got.RemasterMarker)
			assert.Equal(t, "contentid", got.MatchedBy)
		})
	}

	assert.Nil(t, matchOne(t, m, "oreco183a.mkv"))
}

func TestMatchFile_ContentIDTier2_WidenedPrefixes(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	cases := []struct {
		name   string
		wantID string
	}{
		{"118ipx00535.mkv", "118IPX00535"},
		{"lulu00441.mkv", "LULU00441"},
		{"5342abc00123h.mkv", "5342ABC00123H"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got, tc.name)
			assert.Equal(t, tc.wantID, got.ID)
		})
	}
}

func TestMatchFile_Tier2NeverPreemptsTier1(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "53dv899 IPX-535.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "IPX-535", got.ID)

	got = matchOne(t, m, "ABC1234-A.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "ABC1234", got.ID)
	assert.Equal(t, 1, got.PartNumber)
}

func TestMatchString_RemasterParity(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	assert.Equal(t, "RCT-156H", m.MatchString("RCT-156H"))
	assert.Equal(t, "RCT-156H", m.MatchString("RCT-156-HD"))
	assert.Equal(t, "DV-818AI", m.MatchString("DV-818AI"))
	assert.Equal(t, "1RCT00156H", m.MatchString("1rct00156h.mkv"))
	assert.Equal(t, "1RCT00156H", m.MatchString("1rct00156h"))
	assert.Equal(t, "RCT-156H", m.MatchString("RCT-156-HD-2"))
	assert.Equal(t, "", m.MatchString("oreco183a"))
}

func TestRemasterHelpersAndDemotionSkips(t *testing.T) {

	mk := func(name, id string, part int, pattern, marker string) MatchResult {
		return MatchResult{File: models.FileMatchInfo{Path: "/v/" + name}, ID: id, PartNumber: part, MultipartPattern: pattern, RemasterMarker: marker}
	}
	out := ValidateMultipartInDirectory([]MatchResult{
		mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, ""),
		mk("ABC-123B.mkv", "ABC-123", 2, PatternLetter, ""),
		mk("ABC-123H-pt1.mkv", "ABC-123H", 1, PatternExplicit, "H"),
		mk("ABC-123H-badspan.mkv", "ABC-123", 0, "", "H"),
	})
	assert.Equal(t, "ABC-123H", out[2].ID, "H marker with existing parts is never demoted")
	assert.Equal(t, 1, out[2].PartNumber)
	assert.Equal(t, "H", out[3].RemasterMarker, "defensive: marker without trailing-H ID is not demoted")
}

// A bare numeric right after a consumed remaster marker is quality metadata
// (fps shorthand), not a part number; labeled parts and small plain numbers
// stay parts.
func TestMatchFile_RemasterFPSLikeBareNumericIsQualityMetadata(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		id      string
		marker  string
		part    int
		pattern string
	}{
		{"IPX-535-HD-60.mkv", "IPX-535H", "HD", 0, PatternNone},
		{"IPX-535-H-30.mkv", "IPX-535H", "H", 0, PatternNone},
		{"RCT-156-HD-24.mkv", "RCT-156H", "HD", 0, PatternNone},
		{"RCT-156-HD.50.mkv", "RCT-156H", "HD", 0, PatternNone},
		// Small plain numbers stay parts (pinned semantics).
		{"IPX-535-HD-2.mkv", "IPX-535H", "HD", 2, PatternExplicit},
		{"IPX-535-HD-12.mkv", "IPX-535H", "HD", 12, PatternExplicit},
		// Labeled parts are unaffected.
		{"IPX-535-HD-cd2.mkv", "IPX-535H", "HD", 2, PatternExplicit},
		{"IPX-535-HD-pt2.mkv", "IPX-535H", "HD", 2, PatternExplicit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, tc.part, got.PartNumber)
			assert.Equal(t, tc.pattern, got.MultipartPattern)
			assert.Equal(t, tc.pattern == PatternExplicit, got.IsMultiPart)
		})
	}
	assert.Equal(t, "IPX-535H", m.MatchString("IPX-535-HD-60.mkv"))
}

// Stacked marker spellings keep the FIRST marker; the remainder is treated
// as metadata (no part, no second marker).
func TestMatchFile_StackedMarkersFirstWins(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, tc := range []struct{ name, id, marker string }{
		{"RCT-156-HD-AI.mkv", "RCT-156H", "HD"},
		{"RCT-156-hd-ai.mkv", "RCT-156H", "HD"},
		{"DV-818-AI-HD.mkv", "DV-818AI", "AI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, 0, got.PartNumber)
			assert.Equal(t, PatternNone, got.MultipartPattern)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
}

func TestValidateMultipart_ScopedDemotionRollback(t *testing.T) {
	mk := func(path, id string, part int, pattern, marker string, multi bool) MatchResult {
		return MatchResult{File: models.FileMatchInfo{Path: path}, ID: id, PartNumber: part, MultipartPattern: pattern, RemasterMarker: marker, IsMultiPart: multi}
	}
	in := []MatchResult{
		mk("/a/ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", false),
		mk("/a/ABC-123B.mkv", "ABC-123", 2, PatternLetter, "", false),
		mk("/a/ABC-123H.mkv", "ABC-123H", 0, "", "H", false),
		mk("/a/ABC-123-pt8.mkv", "ABC-123", 8, PatternExplicit, "", true),
		mk("/a/XYZ-123A.mkv", "XYZ-123", 1, PatternLetter, "", false),
		mk("/a/XYZ-123B.mkv", "XYZ-123", 2, PatternLetter, "", false),
		mk("/a/XYZ-123H.mkv", "XYZ-123H", 0, "", "H", false),
	}
	out := ValidateMultipartInDirectory(in)
	assert.Equal(t, "ABC-123H", out[2].ID, "failed group's H restores remaster")
	assert.Equal(t, "H", out[2].RemasterMarker)
	assert.Equal(t, "XYZ-123", out[6].ID, "healthy group's demotion survives")
	assert.Equal(t, 8, out[6].PartNumber)
	assert.True(t, out[6].IsMultiPart)
	assert.Equal(t, "", out[6].RemasterMarker)
	assert.True(t, out[4].IsMultiPart)
	assert.True(t, out[5].IsMultiPart)
	assert.True(t, out[0].IsMultiPart, "failed group's bare letters revalidate without H")
	assert.True(t, out[1].IsMultiPart)
	assert.True(t, out[3].IsMultiPart)
}

func TestValidateMultipart_RemasterDemotion(t *testing.T) {
	mk := func(name, id string, part int, pattern, trailing, marker string) MatchResult {
		return MatchResult{
			File:             models.FileMatchInfo{Path: "/v/" + name},
			ID:               id,
			PartNumber:       part,
			MultipartPattern: pattern,
			TrailingPrefix:   trailing,
			RemasterMarker:   marker,
		}
	}

	t.Run("A B H demotes to part 8", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", ""),
			mk("ABC-123B.mkv", "ABC-123", 2, PatternLetter, "", ""),
			mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H"),
		}
		out := ValidateMultipartInDirectory(in)
		require.Len(t, out, 3)
		assert.Equal(t, "ABC-123", out[2].ID)
		assert.Equal(t, 8, out[2].PartNumber)
		assert.True(t, out[2].IsMultiPart)
		assert.Equal(t, "", out[2].RemasterMarker)
		assert.True(t, out[0].IsMultiPart)
		assert.True(t, out[1].IsMultiPart)
	})

	t.Run("A H pair stays remaster", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", ""),
			mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H"),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[1].ID)
		assert.Equal(t, 0, out[1].PartNumber)
		assert.Equal(t, "H", out[1].RemasterMarker)
		assert.Equal(t, 0, out[0].PartNumber)
	})

	t.Run("collision with explicit pt8 rolls back and revalidates", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", ""),
			mk("ABC-123B.mkv", "ABC-123", 2, PatternLetter, "", ""),
			mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H"),
			func() MatchResult {
				r := mk("ABC-123-pt8.mkv", "ABC-123", 8, PatternExplicit, "", "")
				r.IsMultiPart = true
				return r
			}(),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[2].ID)
		assert.Equal(t, "H", out[2].RemasterMarker)
		assert.Equal(t, 0, out[2].PartNumber)
		assert.True(t, out[0].IsMultiPart)
		assert.True(t, out[1].IsMultiPart)
		assert.True(t, out[3].IsMultiPart)
	})

	t.Run("tagged siblings do not confirm", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123a-4k.mkv", "ABC-123", 1, PatternLetter, "-4k", ""),
			mk("ABC-123b-4k.mkv", "ABC-123", 2, PatternLetter, "-4k", ""),
			mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H"),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[2].ID)
		assert.Equal(t, "H", out[2].RemasterMarker)
		assert.True(t, out[0].IsMultiPart)
		assert.True(t, out[1].IsMultiPart)
	})

	t.Run("explicit HD never demotes", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", ""),
			mk("ABC-123B.mkv", "ABC-123", 2, PatternLetter, "", ""),
			mk("ABC-123-HD.mkv", "ABC-123H", 0, "", "", "HD"),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[2].ID)
		assert.Equal(t, "HD", out[2].RemasterMarker)
	})

	t.Run("different directory never confirms", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", ""),
			mk("ABC-123B.mkv", "ABC-123", 2, PatternLetter, "", ""),
			func() MatchResult {
				r := mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H")
				r.File.Path = "/other/ABC-123H.mkv"
				return r
			}(),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[2].ID)
		assert.Equal(t, "H", out[2].RemasterMarker)
	})

	t.Run("A B C H keeps remaster (three-part original)", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", ""),
			mk("ABC-123B.mkv", "ABC-123", 2, PatternLetter, "", ""),
			mk("ABC-123C.mkv", "ABC-123", 3, PatternLetter, "", ""),
			mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H"),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[3].ID, "three visible letters make part 8 unreachable; the remaster reading wins")
		assert.Equal(t, 0, out[3].PartNumber)
		assert.Equal(t, "H", out[3].RemasterMarker)
		assert.True(t, out[0].IsMultiPart)
		assert.True(t, out[1].IsMultiPart)
		assert.True(t, out[2].IsMultiPart)
	})

	t.Run("A B C D H keeps remaster", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", ""),
			mk("ABC-123B.mkv", "ABC-123", 2, PatternLetter, "", ""),
			mk("ABC-123C.mkv", "ABC-123", 3, PatternLetter, "", ""),
			mk("ABC-123D.mkv", "ABC-123", 4, PatternLetter, "", ""),
			mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H"),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[4].ID)
		assert.Equal(t, 0, out[4].PartNumber)
		assert.Equal(t, "H", out[4].RemasterMarker)
	})

	t.Run("B C H keeps remaster (no A)", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123B.mkv", "ABC-123", 2, PatternLetter, "", ""),
			mk("ABC-123C.mkv", "ABC-123", 3, PatternLetter, "", ""),
			mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H"),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[2].ID, "a run without A never demotes")
		assert.Equal(t, 0, out[2].PartNumber)
		assert.Equal(t, "H", out[2].RemasterMarker)
		assert.True(t, out[0].IsMultiPart)
		assert.True(t, out[1].IsMultiPart)
	})

	t.Run("A C H keeps remaster (gapped siblings)", func(t *testing.T) {
		in := []MatchResult{
			mk("ABC-123A.mkv", "ABC-123", 1, PatternLetter, "", ""),
			mk("ABC-123C.mkv", "ABC-123", 3, PatternLetter, "", ""),
			mk("ABC-123H.mkv", "ABC-123H", 0, "", "", "H"),
		}
		out := ValidateMultipartInDirectory(in)
		assert.Equal(t, "ABC-123H", out[2].ID, "a gapped letter set never demotes")
		assert.Equal(t, 0, out[2].PartNumber)
		assert.Equal(t, "H", out[2].RemasterMarker)
	})

	t.Run("end-to-end files keep the remaster spelling", func(t *testing.T) {
		m, err := NewMatcher(&Config{})
		require.NoError(t, err)
		results := m.Match([]models.FileMatchInfo{
			{Path: "/v/RCT-156-A.mkv", Name: "RCT-156-A.mkv", Extension: ".mkv"},
			{Path: "/v/RCT-156-B.mkv", Name: "RCT-156-B.mkv", Extension: ".mkv"},
			{Path: "/v/RCT-156-C.mkv", Name: "RCT-156-C.mkv", Extension: ".mkv"},
			{Path: "/v/RCT-156-H.mkv", Name: "RCT-156-H.mkv", Extension: ".mkv"},
		})
		require.Len(t, results, 4)
		validated := ValidateMultipartInDirectory(results)
		remaster := validated[3]
		assert.Equal(t, "RCT-156H", remaster.ID)
		assert.Equal(t, "H", remaster.RemasterMarker)
		assert.Equal(t, 0, remaster.PartNumber)
		assert.False(t, remaster.IsMultiPart)
	})
}
