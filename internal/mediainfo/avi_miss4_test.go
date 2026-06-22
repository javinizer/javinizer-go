package mediainfo

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- analyzeAVI: top-level strl LIST with audio stream (covers lines 165-167) ---

func TestMiss4_AnalyzeAVI_TopLevelStrlAudioStream(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := createTestAVI(t, tmpDir, "H264", 0x0055)

	f, err := os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, "mp3", info.AudioCodec, "audio codec from top-level strl")
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 44100, info.SampleRate)
}

// --- analyzeAVI: odd-size chunk with word alignment (covers lines 200-202) ---

func TestMiss4_AnalyzeAVI_OddSizeChunkAlignment(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := tmpDir + "/odd_chunk.avi"
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 0xFFFFFFFF)
	writeBytes(t, f, []byte("AVI "))

	// avih
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 40000)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 640)
	writeUint32LE(t, f, 480)
	for i := 0; i < 4; i++ {
		writeUint32LE(t, f, 0)
	}

	// Odd-size unknown chunk (triggers word alignment at line 200-202)
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 7)              // Odd size
	writeBytes(t, f, []byte("1234567")) // 7 bytes of data
	writeByte(t, f, 0)                  // padding byte for word alignment

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
}

// --- analyzeAVI: default LIST type (not hdrl or strl) is skipped ---

func TestMiss4_AnalyzeAVI_DefaultListType(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := tmpDir + "/default_list.avi"
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 0xFFFFFFFF)
	writeBytes(t, f, []byte("AVI "))

	// avih
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 40000)
	for i := 0; i < 13; i++ {
		if i == 4 {
			writeUint32LE(t, f, 100) // TotalFrames
		} else if i == 7 {
			writeUint32LE(t, f, 640) // Width
		} else if i == 8 {
			writeUint32LE(t, f, 480) // Height
		} else {
			writeUint32LE(t, f, 0)
		}
	}

	// LIST with unknown type (e.g., "movi") - covers line 178-180
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 12)
	writeBytes(t, f, []byte("movi"))
	writeBytes(t, f, []byte("00dc"))
	writeUint32LE(t, f, 0)

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 640, info.Width)
}

// --- parseStrlList: audio stream with WAVEFORMATEX data ---

func TestMiss4_ParseStrlList_AudioStreamWithWaveFormat(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := tmpDir + "/audio_wave.avi"
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// Build a standalone strl LIST chunk for an audio stream
	writeBytes(t, f, []byte("LIST"))
	strlSize := uint32(4 + 8 + 48 + 8 + 18)
	writeUint32LE(t, f, strlSize)
	writeBytes(t, f, []byte("strl"))

	// strh
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

	// strf (WAVEFORMATEX)
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 18)
	writeUint16LE(t, f, 0x0055) // MP3
	writeUint16LE(t, f, 2)      // channels
	writeUint32LE(t, f, 48000)  // sample rate
	writeUint32LE(t, f, 192000) // avg bytes/sec
	writeUint16LE(t, f, 4)      // block align
	writeUint16LE(t, f, 16)     // bits per sample

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Skip LIST header (8 bytes) + list type (4 bytes) = 12 bytes
	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, strlSize)
	require.NoError(t, err)
	assert.True(t, stream.isAudio)
	assert.False(t, stream.isVideo)
	assert.Equal(t, "mp3", stream.codec)
	assert.Equal(t, 2, stream.audioChannels)
	assert.Equal(t, 48000, stream.audioSampleRate)
	assert.Equal(t, 1536, stream.audioBitrate) // 192000 * 8 / 1000
}

// --- parseStrlList: unknown chunk inside strl (default skip path) ---

func TestMiss4_ParseStrlList_UnknownChunk(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := tmpDir + "/strl_unknown.avi"
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// Build a strl LIST with an unknown chunk
	writeBytes(t, f, []byte("LIST"))
	// 4 (strl type) + 8 (strh header) + 48 (strh data) + 8 (unknown header) + 10 (unknown data)
	strlSize := uint32(4 + 8 + 48 + 8 + 10)
	writeUint32LE(t, f, strlSize)
	writeBytes(t, f, []byte("strl"))

	// strh video
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("vids"))
	writeBytes(t, f, []byte("H264"))
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 30)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 640)
	writeUint16LE(t, f, 480)

	// Unknown chunk (e.g., "strd")
	writeBytes(t, f, []byte("strd"))
	writeUint32LE(t, f, 10)
	writeBytes(t, f, []byte("1234567890"))

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, strlSize)
	require.NoError(t, err)
	assert.True(t, stream.isVideo)
}

