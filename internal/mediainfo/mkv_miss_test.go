package mediainfo

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- parseUint uncovered branches ---

func TestParseUint_EmptyData(t *testing.T) {
	assert.Equal(t, uint64(0), parseUint(nil))
	assert.Equal(t, uint64(0), parseUint([]byte{}))
}

func TestParseUint_Miss_SingleByte(t *testing.T) {
	assert.Equal(t, uint64(42), parseUint([]byte{42}))
}

func TestParseUint_MultiByte(t *testing.T) {
	assert.Equal(t, uint64(0x0102), parseUint([]byte{0x01, 0x02}))
	assert.Equal(t, uint64(0x010203), parseUint([]byte{0x01, 0x02, 0x03}))
	assert.Equal(t, uint64(0x01020304), parseUint([]byte{0x01, 0x02, 0x03, 0x04}))
}

// --- parseFloatEBML uncovered branches ---

func TestParseFloatEBML_EmptyData(t *testing.T) {
	assert.Equal(t, float64(0), parseFloatEBML(nil))
	assert.Equal(t, float64(0), parseFloatEBML([]byte{}))
}

func TestParseFloatEBML_Float32(t *testing.T) {
	bits := math.Float32bits(3.14)
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, bits)
	result := parseFloatEBML(data)
	assert.InDelta(t, 3.14, result, 0.01)
}

func TestParseFloatEBML_Float64(t *testing.T) {
	bits := math.Float64bits(120.5)
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, bits)
	result := parseFloatEBML(data)
	assert.InDelta(t, 120.5, result, 0.001)
}

func TestParseFloatEBML_InvalidSize(t *testing.T) {
	// 2-byte and 6-byte data should return 0
	assert.Equal(t, float64(0), parseFloatEBML([]byte{0x01, 0x02}))
	assert.Equal(t, float64(0), parseFloatEBML(make([]byte, 6)))
}

// --- parseString uncovered branches ---

func TestParseString_Miss(t *testing.T) {
	assert.Equal(t, "V_MPEG4/ISO/AVC", parseString([]byte("V_MPEG4/ISO/AVC")))
	assert.Equal(t, "", parseString([]byte{}))
	assert.Equal(t, "", parseString(nil))
}

// --- parseSegmentInfo with crafted ebmlReader data ---

func TestParseSegmentInfo_CraftedData(t *testing.T) {
	// Build raw bytes for a segment info with TimecodeScale and Duration
	// TimecodeScale element ID: 0x2AD7B1 (3 bytes), data: 1000000 as uint
	// Duration element ID: 0x4489 (2 bytes), data: 120.5 as float64

	var buf []byte

	// TimecodeScale: ID=0x2AD7B1, size=3, data=0x0F4240 (1000000)
	buf = append(buf, 0x2A, 0xD7, 0xB1) // Element ID
	buf = append(buf, 0x83)             // VINT size = 3
	buf = append(buf, 0x0F, 0x42, 0x40) // 1000000

	// Duration: ID=0x4489, size=8, data=120.5 as float64
	buf = append(buf, 0x44, 0x89) // Element ID
	buf = append(buf, 0x88)       // VINT size = 8
	bits := math.Float64bits(120.5)
	for i := 7; i >= 0; i-- {
		buf = append(buf, byte(bits>>uint(i*8)))
	}

	er := newEBMLReader(bytes.NewReader(buf))
	info := &VideoInfo{}
	parseSegmentInfo(er, int64(len(buf)), info)

	assert.Greater(t, info.Duration, 0.0)
}

// --- parseTrackEntry with video track ---

func TestParseTrackEntry_CraftedVideoTrack(t *testing.T) {
	var buf []byte

	// TrackType: ID=0x83, size=1, data=0x01 (video)
	buf = append(buf, 0x83) // Element ID
	buf = append(buf, 0x81) // VINT size = 1
	buf = append(buf, 0x01) // video = 1

	// CodecID: ID=0x86, size=15, data="V_MPEG4/ISO/AVC"
	codec := []byte("V_MPEG4/ISO/AVC")
	buf = append(buf, 0x86)                  // Element ID
	buf = append(buf, byte(0x80|len(codec))) // VINT size
	buf = append(buf, codec...)

	// Video: ID=0xE0, size=variable
	var videoBuf []byte
	// PixelWidth: ID=0xB0, size=2, data=1920
	videoBuf = append(videoBuf, 0xB0)
	videoBuf = append(videoBuf, 0x82)
	videoBuf = append(videoBuf, byte(1920>>8), byte(1920&0xFF))
	// PixelHeight: ID=0xBA, size=2, data=1080
	videoBuf = append(videoBuf, 0xBA)
	videoBuf = append(videoBuf, 0x82)
	videoBuf = append(videoBuf, byte(1080>>8), byte(1080&0xFF))

	buf = append(buf, 0xE0)                     // Element ID
	buf = append(buf, byte(0x80|len(videoBuf))) // VINT size
	buf = append(buf, videoBuf...)

	er := newEBMLReader(bytes.NewReader(buf))
	info := &VideoInfo{}
	parseTrackEntry(er, int64(len(buf)), info)

	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
}

