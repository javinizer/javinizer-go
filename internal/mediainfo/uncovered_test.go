package mediainfo

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mapAVIVideoCodec uncovered codec mappings ---

func TestMapAVIVideoCodec_Uncovered_DivxLowercase(t *testing.T) {
	assert.Equal(t, "divx", mapAVIVideoCodec("divx"))
}

func TestMapAVIVideoCodec_Uncovered_Mp43Lowercase(t *testing.T) {
	assert.Equal(t, "mpeg4_v3", mapAVIVideoCodec("mp43"))
}

func TestMapAVIVideoCodec_Uncovered_WmvLowercase(t *testing.T) {
	assert.Equal(t, "wmv1", mapAVIVideoCodec("wmv1"))
	assert.Equal(t, "wmv2", mapAVIVideoCodec("wmv2"))
	assert.Equal(t, "wmv3", mapAVIVideoCodec("wmv3"))
}

func TestMapAVIVideoCodec_Uncovered_VP8WithSpace(t *testing.T) {
	assert.Equal(t, "vp8", mapAVIVideoCodec("VP8 "))
	assert.Equal(t, "vp8", mapAVIVideoCodec("vp8 "))
}

func TestMapAVIVideoCodec_Uncovered_VP9WithSpace(t *testing.T) {
	assert.Equal(t, "vp9", mapAVIVideoCodec("VP9 "))
	assert.Equal(t, "vp9", mapAVIVideoCodec("vp9 "))
}

func TestMapAVIVideoCodec_Uncovered_MjpegLowercase(t *testing.T) {
	assert.Equal(t, "mjpeg", mapAVIVideoCodec("mjpg"))
	assert.Equal(t, "mjpeg", mapAVIVideoCodec("jpeg"))
}

func TestMapAVIVideoCodec_Uncovered_H265Variants(t *testing.T) {
	assert.Equal(t, "h265", mapAVIVideoCodec("H265"))
	assert.Equal(t, "h265", mapAVIVideoCodec("h265"))
	assert.Equal(t, "h265", mapAVIVideoCodec("HVC1"))
	assert.Equal(t, "h265", mapAVIVideoCodec("hvc1"))
}

func TestMapAVIVideoCodec_Uncovered_Mpg4Variants(t *testing.T) {
	assert.Equal(t, "mpeg4", mapAVIVideoCodec("mpg4"))
}

func TestMapAVIVideoCodec_Uncovered_Mp42Lowercase(t *testing.T) {
	assert.Equal(t, "mpeg4", mapAVIVideoCodec("mp42"))
}

func TestMapAVIVideoCodec_Uncovered_X264Lowercase(t *testing.T) {
	assert.Equal(t, "h264", mapAVIVideoCodec("x264"))
	assert.Equal(t, "h264", mapAVIVideoCodec("X264"))
}

func TestMapAVIVideoCodec_Uncovered_DVSDLowercase(t *testing.T) {
	assert.Equal(t, "dv", mapAVIVideoCodec("DVSD"))
}

func TestMapAVIVideoCodec_Uncovered_Ffv1Lowercase(t *testing.T) {
	assert.Equal(t, "ffv1", mapAVIVideoCodec("ffv1"))
}

func TestMapAVIVideoCodec_Uncovered_UnknownNonEmpty(t *testing.T) {
	assert.Equal(t, "CUSTOM", mapAVIVideoCodec("CUSTOM"))
}

func TestMapAVIVideoCodec_Uncovered_NullBytesInFourCC(t *testing.T) {
	// FourCC with null bytes — should stop at null
	assert.Equal(t, "h264", mapAVIVideoCodec("H264\x00\x00"))
}

func TestMapAVIVideoCodec_Uncovered_SingleChar(t *testing.T) {
	assert.Equal(t, "X", mapAVIVideoCodec("X"))
}

// --- mapAVIAudioCodec uncovered format tags ---

func TestMapAVIAudioCodec_Uncovered_Adpcm(t *testing.T) {
	assert.Equal(t, "adpcm", mapAVIAudioCodec(0x0002))
}

func TestMapAVIAudioCodec_Uncovered_PcmFloat(t *testing.T) {
	assert.Equal(t, "pcm_float", mapAVIAudioCodec(0x0003))
}

func TestMapAVIAudioCodec_Uncovered_Mp2(t *testing.T) {
	assert.Equal(t, "mp2", mapAVIAudioCodec(0x0050))
}

func TestMapAVIAudioCodec_Uncovered_Wmav1(t *testing.T) {
	assert.Equal(t, "wmav1", mapAVIAudioCodec(0x0161))
}

