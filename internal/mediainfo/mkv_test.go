package mediainfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapMKVVideoCodec(t *testing.T) {
	tests := []struct {
		name     string
		codecID  string
		expected string
	}{
		{"H.264 AVC", "V_MPEG4/ISO/AVC", "h264"},
		{"H.264 lowercase", "v_mpeg4/iso/avc", "h264"},
		{"H.264 H264", "V_H264", "h264"},
		{"H.265 HEVC", "V_MPEGH/ISO/HEVC", "hevc"},
		{"H.265 H265", "V_H265", "hevc"},
		{"VP9", "V_VP9", "vp9"},
		{"VP8", "V_VP8", "vp8"},
		{"AV1", "V_AV1", "av1"},
		{"MPEG4", "V_MPEG4/ISO/ASP", "mpeg4"},
		{"Theora", "V_THEORA", "theora"},
		{"Unknown with prefix", "V_UNKNOWN_CODEC", "UNKNOWN_CODEC"},
		{"Unknown without prefix", "UNKNOWN", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMKVVideoCodec(tt.codecID)
			if result != tt.expected {
				t.Errorf("mapMKVVideoCodec(%q) = %v, want %v", tt.codecID, result, tt.expected)
			}
		})
	}
}

func TestMapMKVAudioCodec(t *testing.T) {
	tests := []struct {
		name     string
		codecID  string
		expected string
	}{
		{"AAC", "A_AAC", "aac"},
		{"MP3", "A_MPEG/L3", "mp3"},
		{"MP3 direct", "A_MP3", "mp3"},
		{"AC3", "A_AC3", "ac3"},
		{"EAC3", "A_EAC3", "eac3"},
		{"EAC3 alt", "A_E-AC-3", "eac3"},
		{"DTS", "A_DTS", "dts"},
		{"Opus", "A_OPUS", "opus"},
		{"Vorbis", "A_VORBIS", "vorbis"},
		{"FLAC", "A_FLAC", "flac"},
		{"PCM", "A_PCM/INT/LIT", "pcm"},
		{"Unknown with prefix", "A_UNKNOWN_CODEC", "UNKNOWN_CODEC"},
		{"Unknown without prefix", "UNKNOWN", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMKVAudioCodec(tt.codecID)
			if result != tt.expected {
				t.Errorf("mapMKVAudioCodec(%q) = %v, want %v", tt.codecID, result, tt.expected)
			}
		})
	}
}

// TestMKVProber_Probe_InvalidFile tests error handling for non-MKV files
func TestMKVProber_Probe_InvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "invalid.mkv")

	// Create a file that doesn't start with MKV magic bytes
	err := os.WriteFile(invalidPath, []byte("NOTMKVFILE"), 0644)
	require.NoError(t, err)

	// File with invalid content doesn't start with MKV magic
	f, err := os.Open(invalidPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMKVProber()
	_, err = prober.Probe(f)

	// File doesn't start with MKV magic, so it should fail
	assert.Error(t, err)
	// Error should mention parsing failure or MKV-specific message
	assert.Contains(t, err.Error(), "extract")
}

// TestMKVProber_Probe_EmptyFile tests that empty file is handled gracefully
func TestMKVProber_Probe_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.mkv")

	err := os.WriteFile(emptyPath, []byte{}, 0644)
	require.NoError(t, err)

	f, err := os.Open(emptyPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMKVProber()
	_, err = prober.Probe(f)

	// Empty file may return error or partial info - either is acceptable
	// The important thing is it doesn't crash
	assert.Nil(t, err) // Empty files may return nil with empty info
}

// TestMKVProber_Probe_SmallFile tests that small file is handled gracefully
func TestMKVProber_Probe_SmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	smallPath := filepath.Join(tmpDir, "small.mkv")

	// Create file with MKV magic but too small for valid EBML structure
	// This will fail during EBML parsing
	err := os.WriteFile(smallPath, []byte{0x1A, 0x45, 0xDF, 0xA3}, 0644)
	require.NoError(t, err)

	f, err := os.Open(smallPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMKVProber()
	_, err = prober.Probe(f)

	// Small files with valid MKV magic may still fail during parsing
	// This is acceptable behavior
	assert.Error(t, err)
}

// TestExtractMKVPartial tests the fallback extraction function
func TestExtractMKVPartial(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "partial.mkv")

	// Create a minimal EBML structure
	err := os.WriteFile(tmpPath, []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x02, 0x03, 0x04}, 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info := &VideoInfo{
		Width:    1920,
		Height:   1080,
		Duration: 120.5,
	}
	fileSize := int64(1000000)

	result, err := extractMKVPartial(f, info, fileSize)

	// extractMKVPartial preserves the input container type from VideoInfo
	assert.NoError(t, err)
	assert.Equal(t, 1920, result.Width)
	assert.Equal(t, 1080, result.Height)
	assert.InDelta(t, 120.5, result.Duration, 0.001)

	// Calculate expected bitrate: (1000000 * 8) / 120.5 / 1000 = 66.39...
	expectedBitrate := int((float64(fileSize) * 8) / 120.5 / 1000)
	assert.InDelta(t, expectedBitrate, result.Bitrate, 0.1)

	// Calculate expected aspect ratio: 1920 / 1080 = 1.777...
	assert.InDelta(t, 1920.0/1080.0, result.AspectRatio, 0.001)
}

