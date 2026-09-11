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

func TestMatchFile_RemasterPartsBindToBase(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	got := matchOne(t, m, "RCT-156-HD-2.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT-156", got.ID)
	assert.Equal(t, 2, got.PartNumber)
	assert.Equal(t, PatternTrailing, got.MultipartPattern)
	assert.Equal(t, "", got.RemasterMarker)

	got = matchOne(t, m, "RCT-156H-pt2.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT-156", got.ID)
	assert.Equal(t, 2, got.PartNumber)
	assert.Equal(t, PatternExplicit, got.MultipartPattern)
	assert.Equal(t, "", got.RemasterMarker)

	got = matchOne(t, m, "pt2-RCT-156-HD.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT-156H", got.ID)
	assert.Equal(t, "HD", got.RemasterMarker)
	assert.Equal(t, 0, got.PartNumber)
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
	assert.Equal(t, "RCT-156", m.MatchString("RCT-156-HD-2"))
	assert.Equal(t, "", m.MatchString("oreco183a"))
}

func TestRemasterHelpersAndDemotionSkips(t *testing.T) {
	assert.Equal(t, "zzz", remainderAfterID("zzz", "abc"))
	assert.Equal(t, "-2", remainderAfterID("RCT-156-2", "RCT-156"))

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
}