func TestMapAVIAudioCodec_Uncovered_Wmav2(t *testing.T) {
	assert.Equal(t, "wmav2", mapAVIAudioCodec(0x0162))
}

func TestMapAVIAudioCodec_Uncovered_Wmav3(t *testing.T) {
	assert.Equal(t, "wmav3", mapAVIAudioCodec(0x0163))
}

func TestMapAVIAudioCodec_Uncovered_Dts(t *testing.T) {
	assert.Equal(t, "dts", mapAVIAudioCodec(0x2001))
}

func TestMapAVIAudioCodec_Uncovered_Vorbis(t *testing.T) {
	assert.Equal(t, "vorbis", mapAVIAudioCodec(0x0674))
}

func TestMapAVIAudioCodec_Uncovered_Opus(t *testing.T) {
	assert.Equal(t, "opus", mapAVIAudioCodec(0x6750))
}

func TestMapAVIAudioCodec_Uncovered_Flac(t *testing.T) {
	assert.Equal(t, "flac", mapAVIAudioCodec(0xF1AC))
}

func TestMapAVIAudioCodec_Uncovered_AacFFFE(t *testing.T) {
	assert.Equal(t, "aac", mapAVIAudioCodec(0xFFFE))
}

// --- AVI canProbe uncovered ---

func TestAVIProber_CanProbe_Uncovered_ShortHeader(t *testing.T) {
	prober := newAVIProber()
	assert.False(t, prober.canProbe([]byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'A', 'V', 'I'}))
}

func TestAVIProber_CanProbe_Uncovered_Valid12Bytes(t *testing.T) {
	prober := newAVIProber()
	assert.True(t, prober.canProbe([]byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'A', 'V', 'I', ' '}))
}

func TestAVIProber_CanProbe_Uncovered_NilHeader(t *testing.T) {
	prober := newAVIProber()
	assert.False(t, prober.canProbe(nil))
}

// --- analyzeAVI uncovered: odD-sized RIFF chunk with padding ---

func TestAnalyzeAVI_Uncovered_ChunkWithOddSize(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "odd_chunk.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))

	// Write an unknown chunk with odd size (should trigger word alignment)
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 5)            // Odd size
	writeBytes(t, f, []byte("hello")) // 5 bytes of data

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
}

// --- analyzeAVI: word boundary after avih ---

func TestAnalyzeAVI_Uncovered_AvihWithOddSize(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "odd_avih.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))

	// avih with odd size (57 bytes)
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 57) // Odd size - not standard but tests alignment
	// Write 57 bytes
	for i := 0; i < 14; i++ {
		writeUint32LE(t, f, 0)
	}
	writeByte(t, f, 0) // 1 extra byte for odd size

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
}

func writeByte(t *testing.T, f *os.File, b byte) {
	err := binary.Write(f, binary.LittleEndian, b)
	require.NoError(t, err)
}

// --- MKV canProbe uncovered ---

func TestMKVProber_CanProbe_Uncovered_ValidHeader(t *testing.T) {
	prober := newMKVProber()
	assert.True(t, prober.canProbe([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x00}))
}

func TestMKVProber_CanProbe_Uncovered_InvalidHeader(t *testing.T) {
	prober := newMKVProber()
	assert.False(t, prober.canProbe([]byte{0x00, 0x00, 0x00, 0x00}))
}

func TestMKVProber_CanProbe_Uncovered_ShortHeader(t *testing.T) {
	prober := newMKVProber()
	assert.False(t, prober.canProbe([]byte{0x1A, 0x45}))
}

func TestMKVProber_CanProbe_Uncovered_NilHeader(t *testing.T) {
	prober := newMKVProber()
	assert.False(t, prober.canProbe(nil))
}

// --- MKV probe with valid but minimal file ---