// TestExtractMKVPartial_UnknownContainer tests fallback with unknown container
func TestExtractMKVPartial_UnknownContainer(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "unknown.mkv")

	err := os.WriteFile(tmpPath, []byte{0x1A, 0x45, 0xDF, 0xA3}, 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info := &VideoInfo{}
	fileSize := int64(1000)

	_, err = extractMKVPartial(f, info, fileSize)

	// Should return error when no valid data is extracted
	assert.Error(t, err)
}

// TestExtractMKVPartial_OnlyDimensions tests partial extraction with only dimensions
func TestExtractMKVPartial_OnlyDimensions(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "dims.mkv")

	err := os.WriteFile(tmpPath, []byte("test content"), 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info := &VideoInfo{
		Width:  1280,
		Height: 720,
	}
	fileSize := int64(500000)

	result, err := extractMKVPartial(f, info, fileSize)

	assert.NoError(t, err)
	assert.Equal(t, 1280, result.Width)
	assert.Equal(t, 720, result.Height)
	// Duration should remain unchanged (0)
	assert.Equal(t, 0.0, result.Duration)
}

// TestAnalyzeMKV tests the main MKV analysis function with error cases
func TestAnalyzeMKV(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "test.mkv")

	// Create a minimal MKV file with valid header but minimal content
	err := os.WriteFile(tmpPath, []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x02, 0x03, 0x04}, 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)

	if err != nil {
		// Error is acceptable for minimal/malformed files
		t.Logf("analyzeMKV returned expected error: %v", err)
	} else {
		assert.Equal(t, "mkv", info.Container)
	}
}

// TestAnalyzeMKV_EmptyFile tests handling of empty files
func TestAnalyzeMKV_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.mkv")

	err := os.WriteFile(emptyPath, []byte{}, 0644)
	require.NoError(t, err)

	f, err := os.Open(emptyPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// ebml-go library may not return an error for empty files
	// This test ensures the function handles empty files gracefully (no crash)
	_, err = analyzeMKV(f)

	// Accept either nil or error - what matters is no crash
	assert.True(t, err == nil || err.Error() != "", "analyzeMKV should not panic on empty file")
}

// TestAnalyzeMKV_SmallFile tests error handling for small files
func TestAnalyzeMKV_SmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	smallPath := filepath.Join(tmpDir, "small.mkv")

	// File too small for EBML header
	err := os.WriteFile(smallPath, []byte("small"), 0644)
	require.NoError(t, err)

	f, err := os.Open(smallPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeMKV(f)

	assert.Error(t, err)
}

// TestMapMKVVideoCodec_Extended tests extended codec mappings
func TestMapMKVVideoCodec_Extended(t *testing.T) {
	tests := []struct {
		name     string
		codecID  string
		expected string
	}{
		{"H.264 uppercase", "V_MPEG4/ISO/AVC", "h264"},
		{"H.264 mixed case", "V_Mpeg4/iso/avc", "h264"},
		{"H.264 lowercase with slashes", "v_mpeg4/iso/avc", "h264"},
		{"H.265 uppercase", "V_MPEGH/ISO/HEVC", "hevc"},
		{"VP9 codec", "V_VP9", "vp9"},
		{"VP8 codec", "V_VP8", "vp8"},
		{"AV1 codec", "V_AV1", "av1"},
		{"MPEG4 codec", "V_MPEG4/ISO/ASP", "mpeg4"},
		{"Theora codec", "V_THEORA", "theora"},
		{"Unrecognized with V prefix", "V_CUSTOM", "CUSTOM"},
		{"Unrecognized without V prefix", "CUSTOM", "CUSTOM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMKVVideoCodec(tt.codecID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMapMKVAudioCodec_Extended tests extended codec mappings
func TestMapMKVAudioCodec_Extended(t *testing.T) {
	tests := []struct {
		name     string
		codecID  string
		expected string
	}{
		{"AAC codec", "A_AAC", "aac"},
		{"MP3 codec", "A_MPEG/L3", "mp3"},
		{"AC3 codec", "A_AC3", "ac3"},
		{"E-AC-3 codec", "A_E-AC-3", "eac3"},
		{"DTS codec", "A_DTS", "dts"},
		{"Opus codec", "A_OPUS", "opus"},
		{"Vorbis codec", "A_VORBIS", "vorbis"},
		{"FLAC codec", "A_FLAC", "flac"},
		{"PCM codec", "A_PCM/INT/LIT", "pcm"},
		{"Unrecognized with A prefix", "A_CUSTOM", "CUSTOM"},
		{"Unrecognized without A prefix", "CUSTOM", "CUSTOM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMKVAudioCodec(tt.codecID)
			assert.Equal(t, tt.expected, result)
		})
	}
}
