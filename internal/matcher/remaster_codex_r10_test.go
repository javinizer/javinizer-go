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
	assert.True(t, builtinQualityShadowsContentID("PCM192 1rct00156h", "PCM192"))
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
		// Dotless standard color/transfer spellings — BT.601, Rec.601 and
		// Rec.2020 without the dot — are metadata too: the vocabulary's
		// (bt|rec|st|smpte)+digits class covers them along with future
		// standards (BT1886, REC2100, ST2086, SMPTE2086).
		{"ABC.123.HD BT601.mkv", "ABC-123H"},
		{"ABC.123.HD REC601.mkv", "ABC-123H"},
		{"ABC.123.HD REC2020.mkv", "ABC-123H"},
		{"ABC.123.HD BT1886.mkv", "ABC-123H"},
		{"ABC.123.HD REC2100.mkv", "ABC-123H"},
		{"ABC.123.HD ST2086.mkv", "ABC-123H"},
		{"ABC.123.HD SMPTE2086.mkv", "ABC-123H"},
		// Dotted spellings never match the trailing catalog grammar; they
		// stay metadata as well.
		{"ABC.123.HD BT.601.mkv", "ABC-123H"},
		{"ABC.123.HD REC.601.mkv", "ABC-123H"},
		{"ABC.123.HD REC.2020.mkv", "ABC-123H"},
		// The class is bounded: two-digit numbers (REC12) leave the
		// separated id alone, five-digit id-shaped tokens and hyphenated
		// spellings stay catalog-id grammar, a plain trailing id still
		// replaces the separated id, and a leading metadata token does not
		// shadow a strong raw id.
		{"ABC.123.HD REC12.mkv", "ABC-123H"},
		{"ABC.123.HD BT60123.mkv", "BT60123"},
		{"ABC.123.HD BT-601.mkv", "BT-601"},
		{"ABC.123.HD ABC987.mkv", "ABC987"},
		{"BT601 1rct00156h.mkv", "1RCT00156H"},
		// Lossless-audio sample rates are metadata too: the vocabulary's
		// (l)pcm+digits class covers the 3-4-digit sample rates (192, 384,
		// 768, 1411) that the builtin amateur pattern would otherwise
		// accept, along with dotted and spaced spellings.
		{"ABC.123.HD PCM192.mkv", "ABC-123H"},
		{"ABC.123.HD LPCM384.mkv", "ABC-123H"},
		{"ABC.123.HD PCM768.mkv", "ABC-123H"},
		{"ABC.123.HD LPCM1411.mkv", "ABC-123H"},
		{"ABC.123.HD PCM.192.mkv", "ABC-123H"},
		{"ABC.123.HD LPCM.384.mkv", "ABC-123H"},
		{"ABC.123.HD PCM 192.mkv", "ABC-123H"},
		// The audio class is bounded like the color class: two-digit
		// numbers (PCM12, PCM96) leave the separated id alone, five-digit
		// id-shaped tokens and hyphenated spellings stay catalog-id
		// grammar, a non-audio trailing token still replaces the separated
		// id, and a leading PCM token does not shadow a strong raw id.
		{"ABC.123.HD PCM12.mkv", "ABC-123H"},
		{"ABC.123.HD PCM96.mkv", "ABC-123H"},
		{"ABC.123.HD PCM00123.mkv", "PCM00123"},
		{"ABC.123.HD PCM-192.mkv", "PCM-192"},
		{"ABC.123.HD HIKARI345.mkv", "HIKARI345"},
		{"PCM192 1rct00156h.mkv", "1RCT00156H"},
		// Codec rate suffixes fold into the lossless-audio class: DTS768,
		// FLAC192, and OPUS192 sample-rate/bitrate spellings are metadata
		// too — the bare dts/flac/opus literals stop at the word boundary
		// before the digits, so without the class these tokens satisfy
		// the builtin amateur pattern as replacement catalog ids.
		{"ABC.123.HD DTS768.mkv", "ABC-123H"},
		{"ABC.123.HD FLAC192.mkv", "ABC-123H"},
		{"ABC.123.HD OPUS192.mkv", "ABC-123H"},
		{"ABC.123.HD OPUS128.mkv", "ABC-123H"},
		{"ABC.123.HD DTS1536.mkv", "ABC-123H"},
		{"ABC.123.HD DTS.768.mkv", "ABC-123H"},
		{"ABC.123.HD FLAC 192.mkv", "ABC-123H"},
		{"DTS768 1rct00156h.mkv", "1RCT00156H"},
		// The codec class is bounded like the audio class: two-digit
		// numerals (DTS24) leave the separated id alone, five-digit
		// id-shaped tokens stay catalog-id grammar, and hyphenated
		// spellings keep the bare literals' veto. The dts series is real
		// (its DMM content-id prefixes are pinned in the r18.dev lookup
		// table), so its canonical spellings keep matching as ids:
		// hyphenated display ids and numerically prefixed content ids.
		{"ABC.123.HD DTS24.mkv", "ABC-123H"},
		{"ABC.123.HD DTS00123.mkv", "DTS00123"},
		{"ABC.123.HD DTS-768.mkv", "ABC-123H"},
		{"DTS-24.mkv", "DTS-24"},
		{"DTS-768.mkv", "DTS-768"},
		{"189dts00087.mkv", "189DTS00087"},
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
		// The VVC codec-name spelling (H.266/VVC266) is metadata like its
		// avc/hevc siblings; the numeric h.26x spellings (H266, x266) are
		// already covered by the [hx]26 class.
		{"ABC.123.HD VVC266.mkv", "ABC-123H"},
		{"ABC.123.HD.VVC266.mkv", "ABC-123H"},
		{"ABC.123.HD VVC1080.mkv", "ABC-123H"},
		// No vvc series exists in the r18.dev content-id prefix lookup
		// (only lvvc, which the word boundary protects), so the free-digit
		// codec-name alias has no id-grammar exceptions to preserve.
		{"ABC.123.HD VVC24.mkv", "ABC-123H"},
		{"ABC.123.HD VVC00123.mkv", "ABC-123H"},
		{"VVC266 1rct00156h.mkv", "1RCT00156H"},
		{"189vvc00087.mkv", "189VVC00087"},
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

	// Dotless standard color/transfer metadata (BT.601, Rec.601, Rec.2020)
	// must not replace the separated id, and the marker stays intact.
	for _, name := range []string{"ABC.123.HD BT601.mkv", "ABC.123.HD REC601.mkv", "ABC.123.HD REC2020.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// Lossless-audio sample-rate metadata after a separated remaster id
	// must not replace it, and the marker stays intact.
	for _, name := range []string{"ABC.123.HD PCM192.mkv", "ABC.123.HD LPCM384.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// Codec rate-suffix metadata after a separated remaster id must not
	// replace it either, and the marker stays intact.
	for _, name := range []string{"ABC.123.HD DTS768.mkv", "ABC.123.HD FLAC192.mkv", "ABC.123.HD OPUS192.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// The VVC codec-name spelling after a separated remaster id must not
	// replace it either, and the marker stays intact.
	for _, name := range []string{"ABC.123.HD VVC266.mkv", "ABC.123.HD.VVC266.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// The class is bounded on the id-shaped side: a five-digit trailing
	// token is a plausible catalog id and still replaces the separated id.
	fileResult = matchOne(t, m, "ABC.123.HD BT60123.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "BT60123", fileResult.ID)
}
