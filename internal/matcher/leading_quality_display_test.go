package matcher

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestLeadingQualityYieldsToDisplay(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, prefix := range []string{"x265", "WEB2160", "WEB-DL2160", "DTS-HD192", "AAC-LC192", "FHD1080P", "PCM192", "x265 WEB2160 DTS-HD192", "x265 WEB-DL2160 AAC-LC192"} {
		for _, tc := range []struct{ tail, id, marker string }{
			{"ABC-123-HD", "ABC-123H", "HD"}, {"ABC-123-H", "ABC-123H", "H"},
			{"ABC-123-AI", "ABC-123AI", "AI"}, {"ABC-123-Z-HD", "ABC-123ZH", "HD"},
			{"ABC-123-E-AI", "ABC-123EAI", "AI"}, {"ABC-123", "ABC-123", ""},
			{"T28-123-HD", "T28-123H", "HD"}, {"T-28123-HD", "T-28123H", "HD"},
		} {
			t.Run(prefix+"/"+tc.tail, func(t *testing.T) {
				name := prefix + " " + tc.tail + ".mkv"
				require.Equal(t, tc.id, m.MatchString(name))
				for _, ext := range []string{".mkv", ""} {
					got := m.MatchFile(models.FileMatchInfo{Name: name, Extension: ext})
					require.NotNil(t, got)
					require.Equal(t, tc.id, got.ID)
					require.Equal(t, "builtin", got.MatchedBy)
					require.Equal(t, tc.marker, got.RemasterMarker)
				}
			})
		}
	}
}

func TestLeadingQualityKeepsPrecedence(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ name, id string }{
		{"x265.mkv", "X265"}, {"WEB2160.mkv", "WEB2160"}, {"x265 WEB2160.mkv", "X265"},
		{"x265 N1234 ABC-123-HD.mkv", "N1234"}, {"N1234 x265 ABC-123-HD.mkv", "N1234"},
		{"ABC-122 x265 ABC-123-HD.mkv", "ABC-122"}, {"x265 ABC-122 ABC-123-HD.mkv", "ABC-122"},
		{"ABC-123-HD x265.mkv", "ABC-123H"}, {"x265 ABC-123-HD WEB2160.mkv", "ABC-123H"},
		{"x265 DTS-24 ABC-123-HD.mkv", "DTS-24"}, {"x265 AVC-123 ABC-123-HD.mkv", "AVC-123"},
		{"189web00087 x265 ABC-123-HD.mkv", "189WEB00087"},
		{"x265 1rct00156hd ABC-123-HD.mkv", "1RCT00156HD"},
		// The raw fallback retains the selected CID's supported channel spelling.
		{"x265 h_003rct00156hd ABC-123-HD.mkv", "H_003RCT00156HD"},
		{"h_003rct00156hd x265 ABC-123-HD.mkv", "H_003RCT00156HD"},
		{"x265 189web00087 ABC-123-HD.mkv", "189WEB00087"},
		{"x265 189dts00192 ABC-123-HD.mkv", "189DTS00192"},
		{"x265 1x265hd ABC-123-HD.mkv", "1X265HD"},
		{"WEB-DL2160.mkv", "DL2160"}, {"DTS-HD192.mkv", "HD192"},
		{"x265 ＲＣＴ-156-ＨＤ.mkv", "X265"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.id, m.MatchString(tc.name))
			got := m.MatchFile(models.FileMatchInfo{Name: tc.name, Extension: ".mkv"})
			require.NotNil(t, got)
			require.Equal(t, tc.id, got.ID)
		})
	}
	for _, tc := range []struct{ pattern, name, id string }{
		{`^(x265) ABC-123-HD\.mkv$`, "x265 ABC-123-HD.mkv", "X265"},
		{`^(WEB2160) ABC-123-HD$`, "WEB2160 ABC-123-HD.mkv", "WEB2160"},
		{`(rct\d+)`, "x265 1rct00156hd ABC-123-HD.mkv", "RCT00156"},
	} {
		custom, err := NewMatcher(&Config{RegexEnabled: true, RegexPattern: tc.pattern})
		require.NoError(t, err)
		require.Equal(t, tc.id, custom.MatchString(tc.name))
		// Empty Extension preserves the raw argument for the first custom attempt.
		got := custom.MatchFile(models.FileMatchInfo{Name: tc.name})
		require.NotNil(t, got)
		require.Equal(t, tc.id, got.ID)
		require.Equal(t, "regex", got.MatchedBy)
	}
}

func TestLeadingQualityKeepsSelectedMultipartTail(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id, marker, suffix string
		part                     int
	}{
		{"x265 ABC-123-HD-cd2.mkv", "ABC-123H", "HD", "-cd2", 2},
		{"WEB-DL2160 ABC-123-AI-part3.mkv", "ABC-123AI", "AI", "-part3", 3},
		{"DTS-HD192 ABC-123-HDvol2.mkv", "ABC-123H", "HD", "-vol2", 2},
		{"x265 ABC-122-cd1 ABC-123-HD-cd2.mkv", "ABC-122", "", "-cd1", 1},
		{"x265 ABC-123-HD-60.mkv", "ABC-123H", "HD", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.id, m.MatchString(tc.name))
			got := m.MatchFile(models.FileMatchInfo{Name: tc.name, Extension: ".mkv"})
			require.NotNil(t, got)
			require.Equal(t, tc.id, got.ID)
			require.Equal(t, tc.marker, got.RemasterMarker)
			require.Equal(t, tc.part, got.PartNumber)
			require.Equal(t, tc.suffix, got.PartSuffix)
			require.Equal(t, tc.part > 0, got.IsMultiPart)
		})
	}
}
