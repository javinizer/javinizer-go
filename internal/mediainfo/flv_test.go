package mediainfo

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a minimal valid FLV file for testing
func createTestFLV(t *testing.T, tmpDir string, videoCodecID uint8, audioCodecID uint8, includeMetadata bool) string {
	flvPath := filepath.Join(tmpDir, "test.flv")
	f, err := os.Create(flvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// FLV header
	writeBytes(t, f, []byte("FLV"))
	writeByte(t, f, 1)     // Version
	writeByte(t, f, 5)     // TypeFlags (audio + video)
	writeUint32BE(t, f, 9) // DataOffset

	// First PreviousTagSize (always 0)
	writeUint32BE(t, f, 0)

	if includeMetadata {
		// Script data tag (onMetaData)
		writeByte(t, f, 18) // Tag type: script data

		// Build metadata content
		metadataContent := buildAMF0Metadata(t)
		dataSize := uint32(len(metadataContent))

		// Write data size (24-bit)
		write24BitBE(t, f, dataSize)

		// Timestamp (24-bit + 8-bit extension) = 0
		write24BitBE(t, f, 0)
		writeByte(t, f, 0) // Timestamp extension

		// StreamID (24-bit, always 0)
		write24BitBE(t, f, 0)

		// Write metadata content
		writeBytes(t, f, metadataContent)

		// PreviousTagSize
		writeUint32BE(t, f, dataSize+11)
	}

	// Video tag
	writeByte(t, f, 9) // Tag type: video
	// FLV video tag format: bits 7-4 = frame type (1=keyframe), bits 3-0 = codec ID
	frameType := byte(0x10) // Keyframe
	videoData := []byte{frameType | (videoCodecID & 0x0F), 0x00, 0x00, 0x00}
	write24BitBE(t, f, uint32(len(videoData)))
	write24BitBE(t, f, 33) // Timestamp 33ms
	writeByte(t, f, 0)
	write24BitBE(t, f, 0)
	writeBytes(t, f, videoData)
	writeUint32BE(t, f, uint32(len(videoData)+11))

	// Audio tag
	writeByte(t, f, 8)                                // Tag type: audio
	soundFormat := (audioCodecID << 4) | (3 << 2) | 1 // 44.1kHz, stereo
	audioData := []byte{soundFormat, 0x00, 0x00, 0x00}
	write24BitBE(t, f, uint32(len(audioData)))
	write24BitBE(t, f, 66) // Timestamp 66ms
	writeByte(t, f, 0)
	write24BitBE(t, f, 0)
	writeBytes(t, f, audioData)
	writeUint32BE(t, f, uint32(len(audioData)+11))

	return flvPath
}

// Build AMF0 encoded metadata
func buildAMF0Metadata(t *testing.T) []byte {
	var data []byte

	// String type (0x02) + "onMetaData"
	data = append(data, 0x02)
	data = append(data, 0x00, 0x0A) // Length: 10
	data = append(data, []byte("onMetaData")...)

	// ECMA array type (0x08)
	data = append(data, 0x08)
	data = append(data, 0x00, 0x00, 0x00, 0x05) // Array length: 5 properties

	// Property: duration (number)
	data = append(data, 0x00, 0x08) // "duration" length
	data = append(data, []byte("duration")...)
	data = append(data, 0x00) // Number type
	durationBits := math.Float64bits(10.5)
	durationBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(durationBytes, durationBits)
	data = append(data, durationBytes...)

	// Property: width (number)
	data = append(data, 0x00, 0x05) // "width" length
	data = append(data, []byte("width")...)
	data = append(data, 0x00)
	widthBits := math.Float64bits(1920.0)
	widthBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(widthBytes, widthBits)
	data = append(data, widthBytes...)

	// Property: height (number)
	data = append(data, 0x00, 0x06) // "height" length
	data = append(data, []byte("height")...)
	data = append(data, 0x00)
	heightBits := math.Float64bits(1080.0)
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, heightBits)
	data = append(data, heightBytes...)

	// Property: framerate (number)
	data = append(data, 0x00, 0x09) // "framerate" length
	data = append(data, []byte("framerate")...)
	data = append(data, 0x00)
	fpsBits := math.Float64bits(30.0)
	fpsBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(fpsBytes, fpsBits)
	data = append(data, fpsBytes...)

	// Property: videocodecid (number)
	data = append(data, 0x00, 0x0C) // "videocodecid" length
	data = append(data, []byte("videocodecid")...)
	data = append(data, 0x00)
	codecBits := math.Float64bits(7.0) // H.264
	codecBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(codecBytes, codecBits)
	data = append(data, codecBytes...)

	// End of object marker
	data = append(data, 0x00, 0x00, 0x09)

	return data
}