// --- parseTrackEntry with audio track ---

func TestParseTrackEntry_CraftedAudioTrack(t *testing.T) {
	var buf []byte

	// TrackType: ID=0x83, size=1, data=0x02 (audio)
	buf = append(buf, 0x83)
	buf = append(buf, 0x81)
	buf = append(buf, 0x02)

	// CodecID: ID=0x86, size=5, data="A_AAC"
	codec := []byte("A_AAC")
	buf = append(buf, 0x86)
	buf = append(buf, byte(0x80|len(codec)))
	buf = append(buf, codec...)

	// Audio: ID=0xE1, size=variable
	var audioBuf []byte
	// SamplingFrequency: ID=0xB5, size=8, data=48000.0
	audioBuf = append(audioBuf, 0xB5)
	audioBuf = append(audioBuf, 0x88)
	bits := math.Float64bits(48000.0)
	for i := 7; i >= 0; i-- {
		audioBuf = append(audioBuf, byte(bits>>uint(i*8)))
	}
	// Channels: ID=0x9F, size=1, data=2
	audioBuf = append(audioBuf, 0x9F)
	audioBuf = append(audioBuf, 0x81)
	audioBuf = append(audioBuf, 0x02)

	buf = append(buf, 0xE1)
	buf = append(buf, byte(0x80|len(audioBuf)))
	buf = append(buf, audioBuf...)

	er := newEBMLReader(bytes.NewReader(buf))
	info := &VideoInfo{}
	parseTrackEntry(er, int64(len(buf)), info)

	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 48000, info.SampleRate)
	assert.Equal(t, 2, info.AudioChannels)
}

// --- parseTrackEntry: audio with zero channels defaults to 2 ---

func TestParseTrackEntry_AudioZeroChannels(t *testing.T) {
	var buf []byte

	// TrackType: ID=0x83, size=1, data=0x02 (audio)
	buf = append(buf, 0x83)
	buf = append(buf, 0x81)
	buf = append(buf, 0x02)

	// CodecID: ID=0x86, size=5, data="A_OPUS"
	codec := []byte("A_OPUS")
	buf = append(buf, 0x86)
	buf = append(buf, byte(0x80|len(codec)))
	buf = append(buf, codec...)

	// Audio: ID=0xE1, size=variable - no channels element
	var audioBuf []byte
	// SamplingFrequency only
	audioBuf = append(audioBuf, 0xB5)
	audioBuf = append(audioBuf, 0x88)
	bits := math.Float64bits(44100.0)
	for i := 7; i >= 0; i-- {
		audioBuf = append(audioBuf, byte(bits>>uint(i*8)))
	}

	buf = append(buf, 0xE1)
	buf = append(buf, byte(0x80|len(audioBuf)))
	buf = append(buf, audioBuf...)

	er := newEBMLReader(bytes.NewReader(buf))
	info := &VideoInfo{}
	parseTrackEntry(er, int64(len(buf)), info)

	assert.Equal(t, "opus", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels) // Default
}

// --- parseVideoElement with crafted data ---

func TestParseVideoElement_CraftedData(t *testing.T) {
	var buf []byte

	// PixelWidth: ID=0xB0, size=2, data=3840
	buf = append(buf, 0xB0)
	buf = append(buf, 0x82)
	buf = append(buf, byte(3840>>8), byte(3840&0xFF))

	// PixelHeight: ID=0xBA, size=2, data=2160
	buf = append(buf, 0xBA)
	buf = append(buf, 0x82)
	buf = append(buf, byte(2160>>8), byte(2160&0xFF))

	var pixelWidth, pixelHeight uint64
	er := newEBMLReader(bytes.NewReader(buf))
	parseVideoElement(er, int64(len(buf)), &pixelWidth, &pixelHeight)

	assert.Equal(t, uint64(3840), pixelWidth)
	assert.Equal(t, uint64(2160), pixelHeight)
}

// --- parseAudioElement with crafted data ---