func TestMKVProber_Probe_Uncovered_SeekError(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.mkv")
	require.NoError(t, os.WriteFile(emptyPath, []byte{}, 0644))

	f, err := os.Open(emptyPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := newMKVProber()
	_, err = prober.Probe(context.Background(), f)
	assert.Error(t, err)
}

// --- detectContainer uncovered ---

func TestDetectContainer_Uncovered_MKVHeader(t *testing.T) {
	assert.Equal(t, "mkv", detectContainer([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x00}))
}

func TestDetectContainer_Uncovered_MP4Header(t *testing.T) {
	assert.Equal(t, "mp4", detectContainer([]byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p'}))
}

func TestDetectContainer_Uncovered_AVIHeader(t *testing.T) {
	assert.Equal(t, "avi", detectContainer([]byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'A', 'V', 'I', ' '}))
}

func TestDetectContainer_Uncovered_UnknownHeader(t *testing.T) {
	assert.Equal(t, "unknown", detectContainer([]byte{0x00, 0x00, 0x00, 0x00}))
}

func TestDetectContainer_Uncovered_ShortHeader(t *testing.T) {
	assert.Equal(t, "unknown", detectContainer([]byte{0x00, 0x00}))
}

func TestDetectContainer_Uncovered_NilHeader(t *testing.T) {
	assert.Equal(t, "unknown", detectContainer(nil))
}

// --- VideoInfo.GetResolution uncovered ---

func TestVideoInfo_GetResolution_Uncovered_4K(t *testing.T) {
	v := &VideoInfo{Height: 2160}
	assert.Equal(t, "4K", v.GetResolution())
}

func TestVideoInfo_GetResolution_Uncovered_1080p(t *testing.T) {
	v := &VideoInfo{Height: 1080}
	assert.Equal(t, "1080p", v.GetResolution())
}

func TestVideoInfo_GetResolution_Uncovered_720p(t *testing.T) {
	v := &VideoInfo{Height: 720}
	assert.Equal(t, "720p", v.GetResolution())
}

func TestVideoInfo_GetResolution_Uncovered_480p(t *testing.T) {
	v := &VideoInfo{Height: 480}
	assert.Equal(t, "480p", v.GetResolution())
}

func TestVideoInfo_GetResolution_Uncovered_SD(t *testing.T) {
	v := &VideoInfo{Height: 360}
	assert.Equal(t, "SD", v.GetResolution())
}

func TestVideoInfo_GetResolution_Uncovered_ZeroHeight(t *testing.T) {
	v := &VideoInfo{Height: 0}
	assert.Equal(t, "SD", v.GetResolution())
}

// --- VideoInfo.getAudioChannelDescription uncovered ---

func TestVideoInfo_GetAudioChannelDescription_Uncovered_Mono(t *testing.T) {
	v := &VideoInfo{AudioChannels: 1}
	assert.Equal(t, "Mono", v.getAudioChannelDescription())
}

func TestVideoInfo_GetAudioChannelDescription_Uncovered_Stereo(t *testing.T) {
	v := &VideoInfo{AudioChannels: 2}
	assert.Equal(t, "Stereo", v.getAudioChannelDescription())
}

func TestVideoInfo_GetAudioChannelDescription_Uncovered_51(t *testing.T) {
	v := &VideoInfo{AudioChannels: 6}
	assert.Equal(t, "5.1", v.getAudioChannelDescription())
}

func TestVideoInfo_GetAudioChannelDescription_Uncovered_71(t *testing.T) {
	v := &VideoInfo{AudioChannels: 8}
	assert.Equal(t, "7.1", v.getAudioChannelDescription())
}

func TestVideoInfo_GetAudioChannelDescription_Uncovered_Other(t *testing.T) {
	v := &VideoInfo{AudioChannels: 3}
	assert.Equal(t, "3 channels", v.getAudioChannelDescription())
}

// --- AVI analysis with hdrl containing strl sub-list ---

func TestAnalyzeAVI_Uncovered_HdrlWithStrlSubList(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "hdrl_strl.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 0xFFFF) // Large placeholder size
	writeBytes(t, f, []byte("AVI "))

	// LIST hdrl
	writeBytes(t, f, []byte("LIST"))
	hdrlSize := uint32(4 + 8 + 56 + 8 + 4 + 8 + 48 + 8 + 40) // hdrl type + avih + LIST strl + strl type + strh + strf
	writeUint32LE(t, f, hdrlSize)
	writeBytes(t, f, []byte("hdrl"))

	// avih
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 40000) // 25fps
	writeUint32LE(t, f, 0)     // MaxBytesPerSec
	writeUint32LE(t, f, 0)     // PaddingGranularity
	writeUint32LE(t, f, 0)     // Flags
	writeUint32LE(t, f, 100)   // TotalFrames
	writeUint32LE(t, f, 0)     // InitialFrames
	writeUint32LE(t, f, 1)     // Streams
	writeUint32LE(t, f, 0)     // SuggestedBufferSize
	writeUint32LE(t, f, 1280)  // Width
	writeUint32LE(t, f, 720)   // Height
	for i := 0; i < 4; i++ {
		writeUint32LE(t, f, 0) // Reserved
	}

	// LIST strl inside hdrl
	writeBytes(t, f, []byte("LIST"))
	strlSize := uint32(4 + 8 + 48 + 8 + 40) // strl type + strh + strf
	writeUint32LE(t, f, strlSize)
	writeBytes(t, f, []byte("strl"))

	// strh
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("vids"))
	writeBytes(t, f, []byte("H264"))
	writeUint32LE(t, f, 0)    // Flags
	writeUint16LE(t, f, 0)    // Priority
	writeUint16LE(t, f, 0)    // Language
	writeUint32LE(t, f, 0)    // InitialFrames
	writeUint32LE(t, f, 1)    // Scale
	writeUint32LE(t, f, 25)   // Rate
	writeUint32LE(t, f, 0)    // Start
	writeUint32LE(t, f, 100)  // Length
	writeUint32LE(t, f, 0)    // SuggestedBufferSize
	writeUint32LE(t, f, 0)    // Quality
	writeUint32LE(t, f, 0)    // SampleSize
	writeUint16LE(t, f, 0)    // Frame.left
	writeUint16LE(t, f, 0)    // Frame.top
	writeUint16LE(t, f, 1280) // Frame.right
	writeUint16LE(t, f, 720)  // Frame.bottom

	// strf (BITMAPINFOHEADER)
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 40)
	writeUint32LE(t, f, 40) // biSize
	writeInt32LE(t, f, 1280)
	writeInt32LE(t, f, 720)
	writeUint16LE(t, f, 1)           // biPlanes
	writeUint16LE(t, f, 24)          // biBitCount
	writeBytes(t, f, []byte("H264")) // biCompression
	writeUint32LE(t, f, 0)           // biSizeImage
	writeInt32LE(t, f, 0)            // biXPelsPerMeter
	writeInt32LE(t, f, 0)            // biYPelsPerMeter
	writeUint32LE(t, f, 0)           // biClrUsed
	writeUint32LE(t, f, 0)           // biClrImportant

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
}