// --- parseStrlList: odd-size chunk alignment inside strl ---

func TestMiss4_ParseStrlList_OddSizeUnknownChunkAlignment(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := tmpDir + "/strl_odd.avi"
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// strl with odd-size unknown chunk
	writeBytes(t, f, []byte("LIST"))
	strlSize := uint32(4 + 8 + 48 + 8 + 5)
	writeUint32LE(t, f, strlSize)
	writeBytes(t, f, []byte("strl"))

	// strh video
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("vids"))
	writeBytes(t, f, []byte("H264"))
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 30)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 640)
	writeUint16LE(t, f, 480)

	// Odd-size unknown chunk
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 5) // Odd size
	writeBytes(t, f, []byte("12345"))
	// Need padding byte for word alignment
	writeByte(t, f, 0)

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, strlSize)
	require.NoError(t, err)
	assert.True(t, stream.isVideo)
}

// --- parseHdrlList: avih inside hdrl (covers lines 243-253) ---

func TestMiss4_ParseHdrlList_AvihInsideHdrl(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := tmpDir + "/hdrl_avih.avi"
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST hdrl with avih
	writeBytes(t, f, []byte("LIST"))
	hdrlSize := uint32(4 + 8 + 56)
	writeUint32LE(t, f, hdrlSize)
	writeBytes(t, f, []byte("hdrl"))

	// avih
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 33333) // MicroSecPerFrame
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100) // TotalFrames
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1280) // Width
	writeUint32LE(t, f, 720)  // Height
	for i := 0; i < 4; i++ {
		writeUint32LE(t, f, 0)
	}

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info := &VideoInfo{}
	_, _ = f.Seek(12, 0)

	videoFound := false
	audioFound := false
	err = parseHdrlList(f, info, 12, hdrlSize, &videoFound, &audioFound)
	require.NoError(t, err)
	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
	assert.InDelta(t, 30.0, info.FrameRate, 0.1)
}

// --- parseHdrlList: non-strl LIST inside hdrl is skipped ---

func TestMiss4_ParseHdrlList_NonStrlListInHdrl(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := tmpDir + "/hdrl_nonstrl.avi"
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST hdrl with a non-strl sub-LIST
	writeBytes(t, f, []byte("LIST"))
	hdrlSize := uint32(4 + 8 + 56 + 8 + 4 + 4) // avih + non-strl LIST
	writeUint32LE(t, f, hdrlSize)
	writeBytes(t, f, []byte("hdrl"))

	// avih
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 40000)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 50)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 640)
	writeUint32LE(t, f, 480)
	for i := 0; i < 4; i++ {
		writeUint32LE(t, f, 0)
	}

	// Non-strl LIST (e.g., "JUNK")
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 8)
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 0)

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info := &VideoInfo{}
	_, _ = f.Seek(12, 0)

	videoFound := false
	audioFound := false
	err = parseHdrlList(f, info, 12, hdrlSize, &videoFound, &audioFound)
	require.NoError(t, err)
	assert.Equal(t, 640, info.Width)
}

// --- mapAVIAudioCodec: additional format tags ---

func TestMiss4_MapAVIAudioCodec_AdditionalTags(t *testing.T) {
	assert.Equal(t, "adpcm", mapAVIAudioCodec(0x0002))
	assert.Equal(t, "pcm_float", mapAVIAudioCodec(0x0003))
	assert.Equal(t, "mp2", mapAVIAudioCodec(0x0050))
	assert.Equal(t, "wmav1", mapAVIAudioCodec(0x0161))
	assert.Equal(t, "wmav2", mapAVIAudioCodec(0x0162))
	assert.Equal(t, "wmav3", mapAVIAudioCodec(0x0163))
	assert.Equal(t, "ac3", mapAVIAudioCodec(0x2000))
	assert.Equal(t, "dts", mapAVIAudioCodec(0x2001))
	assert.Equal(t, "aac", mapAVIAudioCodec(0x00FF))
	assert.Equal(t, "aac", mapAVIAudioCodec(0xFFFE))
	assert.Equal(t, "vorbis", mapAVIAudioCodec(0x0674))
	assert.Equal(t, "opus", mapAVIAudioCodec(0x6750))
	assert.Equal(t, "flac", mapAVIAudioCodec(0xF1AC))
}

