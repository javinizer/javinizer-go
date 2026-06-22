package mediainfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMKVProberCanProbeDeep2_ValidHeader(t *testing.T) {
	p := newMKVProber()
	header := []byte{0x1A, 0x45, 0xDF, 0xA3}
	assert.True(t, p.canProbe(header))
}

func TestMKVProberCanProbeDeep2_InvalidHeader(t *testing.T) {
	p := newMKVProber()
	assert.False(t, p.canProbe([]byte{0, 0, 0, 0}))
	assert.False(t, p.canProbe(nil))
	assert.False(t, p.canProbe([]byte{}))
	assert.False(t, p.canProbe([]byte{0x1A}))
}

func TestMKVProberNameDeep2(t *testing.T) {
	p := newMKVProber()
	assert.Equal(t, "mkv", p.Name())
}

func TestMKVCodecMappingDeep2(t *testing.T) {
	assert.Equal(t, "h264", codecH264)
	assert.Equal(t, "hevc", codecHEVC)
	assert.Equal(t, "vp9", codecVP9)
	assert.Equal(t, "aac", codecAAC)
	assert.Equal(t, "mp3", codecMP3)
	assert.Equal(t, "opus", codecOPUS)
}

func TestMKVTrackTypesDeep2(t *testing.T) {
	assert.Equal(t, 1, trackTypeVideo)
	assert.Equal(t, 2, trackTypeAudio)
	assert.Equal(t, 17, trackTypeSubtitle)
}

func TestMapMKVVideoCodecDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"V_MPEG4/ISO/AVC", "h264"},
		{"V_MPEGH/ISO/HEVC", "hevc"},
		{"V_VP9", "vp9"},
		{"V_MPEG4/ISO/ASP", "mpeg4"},
		{"V_UNKNOWN", "UNKNOWN"}, // unknown codec strips V_ prefix
	}
	for _, tt := range tests {
		result := mapMKVVideoCodec(tt.input)
		assert.Equal(t, tt.expected, result, "input=%q", tt.input)
	}
}

func TestMapMKVAudioCodecDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"A_AAC", "aac"},
		{"A_MPEG/L3", "mp3"},
		{"A_OPUS", "opus"},
		{"A_UNKNOWN", "UNKNOWN"}, // strips A_ prefix
	}
	for _, tt := range tests {
		result := mapMKVAudioCodec(tt.input)
		assert.Equal(t, tt.expected, result, "input=%q", tt.input)
	}
}

func TestEBMLElementConstantsDeep2(t *testing.T) {
	// Verify element IDs are correct
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
}