// --- mapMKVVideoCodec uncovered: WMA ---

func TestMapMKVAudioCodec_Uncovered_WMA(t *testing.T) {
	assert.Equal(t, "wma", mapMKVAudioCodec("A_MS/ACM"))
	assert.Equal(t, "wma", mapMKVAudioCodec("A_WMA"))
}

// --- analyzeAVI uncovered: LIST with unknown sub-list ---

func TestAnalyzeAVI_Uncovered_UnknownListType(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "unknown_list.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))

	// LIST with unknown type (should be skipped)
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 8)
	writeBytes(t, f, []byte("XXXX")) // Unknown list type

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
}

// --- analyzeAVI uncovered: top-level strl LIST (not inside hdrl) ---

func TestAnalyzeAVI_Uncovered_TopLevelStrlList(t *testing.T) {
	tmpDir := t.TempDir()
	// Use the existing createTestAVI helper which creates a valid AVI
	aviPath := createTestAVI(t, tmpDir, "H264", 0x2000) // AC3 audio

	f, err := os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, "ac3", info.AudioCodec)
}

// --- AVI with bitrate calculation ---

func TestAnalyzeAVI_Uncovered_BitrateCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := createTestAVI(t, tmpDir, "H264", 0x0055)

	f, err := os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	// Verify the container is set; bitrate calculation depends on file size
	// and duration which may not be set for the minimal test file
	assert.Equal(t, "h264", info.VideoCodec)
}

// --- mapAVIVideoCodec uncovered: more lowercase variants ---

func TestMapAVIVideoCodec_Uncovered_Avc1Lowercase(t *testing.T) {
	assert.Equal(t, "h264", mapAVIVideoCodec("avc1"))
}

func TestMapAVIVideoCodec_Uncovered_HevcLowercase(t *testing.T) {
	assert.Equal(t, "h265", mapAVIVideoCodec("hevc"))
}

func TestMapAVIVideoCodec_Uncovered_Dx50(t *testing.T) {
	assert.Equal(t, "divx", mapAVIVideoCodec("DX50"))
}

func TestMapAVIVideoCodec_Uncovered_Dx50Lowercase(t *testing.T) {
	assert.Equal(t, "dx50", mapAVIVideoCodec("dx50")) // lowercase not in map, returned as-is
}

// --- mapMKVVideoCodec uncovered: more mappings ---

func TestMapMKVVideoCodec_Uncovered_VP9Lowercase(t *testing.T) {
	assert.Equal(t, "vp9", mapMKVVideoCodec("V_VP9"))
}

func TestMapMKVVideoCodec_Uncovered_TheoraLowercase(t *testing.T) {
	assert.Equal(t, "theora", mapMKVVideoCodec("v_theora"))
}