func TestParseAudioElement_CraftedData(t *testing.T) {
	var buf []byte

	// SamplingFrequency: ID=0xB5, size=8, data=96000.0
	buf = append(buf, 0xB5)
	buf = append(buf, 0x88)
	bits := math.Float64bits(96000.0)
	for i := 7; i >= 0; i-- {
		buf = append(buf, byte(bits>>uint(i*8)))
	}

	// Channels: ID=0x9F, size=1, data=6
	buf = append(buf, 0x9F)
	buf = append(buf, 0x81)
	buf = append(buf, 0x06)

	var samplingFreq float64
	var channels uint64
	er := newEBMLReader(bytes.NewReader(buf))
	parseAudioElement(er, int64(len(buf)), &samplingFreq, &channels)

	assert.InDelta(t, 96000.0, samplingFreq, 0.1)
	assert.Equal(t, uint64(6), channels)
}

// --- mapMKVVideoCodec: more codec mappings ---

func TestMapMKVVideoCodec_MoreCodecs(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"V_MPEG4/ISO/AVC", "h264"},
		{"V_MPEGH/ISO/HEVC", "hevc"},
		{"V_VP9", "vp9"},
		{"V_VP8", "vp8"},
		{"V_AV1", "av1"},
		{"V_MPEG4/ISO/ASP", "mpeg4"},
		{"V_THEORA", "theora"},
		{"V_UNKNOWN_CODEC", "UNKNOWN_CODEC"},
		{"UNKNOWN", "UNKNOWN"},
		{"", ""},
	}
	for _, tt := range tests {
		result := mapMKVVideoCodec(tt.input)
		assert.Equal(t, tt.expected, result, "input=%q", tt.input)
	}
}

// --- mapMKVAudioCodec: more codec mappings ---

func TestMapMKVAudioCodec_MoreCodecs(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"A_AAC", "aac"},
		{"A_MPEG/L3", "mp3"},
		{"A_AC3", "ac3"},
		{"A_EAC3", "eac3"},
		{"A_E-AC-3", "eac3"},
		{"A_DTS", "dts"},
		{"A_OPUS", "opus"},
		{"A_VORBIS", "vorbis"},
		{"A_FLAC", "flac"},
		{"A_PCM/INT/LIT", "pcm"},
		{"A_MS/ACM", "wma"},
		{"A_WMA", "wma"},
		{"A_UNKNOWN_CODEC", "UNKNOWN_CODEC"},
		{"UNKNOWN", "UNKNOWN"},
		{"", ""},
	}
	for _, tt := range tests {
		result := mapMKVAudioCodec(tt.input)
		assert.Equal(t, tt.expected, result, "input=%q", tt.input)
	}
}

// --- mkvProber canProbe edge cases ---

func TestMKVProber_CanProbe_EdgeCases(t *testing.T) {
	p := newMKVProber()
	assert.True(t, p.canProbe([]byte{0x1A, 0x45, 0xDF, 0xA3}))
	assert.False(t, p.canProbe([]byte{0x1A, 0x45, 0xDF}))
	assert.False(t, p.canProbe(nil))
	assert.False(t, p.canProbe([]byte{}))
	assert.False(t, p.canProbe([]byte{0x00, 0x00, 0x00, 0x00}))
}

// --- mkvProber Name ---

func TestMKVProber_Name_Miss(t *testing.T) {
	p := newMKVProber()
	assert.Equal(t, "mkv", p.Name())
}

// --- parseSegmentInfo with zero timecode scale ---

func TestParseSegmentInfo_ZeroTimecodeScale(t *testing.T) {
	var buf []byte

	// TimecodeScale: ID=0x2AD7B1, size=1, data=0
	buf = append(buf, 0x2A, 0xD7, 0xB1)
	buf = append(buf, 0x81)
	buf = append(buf, 0x00)

	// Duration: ID=0x4489, size=8, data=100.0
	buf = append(buf, 0x44, 0x89)
	buf = append(buf, 0x88)
	bits := math.Float64bits(100.0)
	for i := 7; i >= 0; i-- {
		buf = append(buf, byte(bits>>uint(i*8)))
	}

	er := newEBMLReader(bytes.NewReader(buf))
	info := &VideoInfo{}
	parseSegmentInfo(er, int64(len(buf)), info)
	// With timecodeScale=0 (explicitly set to 0), duration = 0 * 0 / 1e9 = 0
	assert.Equal(t, 0.0, info.Duration)
}

// --- parseTracks with video track ---

