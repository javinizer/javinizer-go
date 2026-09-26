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
		// The Dolby Digital bitrate family rides the codec rate class
		// — e?ac3 plus 3-4 digits with an optional dot or space —
		// because the bare ac3/eac3 literals stop at the word boundary
		// before the digits: AC3640, AC3448, and EAC3640 bitrate
		// spellings otherwise satisfy the builtin amateur pattern and
		// the trailing catalog grammar as replacement ids behind a
		// separated remaster id, on both entry points. Four-digit
		// suffixes ride the same class (eac3's higher bitrates), so the
		// five-digit ac token AC36400 is covered too.
		{"ABC.123.HD AC3640.mkv", "ABC-123H"},
		{"ABC.123.HD AC3448.mkv", "ABC-123H"},
		{"ABC.123.HD EAC3640.mkv", "ABC-123H"},
		{"ABC.123.HD EAC3448.mkv", "ABC-123H"},
		{"ABC.123.HD EAC31536.mkv", "ABC-123H"},
		{"ABC.123.HD AC36400.mkv", "ABC-123H"},
		{"ABC.123.HD AC3.640.mkv", "ABC-123H"},
		{"ABC.123.HD AC3 640.mkv", "ABC-123H"},
		{"ABC.123.HD EAC3.640.mkv", "ABC-123H"},
		{"ABC.123.HD EAC3 640.mkv", "ABC-123H"},
		{"AC3640 1rct00156h.mkv", "1RCT00156H"},
		// The ac3 member keeps the class's bound, and since the codec
		// name itself ends in a digit, the id-shaped tokens it excludes
		// are the ac series' spellings with shorter remainders or
		// pinned anchors: no ac3 or eac3 series exists in the r18.dev
		// content-id prefix lookup, and the real ac series keeps its
		// spellings — two-digit remainders (AC307), hyphenated display
		// (AC-3640), numerically prefixed (306ac00123), zero-padded
		// (ac00364), standalone fused (AC3640), marker tails (AC3640H),
		// and leading-letter near misses (PEAC3640) all stay id grammar.
		{"ABC.123.HD AC307.mkv", "AC307"},
		{"ABC.123.HD AC364.mkv", "AC364"},
		{"AC-3640.mkv", "AC-3640"},
		{"306ac00123.mkv", "306AC00123"},
		{"ac00364.mkv", "AC00364"},
		{"AC3640.mkv", "AC3640"},
		{"AC3640H.mkv", "AC3640H"},
		{"PEAC3640.mkv", "PEAC3640"},
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
		// Compact source tags are metadata too: the vocabulary's
		// (?:web|remux|bluray)+digits class covers the compact
		// source/resolution spellings that the builtin amateur pattern and
		// the trailing catalog grammar would otherwise accept as replacement
		// catalog ids.
		{"ABC.123.HD WEB2160.mkv", "ABC-123H"},
		{"ABC.123.HD.WEB2160.mkv", "ABC-123H"},
		{"ABC.123.HD REMUX2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB1080.mkv", "ABC-123H"},
		{"ABC.123.HD WEB720.mkv", "ABC-123H"},
		{"ABC.123.HD REMUX1080.mkv", "ABC-123H"},
		{"ABC.123.HD REMUX720.mkv", "ABC-123H"},
		{"ABC.123.HD BLURAY1080.mkv", "ABC-123H"},
		{"ABC.123.HD BLURAY2160.mkv", "ABC-123H"},
		// The source class is bounded like the audio and codec classes:
		// two-digit numerals (WEB24) leave the separated id alone,
		// five-plus-digit id-shaped tokens (WEB12345) stay catalog-id
		// grammar, and zero-padded four-digit tokens (WEB0720) are compact
		// source-tag spellings — no web series exists to own zero-padded
		// raw ids — so they ride the class's 3-4 digit bound.
		{"ABC.123.HD WEB24.mkv", "ABC-123H"},
		{"ABC.123.HD WEB12345.mkv", "WEB12345"},
		{"ABC.123.HD WEB0720.mkv", "ABC-123H"},
		// No web or remux series exists in the r18.dev content-id prefix
		// lookup, so the id grammar keeps its protections: numerically
		// prefixed content ids keep the word boundary, the fweb series
		// keeps its leading letter, and a leading source tag does not
		// shadow a stronger raw id.
		{"WEB2160 1rct00156h.mkv", "1RCT00156H"},
		{"189web00087.mkv", "189WEB00087"},
		{"fweb0123.mkv", "FWEB0123"},
		// Compound source spellings are metadata too: the source class's
		// web+dl/rip qualifier covers the standard WEB-DL2160 spelling and
		// its compact, dotted, spaced, and hyphenated siblings — the
		// catalog scan splits them at the separator, so the DL/RIP
		// fragment would otherwise replace the separated id.
		{"ABC.123.HD WEB-DL2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB-DL1080.mkv", "ABC-123H"},
		{"ABC.123.HD WEB-DL720.mkv", "ABC-123H"},
		{"ABC.123.HD.WEB-DL2160.mkv", "ABC-123H"},
		{"ABC 123 HD WEB-DL2160.mkv", "ABC-123H"},
		{"ABC.123.AI WEB-DL2160.mkv", "ABC-123AI"},
		{"ABC.123.HD WEBDL2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEBRIP2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB-RIP2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB.RIP2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB RIP2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB.DL2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB DL2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB-DLRip2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB_DL2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB-DL 2160.mkv", "ABC-123H"},
		// The compound is bounded like the class: the qualifier must
		// directly precede the 3-4 digit resolution, so id-shaped
		// fragments, real dl-series display ids, and the resolution
		// window keep id grammar.
		{"ABC.123.HD WEB-DL24.mkv", "ABC-123H"},
		{"ABC.123.HD WEB-DL12345.mkv", "DL12345"},
		{"ABC.123.HD WEB-DL-24.mkv", "DL-24"},
		{"ABC.123.HD WEB-DL-2160.mkv", "DL-2160"},
		{"ABC.123.HD WEB-24.mkv", "WEB-24"},
		{"ABC.123.HD WEB-2160.mkv", "WEB-2160"},
		// No webdl, webrip, or rip series exists in the r18.dev content-id
		// prefix lookup; the real dl series keeps its bare DL2160
		// spellings, the web prefix's leading boundary keeps fweb and
		// numerically prefixed spellings in id grammar, and a leading
		// compound tag does not shadow a stronger raw id.
		{"ABC.123.HD DL2160.mkv", "DL2160"},
		{"ABC.123.HD RIP2160.mkv", "RIP2160"},
		{"ABC.123.HD FWEB-DL2160.mkv", "DL2160"},
		{"WEB-DL2160 1rct00156h.mkv", "1RCT00156H"},
		{"WEBDL2160 1rct00156h.mkv", "1RCT00156H"},
		{"WEBRIP2160 1rct00156h.mkv", "1RCT00156H"},

		// Blu-ray rip spellings are metadata too: the source class's
		// bd/br+rip qualifier covers the compact BDRIP1080 and BRRIP1080
		// spellings and their hyphenated, dotted, and spaced siblings —
		// the catalog scan splits the compound at its separator, so the
		// RIP fragment would otherwise replace the separated id. The
		// underscore sibling keeps its word-boundary protection (the
		// underscore is a word character, so the fragment never becomes a
		// candidate) and rides the class anyway.
		{"ABC.123.HD BDRIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BDRIP720.mkv", "ABC-123H"},
		{"ABC.123.HD BDRIP2160.mkv", "ABC-123H"},
		{"ABC.123.HD BDRIP0720.mkv", "ABC-123H"},
		{"ABC.123.HD BDrip1080.mkv", "ABC-123H"},
		{"ABC.123.HD BRRIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BD-RIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BD.RIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BD RIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BD_RIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BR-RIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BR.RIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD BR RIP1080.mkv", "ABC-123H"},
		{"ABC.123.HD.BDRIP1080.mkv", "ABC-123H"},
		{"ABC 123 HD BD-RIP2160.mkv", "ABC-123H"},
		{"ABC.123.AI BDRIP1080.mkv", "ABC-123AI"},
		// The qualifier is required where web's is optional: bd, bdr,
		// and br are real series in the r18.dev content-id prefix lookup,
		// so the round-12 decision keeps their bare resolution spellings
		// and display ids in id grammar, as do numerically prefixed content
		// ids and the series' hyphenated display ids.
		{"ABC.123.HD BD1080.mkv", "BD1080"},
		{"ABC.123.HD BDR1080.mkv", "BDR1080"},
		{"BD1080.mkv", "BD1080"},
		{"BD-1080.mkv", "BD-1080"},
		{"BDR-1080.mkv", "BDR-1080"},
		{"BR-616.mkv", "BR-616"},
		{"3bd00108.mkv", "3BD00108"},
		{"155bdr00108.mkv", "155BDR00108"},
		{"61br00666.mkv", "61BR00666"},
		{"BD12.mkv", ""},
		// The qualifier carries the class's digit bound: two-digit
		// numerals already fail the amateur pattern's own digit bound, and
		// five-plus-digit id-shaped fragments stay catalog-id grammar
		// like WEB-DL12345's DL12345 fragment.
		{"ABC.123.HD BDRIP24.mkv", "ABC-123H"},
		{"ABC.123.HD BD-RIP24.mkv", "ABC-123H"},
		{"ABC.123.HD BDRIP12345.mkv", "ABC-123H"},
		{"ABC.123.HD BD-RIP12345.mkv", "RIP12345"},
		// No bdrip, brrip, or rip series exists in the r18.dev content-id
		// prefix lookup; the compound prefix's leading boundary keeps
		// leading-letter spellings in id grammar (the fweb precedent), a
		// standalone RIP fragment keeps id grammar (the DL2160
		// precedent), and a leading rip tag does not shadow a stronger
		// raw id.
		{"ABC.123.HD RIP1080.mkv", "RIP1080"},
		{"ABC.123.HD XBD-RIP1080.mkv", "RIP1080"},
		{"BDRIP1080 1rct00156h.mkv", "1RCT00156H"},
		{"BD-RIP1080 1rct00156h.mkv", "1RCT00156H"},

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

	// Dolby Digital bitrate metadata after a separated remaster id must
	// not replace it either, and the marker stays intact — for the
	// compact AC3/EAC3 bitrate spellings and their dotted and spaced
	// siblings.
	for _, name := range []string{"ABC.123.HD AC3640.mkv", "ABC.123.HD AC3448.mkv", "ABC.123.HD EAC3640.mkv", "ABC.123.HD AC3.640.mkv", "ABC.123.HD AC3 640.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// The ac3 member's bound keeps the ac series' shorter id shapes: a
	// trailing AC307 still replaces the separated id, like any other
	// short-prefix catalog id.
	fileResult = matchOne(t, m, "ABC.123.HD AC307.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "AC307", fileResult.ID)

	// The VVC codec-name spelling after a separated remaster id must not
	// replace it either, and the marker stays intact.
	for _, name := range []string{"ABC.123.HD VVC266.mkv", "ABC.123.HD.VVC266.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// Compact source-tag metadata after a separated remaster id must not
	// replace it, and the marker stays intact.
	for _, name := range []string{"ABC.123.HD WEB2160.mkv", "ABC.123.HD REMUX2160.mkv", "ABC.123.HD BLURAY1080.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// Compound source-tag metadata after a separated remaster id must not
	// replace it either, and the marker stays intact — for the standard
	// WEB-DL spelling and its compact and separated siblings.
	for _, name := range []string{"ABC.123.HD WEB-DL2160.mkv", "ABC.123.HD WEBDL2160.mkv", "ABC.123.HD WEBRIP2160.mkv", "ABC.123.HD WEB-RIP2160.mkv", "ABC.123.HD WEB.DL2160.mkv", "ABC.123.HD WEB DL2160.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// Blu-ray rip metadata after a separated remaster id must not
	// replace it either, and the marker stays intact — for the compact
	// BDRIP and BRRIP spellings and their separated siblings.
	for _, name := range []string{"ABC.123.HD BDRIP1080.mkv", "ABC.123.HD BRRIP1080.mkv", "ABC.123.HD BD-RIP1080.mkv", "ABC.123.HD BD.RIP1080.mkv", "ABC.123.HD BD RIP1080.mkv", "ABC.123.HD BR-RIP1080.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.Equal(t, "ABC-123H", fileResult.ID)
		assert.Equal(t, "HD", fileResult.RemasterMarker)
	}

	// The round-12 controls keep their id grammar on both entry points:
	// the real bd series' bare resolution spelling and the br series'
	// hyphenated display id still match as ids.
	for _, name := range []string{"BD1080.mkv", "BD-1080.mkv", "BR-616.mkv"} {
		fileResult := matchOne(t, m, name)
		require.NotNil(t, fileResult)
		assert.NotEmpty(t, fileResult.ID)
	}

	// The class is bounded on the id-shaped side: a five-digit trailing
	// token is a plausible catalog id and still replaces the separated id.
	fileResult = matchOne(t, m, "ABC.123.HD BT60123.mkv")
	require.NotNil(t, fileResult)
	assert.Equal(t, "BT60123", fileResult.ID)
}

// Codex round-22: a fused volume suffix after the remaster marker is
// recognized like the cd/disc/disk/pt/part labels — vol rides the same
// marker-boundary label groups and part detection, so the compact
// spelling (RCT156HDvol2) normalizes instead of matching nothing and the
// hyphenated spelling (RCT-156HDvol2) keeps its display id instead of
// misclassifying the marker+label tail as the raw id 156HDVOL2.
func TestFusedRemasterVolumeSuffixes(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id, marker, matchedBy string
		part                        int
		partSuffix                  string
	}{
		{"RCT156HDvol2.mkv", "RCT-156H", "HD", "builtin", 2, "-vol2"},
		{"RCT-156HDvol2.mkv", "RCT-156H", "HD", "builtin", 2, "-vol2"},
		{"RCT156HDvol1.mkv", "RCT-156H", "HD", "builtin", 1, "-vol1"},
		{"RCT-156HD-vol2.mkv", "RCT-156H", "HD", "builtin", 2, "-vol2"},
		{"RCT156HD vol2.mkv", "RCT-156H", "HD", "builtin", 2, "-vol2"},
		{"RCT.156.HDvol2.mkv", "RCT-156H", "HD", "builtin", 2, "-vol2"},
		// The AI marker rides the same label groups.
		{"RCT156AIvol2.mkv", "RCT-156AI", "AI", "builtin", 2, "-vol2"},
		{"RCT-156AIvol2.mkv", "RCT-156AI", "AI", "builtin", 2, "-vol2"},
		// Tier-2 raw content ids keep their fused volume tails.
		{"1rct00156hvol2.mkv", "1RCT00156H", "", "contentid", 2, "-vol2"},
		{"1rct00156hdvol2.mkv", "1RCT00156HD", "", "contentid", 2, "-vol2"},
		// The marker+label debris veto is label-uniform: the cd/pt
		// siblings of the vol misclassification keep their display ids
		// too (they previously parsed as 156HDCD2/156HDPT2).
		{"RCT-156HDcd2.mkv", "RCT-156H", "HD", "builtin", 2, "-cd2"},
		{"RCT-156HDpt2.mkv", "RCT-156H", "HD", "builtin", 2, "-pt2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, tc.part, got.PartNumber)
			assert.Equal(t, tc.partSuffix, got.PartSuffix)
			assert.Equal(t, PatternExplicit, got.MultipartPattern)
			assert.True(t, got.IsMultiPart)
			assert.Equal(t, tc.matchedBy, got.MatchedBy)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
	// Controls: vol-less spellings keep their pinned behavior, the
	// full-token-strong path keeps ids like 118cd2, and a real series
	// whose spelling contains vol keeps its id grammar.
	for _, tc := range []struct{ name, id string }{
		{"RCT156HD.mkv", "RCT-156H"},
		{"RCT156HDcd2.mkv", "RCT-156H"},
		{"118cd2.mkv", "118CD2"},
		{"evol0123.mkv", "EVOL0123"},
	} {
		t.Run(tc.name+" control", func(t *testing.T) {
			assert.Equal(t, tc.id, m.MatchString(tc.name))
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
		})
	}
	single := matchOne(t, m, "RCT156HD.mkv")
	require.NotNil(t, single)
	assert.Equal(t, 0, single.PartNumber)
	assert.Empty(t, single.PartSuffix)
	assert.Nil(t, matchOne(t, m, "VOL2.mkv"))
}

// Codex round-22: numbered TrueHD sample-rate tags are quality metadata
// like the other numbered audio-codec tags — truehd joins the
// pcm/dts/flac/opus/ac3 rate class, so the compact TRUEHD192 spelling no
// longer satisfies the builtin amateur pattern and the trailing catalog
// grammar as a replacement id behind a separated remaster id.
func TestNumberedTrueHDTagIsQuality(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ name, id string }{
		{"ABC.123.HD TRUEHD192.mkv", "ABC-123H"},
		{"ABC.123.HD TRUEHD768.mkv", "ABC-123H"},
		{"ABC.123.HD TrueHD1411.mkv", "ABC-123H"},
		{"ABC.123.HD TRUEHD.768.mkv", "ABC-123H"},
		{"ABC.123.HD TRUEHD 768.mkv", "ABC-123H"},
		// The class keeps the rate members' 3-4 digit bound: two-digit
		// numerals stay catalog-id grammar and five-digit zero-padded
		// tokens keep the raw-id path, exactly like PCM12/PCM00123.
		{"ABC.123.HD TRUEHD24.mkv", "ABC-123H"},
		{"ABC.123.HD TRUEHD00123.mkv", "TRUEHD00123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.id, m.MatchString(tc.name))
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
		})
	}
	// The marker stays on the separated id, not on the tag.
	got := matchOne(t, m, "ABC.123.HD TRUEHD192.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "HD", got.RemasterMarker)
	// A leading numbered tag does not shadow a strong raw id (the
	// DTS768 1rct00156h precedent); a standalone tag keeps the builtin
	// tier's pinned fallback, exactly like standalone DTS768/PCM192.
	assert.Equal(t, "1RCT00156H", m.MatchString("TRUEHD192 1rct00156h.mkv"))
	assert.Equal(t, "TRUEHD192", m.MatchString("TRUEHD192.mkv"))
}