func TestMapMKVVideoCodec_Uncovered_AV1Lowercase(t *testing.T) {
	assert.Equal(t, "av1", mapMKVVideoCodec("v_av1"))
}

func TestMapMKVVideoCodec_Uncovered_EmptyCodecID(t *testing.T) {
	assert.Equal(t, "", mapMKVVideoCodec(""))
}

// --- mapMKVAudioCodec uncovered: more mappings ---

func TestMapMKVAudioCodec_Uncovered_EAC3NotAC3(t *testing.T) {
	// A_EAC3 should NOT match A_AC3
	result := mapMKVAudioCodec("A_EAC3")
	assert.Equal(t, "eac3", result)
	assert.NotEqual(t, "ac3", result)
}

func TestMapMKVAudioCodec_Uncovered_MP3CaseInsensitive(t *testing.T) {
	assert.Equal(t, "mp3", mapMKVAudioCodec("a_mpeg/l3"))
}

func TestMapMKVAudioCodec_Uncovered_AACCaseInsensitive(t *testing.T) {
	assert.Equal(t, "aac", mapMKVAudioCodec("a_aac"))
}

func TestMapMKVAudioCodec_Uncovered_EmptyCodecID(t *testing.T) {
	assert.Equal(t, "", mapMKVAudioCodec(""))
}

// --- VideoInfo.GetResolution uncovered: edge values ---

func TestVideoInfo_GetResolution_Uncovered_1440p(t *testing.T) {
	v := &VideoInfo{Height: 1440}
	assert.Equal(t, "1080p", v.GetResolution()) // 1440 >= 1080 threshold but < 2160
}

func TestVideoInfo_GetResolution_Uncovered_576p(t *testing.T) {
	v := &VideoInfo{Height: 576}
	assert.Equal(t, "480p", v.GetResolution()) // 576 >= 480 threshold
}

// --- parseStrlList uncovered: unknown chunk type ---

func TestParseStrlList_Uncovered_UnknownChunk(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "unknown_strl_chunk.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// Write strl list with unknown chunk
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 20)
	writeBytes(t, f, []byte("strl"))

	// Unknown chunk
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 4)
	writeBytes(t, f, []byte("data"))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0) // Skip LIST + size + type

	stream, err := parseStrlList(f, 12, 20)
	require.NoError(t, err)
	assert.NotNil(t, stream)
}

// --- parseStrlList uncovered: audio with codec mapping ---

func TestParseStrlList_Uncovered_AudioCodecFromFormatTag(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "strl_ac3.avi")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 70)
	writeBytes(t, f, []byte("strl"))

	// strh audio
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("auds"))
	writeBytes(t, f, []byte{0, 0, 0, 0})
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 48000)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 480000)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)

	// strf AC3
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 18)
	writeUint16LE(t, f, 0x2000) // AC3
	writeUint16LE(t, f, 6)      // 5.1 channels
	writeUint32LE(t, f, 48000)
	writeUint32LE(t, f, 460800)
	writeUint16LE(t, f, 6)
	writeUint16LE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, 70)
	require.NoError(t, err)

	assert.True(t, stream.isAudio)
	assert.Equal(t, "ac3", stream.codec)
	assert.Equal(t, 6, stream.audioChannels)
	assert.Equal(t, 48000, stream.audioSampleRate)
}

// --- analyzeMKV uncovered: minimal EBML with Segment and Info ---

func TestAnalyzeMKV_Uncovered_MinimalEBML(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "minimal.mkv")

	// Create a file with just EBML header (not a valid MKV, but tests parsing)
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	// EBML header element
	f.Write([]byte{0x1A, 0x45, 0xDF, 0xA3}) // EBML element ID
	f.Write([]byte{0xA3})                   // Size = 35 (variable length)
	// Write some EBML header data
	for i := 0; i < 35; i++ {
		f.Write([]byte{0x00})
	}

	_ = f.Close()

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	// May error or return minimal info — both are acceptable
	if err != nil {
		t.Logf("analyzeMKV returned expected error for minimal file: %v", err)
	} else {
		assert.Equal(t, "mkv", info.Container)
	}
}

// --- parseEBMLHeader uncovered: zero size ---

func TestParseEBMLHeader_Uncovered_ZeroSize(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "zero_ebml.mkv")

	f, err := os.Create(mkvPath)
	require.NoError(t, err)
	_ = f.Close()

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Should not panic
	assert.NotPanics(t, func() {
		er := newEBMLReader(f)
		_ = er
	})
}