// --- mapAVIVideoCodec: additional FourCC codes ---

func TestMiss4_MapAVIVideoCodec_AdditionalCodes(t *testing.T) {
	assert.Equal(t, "h264", mapAVIVideoCodec("h264"))
	assert.Equal(t, "h264", mapAVIVideoCodec("X264"))
	assert.Equal(t, "h264", mapAVIVideoCodec("x264"))
	assert.Equal(t, "h264", mapAVIVideoCodec("AVC1"))
	assert.Equal(t, "h264", mapAVIVideoCodec("avc1"))
	assert.Equal(t, "h265", mapAVIVideoCodec("H265"))
	assert.Equal(t, "h265", mapAVIVideoCodec("HEVC"))
	assert.Equal(t, "h265", mapAVIVideoCodec("HVC1"))
	assert.Equal(t, "xvid", mapAVIVideoCodec("XVID"))
	assert.Equal(t, "divx", mapAVIVideoCodec("DIVX"))
	assert.Equal(t, "divx", mapAVIVideoCodec("DX50"))
	assert.Equal(t, "mpeg4", mapAVIVideoCodec("MP42"))
	assert.Equal(t, "mpeg4_v3", mapAVIVideoCodec("MP43"))
	assert.Equal(t, "wmv1", mapAVIVideoCodec("WMV1"))
	assert.Equal(t, "wmv2", mapAVIVideoCodec("WMV2"))
	assert.Equal(t, "wmv3", mapAVIVideoCodec("WMV3"))
	assert.Equal(t, "vp8", mapAVIVideoCodec("VP80"))
	assert.Equal(t, "vp9", mapAVIVideoCodec("VP90"))
	assert.Equal(t, "mjpeg", mapAVIVideoCodec("MJPG"))
	assert.Equal(t, "dv", mapAVIVideoCodec("dvsd"))
	assert.Equal(t, "ffv1", mapAVIVideoCodec("FFV1"))
	assert.Equal(t, "unknown", mapAVIVideoCodec(""))
	assert.Equal(t, "CUSTOM", mapAVIVideoCodec("CUSTOM"))
}

// --- analyzeAVI: complete file with hdrl + strl for full coverage ---

func TestMiss4_AnalyzeAVI_HdrlPlusTopLevelStrl(t *testing.T) {
	// Use the standard createTestAVI which is known to work
	tmpDir := t.TempDir()
	aviPath := createTestAVI(t, tmpDir, "H264", 0x0055)

	f, err := os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, "mp3", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 44100, info.SampleRate)
	assert.Greater(t, info.Duration, 0.0)
	assert.Greater(t, info.FrameRate, 0.0)
}

// --- parseStrlList: audio with odd-size strf chunk alignment ---

func TestMiss4_ParseStrlList_AudioOddSizeStrfAlignment(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := tmpDir + "/audio_odd_strf.avi"
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	strlSize := uint32(4 + 8 + 48 + 8 + 19) // strf with 19 bytes (odd)
	writeUint32LE(t, f, strlSize)
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
	writeUint32LE(t, f, 44100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 441000)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)

	// strf with odd size (19 bytes)
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 19) // Odd size
	// WAVEFORMATEX = 18 bytes + 1 padding byte
	var waveFormat [18]byte
	binary.LittleEndian.PutUint16(waveFormat[0:2], 0x0055)  // FormatTag = MP3
	binary.LittleEndian.PutUint16(waveFormat[2:4], 2)       // Channels
	binary.LittleEndian.PutUint32(waveFormat[4:8], 44100)   // SamplesPerSec
	binary.LittleEndian.PutUint32(waveFormat[8:12], 176400) // AvgBytesPerSec
	binary.LittleEndian.PutUint16(waveFormat[12:14], 4)     // BlockAlign
	binary.LittleEndian.PutUint16(waveFormat[14:16], 16)    // BitsPerSample
	writeBytes(t, f, waveFormat[:])
	// Extra padding byte for the odd size
	writeByte(t, f, 0)

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, strlSize)
	require.NoError(t, err)
	assert.True(t, stream.isAudio)
	assert.Equal(t, "mp3", stream.codec)
	assert.Equal(t, 2, stream.audioChannels)
	assert.Equal(t, 44100, stream.audioSampleRate)
}
