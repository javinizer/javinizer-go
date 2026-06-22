package mediainfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- analyzeMKV: full MKV with EBML header + Segment + Info + Tracks ---

func TestMiss2_AnalyzeMKV_FullStructure(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "test.mkv")

	data := buildTestMKVData()
	require.NoError(t, os.WriteFile(mkvPath, data, 0644))

	f, err := os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	assert.Greater(t, info.Duration, 0.0)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, "aac", info.AudioCodec)
}

func buildTestMKVData() []byte {
	// EBML Header element
	ebmlHeaderContent := append(writeEBMLUint(0x4286, 1), writeEBMLUint(0x42F7, 1)...)
	ebmlHeader := writeEBMLElement(0x1A45DFA3, ebmlHeaderContent)

	// Segment Info
	segmentInfoContent := append(writeEBMLUint(0x2AD7B1, 1000000), writeEBMLFloat(0x4489, 120.5)...)
	segmentInfo := writeEBMLElement(0x1549A966, segmentInfoContent)

	// Video track
	videoEl := writeEBMLElement(0xE0, append(writeEBMLUint(0xB0, 1920), writeEBMLUint(0xBA, 1080)...))
	videoTrackContent := append(
		append(
			append(writeEBMLUint(0xD7, 1), writeEBMLUint(0x83, 1)...),
			writeEBMLString(0x86, "V_MPEG4/ISO/AVC")...,
		),
		videoEl...,
	)
	videoTrack := writeEBMLElement(0xAE, videoTrackContent)

	// Audio track
	audioEl := writeEBMLElement(0xE1, append(writeEBMLFloat(0xB5, 48000.0), writeEBMLUint(0x9F, 2)...))
	audioTrackContent := append(
		append(
			append(writeEBMLUint(0xD7, 2), writeEBMLUint(0x83, 2)...),
			writeEBMLString(0x86, "A_AAC")...,
		),
		audioEl...,
	)
	audioTrack := writeEBMLElement(0xAE, audioTrackContent)

	// Tracks
	tracks := writeEBMLElement(0x1654AE6B, append(videoTrack, audioTrack...))

	// Segment element
	segmentContent := append(segmentInfo, tracks...)
	segment := append(append([]byte{0x18, 0x53, 0x80, 0x67}, encodeEBMLSize(uint64(len(segmentContent)))...), segmentContent...)

	return append(ebmlHeader, segment...)
}

// --- analyzeMKV: no usable data returns error ---

func TestMiss2_AnalyzeMKV_NoUsableData(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "empty.mkv")

	data := writeEBMLElement(0x1A45DFA3, writeEBMLUint(0x4286, 1))
	require.NoError(t, os.WriteFile(mkvPath, data, 0644))

	f, err := os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeMKV(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no usable data")
}

// --- analyzeMKV: with cluster element that gets skipped ---

func TestMiss2_AnalyzeMKV_WithCluster(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "cluster.mkv")

	ebmlHeader := writeEBMLElement(0x1A45DFA3, writeEBMLUint(0x4286, 1))
	segmentInfo := writeEBMLElement(0x1549A966, append(writeEBMLUint(0x2AD7B1, 1000000), writeEBMLFloat(0x4489, 60.0)...))
	cluster := writeEBMLElement(0x1F43B675, make([]byte, 10))

	segmentContent := append(segmentInfo, cluster...)
	segment := append(append([]byte{0x18, 0x53, 0x80, 0x67}, encodeEBMLSize(uint64(len(segmentContent)))...), segmentContent...)

	data := append(ebmlHeader, segment...)
	require.NoError(t, os.WriteFile(mkvPath, data, 0644))

	f, err := os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Greater(t, info.Duration, 0.0)
}

// --- mapMKVVideoCodec: various codecs ---

func TestMiss2_MapMKVVideoCodec(t *testing.T) {
	assert.Equal(t, "h264", mapMKVVideoCodec("V_MPEG4/ISO/AVC"))
	assert.Equal(t, "hevc", mapMKVVideoCodec("V_MPEGH/ISO/HEVC"))
	assert.Equal(t, "vp9", mapMKVVideoCodec("V_VP9"))
	assert.Equal(t, "vp8", mapMKVVideoCodec("V_VP8"))
	assert.Equal(t, "av1", mapMKVVideoCodec("V_AV1"))
	assert.Equal(t, "mpeg4", mapMKVVideoCodec("V_MPEG4/ISO/ASP"))
	assert.Equal(t, "theora", mapMKVVideoCodec("V_THEORA"))
	assert.Equal(t, "CUSTOM", mapMKVVideoCodec("V_CUSTOM"))
}

// --- mapMKVAudioCodec: various codecs ---

func TestMiss2_MapMKVAudioCodec(t *testing.T) {
	assert.Equal(t, "aac", mapMKVAudioCodec("A_AAC"))
	assert.Equal(t, "mp3", mapMKVAudioCodec("A_MPEG/L3"))
	assert.Equal(t, "ac3", mapMKVAudioCodec("A_AC3"))
	assert.Equal(t, "eac3", mapMKVAudioCodec("A_EAC3"))
	assert.Equal(t, "dts", mapMKVAudioCodec("A_DTS"))
	assert.Equal(t, "opus", mapMKVAudioCodec("A_OPUS"))
	assert.Equal(t, "vorbis", mapMKVAudioCodec("A_VORBIS"))
	assert.Equal(t, "flac", mapMKVAudioCodec("A_FLAC"))
	assert.Equal(t, "pcm", mapMKVAudioCodec("A_PCM/INT/LIT"))
	assert.Equal(t, "wma", mapMKVAudioCodec("A_MS/ACM"))
	assert.Equal(t, "wma", mapMKVAudioCodec("A_WMA"))
	assert.Equal(t, "CUSTOM", mapMKVAudioCodec("A_CUSTOM"))
}

// --- mkvProber canProbe ---

func TestMiss2_MKVCanProbe(t *testing.T) {
	p := newMKVProber()
	assert.True(t, p.canProbe([]byte{0x1A, 0x45, 0xDF, 0xA3}))
	assert.False(t, p.canProbe([]byte{0x00, 0x00, 0x00, 0x00}))
	assert.False(t, p.canProbe([]byte{0x1A, 0x45}))
}

// --- mkvProber Probe ---

func TestMiss2_MKVProbe(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "probe.mkv")

	data := buildTestMKVData()
	require.NoError(t, os.WriteFile(mkvPath, data, 0644))

	f, err := os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	p := newMKVProber()
	assert.Equal(t, "mkv", p.Name())

	info, err := p.Probe(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
}
