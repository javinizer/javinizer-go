package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Codex round-10: a resolution tag after the remaster marker is not a codec
// spelling, so the marker survives (IPX-535-H-720p); and a quality label
// before the real catalog id must be suppressed for the same catalog grammar
// the matcher accepts, with the id then extracted from the remainder.
func TestBuiltinQualityShadowsContentID(t *testing.T) {
	assert.True(t, builtinQualityShadowsContentID("x265 1rct00156h", "x265"))
	assert.False(t, builtinQualityShadowsContentID("x265", "x265"))
}

func TestRemasterMarkerResolutionAndQualityLabels(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id string
	}{
		// Resolution tags after the marker keep the marker.
		{"IPX-535-H-720p.mkv", "IPX-535H"},
		{"IPX-535-HD-480p.mkv", "IPX-535H"},
		{"IPX-535-H-1080p.mkv", "IPX-535H"},
		// Bare three-digit resolutions behave the same as the p-suffixed forms.
		{"IPX-535-H-720.mkv", "IPX-535H"},
		{"IPX-535-HD-480.mkv", "IPX-535H"},
		{"IPX-535-H-576.mkv", "IPX-535H"},
		{"IPX-535-HD-144.mkv", "IPX-535H"},
		{"IPX-535-HD-240.mkv", "IPX-535H"},
		{"IPX-535-HD-288.mkv", "IPX-535H"},
		{"IPX-535-HD-360.mkv", "IPX-535H"},
		{"IPX-535-HD-432.mkv", "IPX-535H"},
		{"IPX-535-HD-540.mkv", "IPX-535H"},
		// Numbered quality tags (HDR10, HEVC10) are tags, not catalog ids.
		{"ABC.123.HD HDR10.mkv", "ABC-123H"},
		{"ABC.123.HD HEVC10.mkv", "ABC-123H"},
		{"ABC.123.HD BT2020.mkv", "ABC-123H"},
		// Container/container-spelled quality tags (MP3, MP4) are tags too.
		{"ABC.123.HD MP3.mkv", "ABC-123H"},
		{"ABC.123.HD MP4.mkv", "ABC-123H"},
		// ProRes / pixel-format tags are metadata, not catalog ids.
		{"ABC.123.HD PRORES422.mkv", "ABC-123H"},
		{"ABC.123.HD YUV420.mkv", "ABC-123H"},
		{"ABC.123.HD RGB444.mkv", "ABC-123H"},
		{"ABC.123.HD P010.mkv", "ABC-123H"},
		// Numbered Dolby audio tags (DDP5.1, EAC3) are tags too.
		{"ABC.123.HD DDP5.1.mkv", "ABC-123H"},
		{"ABC.123.HD EAC3.mkv", "ABC-123H"},
		// DivX/Xvid numbered tags are tags too.
		{"ABC.123.HD DIVX5.mkv", "ABC-123H"},
		{"ABC.123.HD XVID4.mkv", "ABC-123H"},
		// MPEG-2 and VPx codec tags are tags too.
		{"ABC.123.HD MPEG2.mkv", "ABC-123H"},
		{"ABC.123.HD VP9.mkv", "ABC-123H"},
		// Codec tags still stay ambiguous.
		{"IPX-535-H.264.mkv", "IPX-535"},
		{"IPX-535-H264.mkv", "IPX-535"},
		// Leading quality labels suppress in favor of the trailing catalog id,
		// for every grammar the matcher accepts (separated, one-letter series,
		// long hyphenated series, separated T28, compact three-digit).
		{"FHD 1080 HD ABC.123.HD.mkv", "ABC-123H"},
		{"FHD 1080 HD A-123-HD.mkv", "A-123H"},
		{"FHD 1080 HD ABEAUTY-123-HD.mkv", "ABEAUTY-123H"},
		{"FHD 1080 HD T28.123.HD.mkv", "T28-123H"},
		{"FHD 1080 HD T28 123 HD.mkv", "T28-123H"},
		{"FHD 1080 HD IPX-480.mkv", "IPX-480"},
		{"QUALITY 1080 HD AB123.mkv", "AB123"},
		{"QUALITY 1080 HD ABC123.mkv", "ABC123"},
		{"QUALITY 1080 HD A123.mkv", "A123"},
		{"IPX535 10bit420.mkv", "IPX535"},
		{"IPX535 5point1.mkv", "IPX535"},
		{"ABC.123.HD BT709.mkv", "ABC-123H"},
		{"ABC.123.HD REC709.mkv", "ABC-123H"},
		{"ABC.123.HD SMPTE2084.mkv", "ABC-123H"},
		{"ABC.123.HD PQ2084.mkv", "ABC-123H"},
		{"ABC.123.HD HLG2020.mkv", "ABC-123H"},
		{"ABC.123.HD ST2084.mkv", "ABC-123H"},
		{"ABC.123.HD YCbCr420.mkv", "ABC-123H"},
		{"QUALITY 1080 HD ABCDEFGHI.123.HD.mkv", "ABCDEFGHI-123H"},
		{"birthday2024.mkv", ""},
		{"birthday2024hdr.mkv", ""},
		{"birthday2024raw.mkv", ""},
		{"documentary2024.mkv", ""},
		{"birthday.2024.HD.mkv", ""},
		{"Vacation 2024 HD.mp4", ""},
		{"documentary_2024_AI.mkv", ""},
		// Codec tags trailing the real id must not keep the leading quality
		// phrase alive or shadow the stronger raw content id.
		{"FHD 1080 HD ABC-123-HD x265.mkv", "ABC-123H"},
		{"FHD 1080 HD x265 ABC-123-HD.mkv", "ABC-123H"},
		{"QUALITY 1080 HD 1rct00156h x265.mkv", "1RCT00156H"},
		// The marker suffix comes after the ACTUAL match occurrence, not the
		// first textual occurrence of the id inside an ineligible token.
		{"prefixABC123 junk ABC123-HD.mkv", "ABC123H"},
		{"1080p IPX-535-H-720p.mkv", "IPX-535H"},
		// Ordinary hyphen-number tags after a real separated remaster never
		// suppress it, whatever the fallback matcher would pick instead.
		{"ABC.123.HD FHD-1080.mkv", "ABC-123H"},
		{"ABC.123.HD FHD-720.mkv", "ABC-123H"},
		{"ABC.123.HD scene-2.mkv", "ABC-123H"},
		{"ABC.123.HD AVC1.mkv", "ABC-123H"},
		{"ABC.123.HD AAC2.mkv", "ABC-123H"},
		{"ABC.123.HD VVC1.mkv", "ABC-123H"},
		{"ABC.123.HD AVS3.mkv", "ABC-123H"},
		// Plain forms are unchanged.
		{"ABC.123.HD.mkv", "ABC-123H"},
		{"RCT156H.mkv", "RCT-156H"},
		{"RCT156H-720p.mkv", "RCT-156H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}

	for _, name := range []string{"IPX-535-HD-144.mkv", "IPX-535-HD-240.mkv", "IPX-535-HD-288.mkv", "IPX-535-HD-360.mkv", "IPX-535-HD-432.mkv", "IPX-535-HD-540.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "IPX-535H", fileResult.ID)
	}

	fileResult := matchOne(t, m, "ABC.123.HD BT2020.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD RGB444.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD P010.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	assert.Nil(t, matchOne(t, m, "birthday2024.mkv"))
	assert.Nil(t, matchOne(t, m, "birthday2024hdr.mkv"))
	assert.Nil(t, matchOne(t, m, "birthday2024raw.mkv"))
	assert.Nil(t, matchOne(t, m, "documentary2024.mkv"))
	assert.Nil(t, matchOne(t, m, "birthday.2024.HD.mkv"))
	assert.Nil(t, matchOne(t, m, "Vacation 2024 HD.mp4"))
	assert.Nil(t, matchOne(t, m, "documentary_2024_AI.mkv"))

	fileResult = matchOne(t, m, "FHD 1080 HD IPX-480.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "IPX-480", fileResult.ID)

	fileResult = matchOne(t, m, "IPX535 10bit420.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "IPX535", fileResult.ID)

	fileResult = matchOne(t, m, "IPX535 5point1.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "IPX535", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD BT709.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD YCbCr420.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD REC709.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD SMPTE2084.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD PQ2084.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD HLG2020.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)

	fileResult = matchOne(t, m, "ABC.123.HD ST2084.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "ABC-123H", fileResult.ID)
}