func TestFLVProber_Name(t *testing.T) {
	prober := NewFLVProber()
	assert.Equal(t, "flv", prober.Name())
}

func TestFLVProber_CanProbe(t *testing.T) {
	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{
			name:     "Valid FLV header",
			header:   []byte{'F', 'L', 'V', 0x01, 0x05},
			expected: true,
		},
		{
			name:     "Invalid signature",
			header:   []byte{'X', 'L', 'V', 0x01, 0x05},
			expected: false,
		},
		{
			name:     "Header too short",
			header:   []byte{'F', 'L'},
			expected: false,
		},
	}

	prober := NewFLVProber()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prober.CanProbe(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFLVProber_Probe_WithMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	flvPath := createTestFLV(t, tmpDir, 7, 10, true) // H.264 + AAC, with metadata

	f, err := os.Open(flvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewFLVProber()
	info, err := prober.Probe(f)

	require.NoError(t, err)
	assert.Equal(t, "flv", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.InDelta(t, 30.0, info.FrameRate, 0.1)
	assert.InDelta(t, 10.5, info.Duration, 0.1)
}

func TestFLVProber_Probe_WithoutMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	flvPath := createTestFLV(t, tmpDir, 7, 10, false) // H.264 + AAC, no metadata

	f, err := os.Open(flvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewFLVProber()
	info, err := prober.Probe(f)

	require.NoError(t, err)
	assert.Equal(t, "flv", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, "aac", info.AudioCodec)
	// Without metadata, dimensions won't be set
	assert.Equal(t, 0, info.Width)
	assert.Equal(t, 0, info.Height)
}

func TestFLVProber_Probe_InvalidSignature(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "invalid.flv")

	err := os.WriteFile(invalidPath, []byte("XXXXX"), 0644)
	require.NoError(t, err)

	f, err := os.Open(invalidPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewFLVProber()
	_, err = prober.Probe(f)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid FLV signature")
}

func TestFLVProber_Probe_TruncatedHeader(t *testing.T) {
	tmpDir := t.TempDir()
	truncatedPath := filepath.Join(tmpDir, "truncated.flv")

	err := os.WriteFile(truncatedPath, []byte("FLV"), 0644)
	require.NoError(t, err)

	f, err := os.Open(truncatedPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewFLVProber()
	_, err = prober.Probe(f)

	assert.Error(t, err)
}

func TestMapFLVVideoCodec(t *testing.T) {
	tests := []struct {
		name     string
		codecID  uint8
		expected string
	}{
		{"Sorenson H.263", 2, "h263"},
		{"Screen video", 3, "screen_video"},
		{"VP6", 4, "vp6"},
		{"VP6 with alpha", 5, "vp6_alpha"},
		{"Screen video v2", 6, "screen_video_v2"},
		{"H.264 AVC", 7, "h264"},
		{"H.265 HEVC", 12, "h265"},
		{"Unknown codec", 99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapFLVVideoCodec(tt.codecID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapFLVAudioCodec(t *testing.T) {
	tests := []struct {
		name     string
		codecID  uint8
		expected string
	}{
		{"PCM platform endian", 0, "pcm"},
		{"ADPCM", 1, "adpcm"},
		{"MP3", 2, "mp3"},
		{"PCM little endian", 3, "pcm_le"},
		{"Nellymoser 16kHz", 4, "nellymoser_16khz"},
		{"Nellymoser 8kHz", 5, "nellymoser_8khz"},
		{"Nellymoser", 6, "nellymoser"},
		{"G.711 A-law", 7, "g711_alaw"},
		{"G.711 mu-law", 8, "g711_mulaw"},
		{"AAC", 10, "aac"},
		{"Speex", 11, "speex"},
		{"MP3 8kHz", 14, "mp3_8khz"},
		{"Device specific", 15, "device_specific"},
		{"Unknown codec", 99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapFLVAudioCodec(tt.codecID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapFLVSampleRate(t *testing.T) {
	tests := []struct {
		name     string
		code     uint8
		expected int
	}{
		{"5.5 kHz", 0, 5512},
		{"11 kHz", 1, 11025},
		{"22 kHz", 2, 22050},
		{"44.1 kHz", 3, 44100},
		{"Unknown", 99, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapFLVSampleRate(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsNaN(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		expected bool
	}{
		{"Normal number", 42.0, false},
		{"Zero", 0.0, false},
		{"Negative", -10.5, false},
		{"NaN", math.NaN(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNaN(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadFLVTag(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "tag_test.flv")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	// Write a valid FLV tag header
	writeByte(t, f, 9)       // Tag type: video
	write24BitBE(t, f, 100)  // Data size
	write24BitBE(t, f, 1000) // Timestamp (24-bit)
	writeByte(t, f, 0)       // Timestamp extension
	write24BitBE(t, f, 0)    // Stream ID

	_ = f.Close()

	// Reopen for reading
	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	tag, err := readFLVTag(f)
	require.NoError(t, err)
	assert.Equal(t, uint8(9), tag.TagType)
	assert.Equal(t, uint32(100), tag.DataSize)
	assert.Equal(t, uint32(1000), tag.Timestamp)
	assert.Equal(t, uint32(0), tag.StreamID)
}

func TestReadByte(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "byte_test.bin")

	err := os.WriteFile(testPath, []byte{0x42}, 0644)
	require.NoError(t, err)

	f, err := os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	b, err := readByte(f)
	require.NoError(t, err)
	assert.Equal(t, byte(0x42), b)
}

func TestParseAMF0_SimpleProperties(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "amf0_test.bin")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	// Write AMF0 data
	// String type + "onMetaData"
	writeByte(t, f, 0x02)
	writeUint16BE(t, f, 10)
	writeBytes(t, f, []byte("onMetaData"))

	// Object type
	writeByte(t, f, 0x03)

	// Property: width (number)
	writeUint16BE(t, f, 5) // "width" length
	writeBytes(t, f, []byte("width"))
	writeByte(t, f, 0x00) // Number type
	widthBits := math.Float64bits(1920.0)
	widthBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(widthBytes, widthBits)
	writeBytes(t, f, widthBytes)

	// Property: enabled (boolean)
	writeUint16BE(t, f, 7) // "enabled" length
	writeBytes(t, f, []byte("enabled"))
	writeByte(t, f, 0x01) // Boolean type
	writeByte(t, f, 1)    // true

	// Property: name (string)
	writeUint16BE(t, f, 4) // "name" length
	writeBytes(t, f, []byte("name"))
	writeByte(t, f, 0x02)  // String type
	writeUint16BE(t, f, 4) // String length
	writeBytes(t, f, []byte("test"))

	// End of object marker
	writeUint16BE(t, f, 0)
	writeByte(t, f, 0x09)

	_ = f.Close()

	// Reopen for reading
	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	fileInfo, err := f.Stat()
	require.NoError(t, err)
	metadata, err := parseAMF0(f, uint32(fileInfo.Size()))

	require.NoError(t, err)
	assert.Equal(t, 1920.0, metadata["width"])
	assert.Equal(t, true, metadata["enabled"])
	assert.Equal(t, "test", metadata["name"])
}

func TestApplyFLVMetadata(t *testing.T) {
	info := &VideoInfo{}
	metadata := map[string]interface{}{
		"width":           1920.0,
		"height":          1080.0,
		"duration":        120.5,
		"framerate":       29.97,
		"videocodecid":    7.0,  // H.264
		"audiocodecid":    10.0, // AAC
		"audiosamplerate": 48000.0,
		"audiochannels":   2.0,
	}

	applyFLVMetadata(info, metadata)

	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, 120.5, info.Duration)
	assert.InDelta(t, 29.97, info.FrameRate, 0.01)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 48000, info.SampleRate)
	assert.Equal(t, 2, info.AudioChannels)
}

func TestApplyFLVMetadata_UnknownCodec(t *testing.T) {
	info := &VideoInfo{VideoCodec: "existing"}
	metadata := map[string]interface{}{
		"videocodecid": 99.0, // Unknown codec
	}

	applyFLVMetadata(info, metadata)

	// Should not overwrite with "unknown"
	assert.Equal(t, "existing", info.VideoCodec)
}

func TestFLVProber_Probe_MalformedTags(t *testing.T) {
	tmpDir := t.TempDir()
	flvPath := filepath.Join(tmpDir, "malformed.flv")
	f, err := os.Create(flvPath)
	require.NoError(t, err)

	// FLV header
	writeBytes(t, f, []byte("FLV"))
	writeByte(t, f, 1)
	writeByte(t, f, 5)
	writeUint32BE(t, f, 9)

	// First PreviousTagSize
	writeUint32BE(t, f, 0)

	// Truncated tag - will cause readFLVTag to fail
	writeByte(t, f, 9) // Tag type
	// Missing rest of tag header

	_ = f.Close()

	f, err = os.Open(flvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewFLVProber()
	info, err := prober.Probe(f)

	// Should handle gracefully
	require.NoError(t, err)
	assert.Equal(t, "flv", info.Container)
}

func TestParseAMF0_UnexpectedType(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "amf0_unexpected.bin")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	// Write invalid AMF0 data - wrong type after "onMetaData"
	writeByte(t, f, 0x02) // String type
	writeUint16BE(t, f, 10)
	writeBytes(t, f, []byte("onMetaData"))

	// Invalid type (not ECMA array or Object)
	writeByte(t, f, 0xFF) // Invalid type

	_ = f.Close()

	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	fileInfo, err := f.Stat()
	require.NoError(t, err)
	_, err = parseAMF0(f, uint32(fileInfo.Size()))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected AMF type")
}

func TestParseAMF0_ECMAArray(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "amf0_ecma.bin")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	// String type + "onMetaData"
	writeByte(t, f, 0x02)
	writeUint16BE(t, f, 10)
	writeBytes(t, f, []byte("onMetaData"))

	// ECMA array type
	writeByte(t, f, 0x08)
	writeUint32BE(t, f, 1) // Array length: 1

	// Property: test (number)
	writeUint16BE(t, f, 4)
	writeBytes(t, f, []byte("test"))
	writeByte(t, f, 0x00)                   // Number type
	writeUint64BE(t, f, 0x4024000000000000) // 10.0

	// Property with nested ECMA array (should skip)
	writeUint16BE(t, f, 6)
	writeBytes(t, f, []byte("nested"))
	writeByte(t, f, 0x08) // ECMA array type
	writeUint32BE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	fileInfo, err := f.Stat()
	require.NoError(t, err)
	metadata, err := parseAMF0(f, uint32(fileInfo.Size()))

	require.NoError(t, err)
	assert.Equal(t, 10.0, metadata["test"])
}

func TestParseAMF0_StrictArray(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "amf0_strict.bin")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	// String + "onMetaData"
	writeByte(t, f, 0x02)
	writeUint16BE(t, f, 10)
	writeBytes(t, f, []byte("onMetaData"))

	// Object type
	writeByte(t, f, 0x03)

	// Property with Strict Array (0x0A) - should skip
	writeUint16BE(t, f, 4)
	writeBytes(t, f, []byte("test"))
	writeByte(t, f, 0x0A)  // Strict array type
	writeUint32BE(t, f, 2) // Array length

	_ = f.Close()

	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	fileInfo, err := f.Stat()
	require.NoError(t, err)
	metadata, err := parseAMF0(f, uint32(fileInfo.Size()))

	require.NoError(t, err)
	// Should skip the strict array and return empty metadata
	assert.NotNil(t, metadata)
}

func TestParseAMF0_DateType(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "amf0_date.bin")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	// String + "onMetaData"
	writeByte(t, f, 0x02)
	writeUint16BE(t, f, 10)
	writeBytes(t, f, []byte("onMetaData"))

	// Object type
	writeByte(t, f, 0x03)

	// Property: date (Date type 0x0B)
	writeUint16BE(t, f, 4)
	writeBytes(t, f, []byte("date"))
	writeByte(t, f, 0x0B) // Date type
	// Date is 8 bytes + 2 bytes timezone
	writeUint64BE(t, f, 0x0000000000000000)
	writeUint16BE(t, f, 0)

	// End marker
	writeUint16BE(t, f, 0)
	writeByte(t, f, 0x09)

	_ = f.Close()

	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	fileInfo, err := f.Stat()
	require.NoError(t, err)
	metadata, err := parseAMF0(f, uint32(fileInfo.Size()))

	require.NoError(t, err)
	// Date should be skipped but parsing should succeed
	assert.NotNil(t, metadata)
}

func TestParseAMF0_NestedObject(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "amf0_nested.bin")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	// String + "onMetaData"
	writeByte(t, f, 0x02)
	writeUint16BE(t, f, 10)
	writeBytes(t, f, []byte("onMetaData"))

	// Object type
	writeByte(t, f, 0x03)

	// Property: nested (Object type 0x03)
	writeUint16BE(t, f, 6)
	writeBytes(t, f, []byte("nested"))
	writeByte(t, f, 0x03) // Nested object

	_ = f.Close()

	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	fileInfo, err := f.Stat()
	require.NoError(t, err)
	metadata, err := parseAMF0(f, uint32(fileInfo.Size()))

	require.NoError(t, err)
	// Should skip nested object
	assert.NotNil(t, metadata)
}

// Helper to write uint64 big endian
func writeUint64BE(t *testing.T, f *os.File, value uint64) {
	bytes := []byte{
		byte(value >> 56),
		byte(value >> 48),
		byte(value >> 40),
		byte(value >> 32),
		byte(value >> 24),
		byte(value >> 16),
		byte(value >> 8),
		byte(value),
	}
	_, err := f.Write(bytes)
	require.NoError(t, err)
}

// Helper functions for writing binary data (Big Endian for FLV)
func writeByte(t *testing.T, f *os.File, value byte) {
	_, err := f.Write([]byte{value})
	require.NoError(t, err)
}

func writeUint32BE(t *testing.T, f *os.File, value uint32) {
	err := binary.Write(f, binary.BigEndian, value)
	require.NoError(t, err)
}

func writeUint16BE(t *testing.T, f *os.File, value uint16) {
	err := binary.Write(f, binary.BigEndian, value)
	require.NoError(t, err)
}

func write24BitBE(t *testing.T, f *os.File, value uint32) {
	bytes := []byte{
		byte((value >> 16) & 0xFF),
		byte((value >> 8) & 0xFF),
		byte(value & 0xFF),
	}
	_, err := f.Write(bytes)
	require.NoError(t, err)
}