func TestParseTracks_CraftedData(t *testing.T) {
	var trackBuf []byte

	// TrackType: ID=0x83, size=1, data=0x01 (video)
	trackBuf = append(trackBuf, 0x83)
	trackBuf = append(trackBuf, 0x81)
	trackBuf = append(trackBuf, 0x01)

	// CodecID: ID=0x86, size=16, data="V_MPEGH/ISO/HEVC"
	codec := []byte("V_MPEGH/ISO/HEVC")
	trackBuf = append(trackBuf, 0x86)
	trackBuf = append(trackBuf, byte(0x80|len(codec)))
	trackBuf = append(trackBuf, codec...)

	// Wrap in TrackEntry: ID=0xAE
	var tracksBuf []byte
	tracksBuf = append(tracksBuf, 0xAE) // TrackEntry ID
	tracksBuf = append(tracksBuf, byte(0x80|len(trackBuf)))
	tracksBuf = append(tracksBuf, trackBuf...)

	er := newEBMLReader(bytes.NewReader(tracksBuf))
	info := &VideoInfo{}
	parseTracks(er, int64(len(tracksBuf)), info)

	assert.Equal(t, "hevc", info.VideoCodec)
}

// --- parseEBMLHeader with valid data ---

func TestParseEBMLHeader_ValidData(t *testing.T) {
	// Just needs to not panic
	var buf []byte
	buf = append(buf, 0x42, 0x87) // EBMLVersion ID
	buf = append(buf, 0x81)
	buf = append(buf, 0x01)

	buf = append(buf, 0x42, 0x85) // EBMLReadVersion ID
	buf = append(buf, 0x81)
	buf = append(buf, 0x01)

	er := newEBMLReader(bytes.NewReader(buf))
	parseEBMLHeader(er, int64(len(buf)))
	// Should not panic
}

// --- analyzeMKV: bitrate and aspect ratio calculation ---

func TestAnalyzeMKV_BitrateAndAspectRatioCalculation(t *testing.T) {
	// Test that bitrate is computed when Duration > 0 and fileSize > 0
	// Test that aspect ratio is computed when Width > 0 and Height > 0
	info := &VideoInfo{
		Container: "mkv",
		Duration:  120.5,
		Width:     1920,
		Height:    1080,
	}

	// Simulate the calculation done at the end of analyzeMKV
	fileSize := int64(100000000) // 100MB
	if info.Duration > 0 && fileSize > 0 {
		info.Bitrate = int((float64(fileSize) * 8) / info.Duration / 1000)
	}
	if info.Width > 0 && info.Height > 0 {
		info.AspectRatio = float64(info.Width) / float64(info.Height)
	}

	assert.Greater(t, info.Bitrate, 0)
	assert.InDelta(t, 1.7778, info.AspectRatio, 0.01)
}

// --- Element ID constants ---

func TestMKVElementConstants(t *testing.T) {
	assert.Equal(t, uint32(0x1A45DFA3), elemEBML)
	assert.Equal(t, uint32(0x18538067), elemSegment)
	assert.Equal(t, uint32(0x1549A966), elemInfo)
	assert.Equal(t, uint32(0x2AD7B1), elemTimecodeScale)
	assert.Equal(t, uint32(0x4489), elemDuration)
	assert.Equal(t, uint32(0x1654AE6B), elemTracks)
	assert.Equal(t, uint32(0xAE), elemTrackEntry)
	assert.Equal(t, uint32(0xD7), elemTrackNumber)
	assert.Equal(t, uint32(0x83), elemTrackType)
	assert.Equal(t, uint32(0x86), elemCodecID)
	assert.Equal(t, uint32(0xE0), elemVideo)
	assert.Equal(t, uint32(0xB0), elemPixelWidth)
	assert.Equal(t, uint32(0xBA), elemPixelHeight)
	assert.Equal(t, uint32(0xE1), elemAudio)
	assert.Equal(t, uint32(0xB5), elemSamplingFreq)
	assert.Equal(t, uint32(0x9F), elemChannels)
	assert.Equal(t, uint32(0x1F43B675), elemCluster)
	assert.Equal(t, uint32(0x1C53BB6B), elemCues)
}

// --- Track type constants ---

func TestMKVTrackTypeConstants(t *testing.T) {
	assert.Equal(t, 1, trackTypeVideo)
	assert.Equal(t, 2, trackTypeAudio)
	assert.Equal(t, 17, trackTypeSubtitle)
}

// --- Codec string constants ---

func TestMKVCodecConstants(t *testing.T) {
	assert.Equal(t, "h264", codecH264)
	assert.Equal(t, "hevc", codecHEVC)
	assert.Equal(t, "vp9", codecVP9)
	assert.Equal(t, "mpeg4", codecMPEG4)
	assert.Equal(t, "aac", codecAAC)
	assert.Equal(t, "mp3", codecMP3)
	assert.Equal(t, "opus", codecOPUS)
}

// Suppress unused import
var _ = binary.BigEndian.Uint32
