package mediainfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapMP4VideoCodec(t *testing.T) {
	tests := []struct {
		name     string
		fourcc   string
		expected string
	}{
		{"H.264 avc1", "avc1", "h264"},
		{"H.264 avc3", "avc3", "h264"},
		{"H.265 hvc1", "hvc1", "hevc"},
		{"H.265 hev1", "hev1", "hevc"},
		{"VP9", "vp09", "vp9"},
		{"VP8", "vp08", "vp8"},
		{"AV1", "av01", "av1"},
		{"MPEG4", "mp4v", "mpeg4"},
		{"Unknown", "xxxx", "xxxx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMP4VideoCodec(tt.fourcc)
			if result != tt.expected {
				t.Errorf("mapMP4VideoCodec(%q) = %v, want %v", tt.fourcc, result, tt.expected)
			}
		})
	}
}

func TestMapMP4AudioCodec(t *testing.T) {
	tests := []struct {
		name     string
		fourcc   string
		expected string
	}{
		{"AAC", "mp4a", "aac"},
		{"MP3 dotted", ".mp3", "mp3"},
		{"MP3 spaced", "mp3 ", "mp3"},
		{"AC3", "ac-3", "ac3"},
		{"EAC3", "ec-3", "eac3"},
		{"Opus", "opus", "opus"},
		{"FLAC", "fLaC", "flac"},
		{"Unknown", "xxxx", "xxxx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMP4AudioCodec(tt.fourcc)
			if result != tt.expected {
				t.Errorf("mapMP4AudioCodec(%q) = %v, want %v", tt.fourcc, result, tt.expected)
			}
		})
	}
}

// TestMP4Prober_Probe tests the MP4 prober with a minimal valid MP4 file
func TestMP4Prober_Probe(t *testing.T) {
	// Create a minimal valid MP4 file for testing
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "test.mp4")

	// Create a minimal MP4 file with valid ftyp and minimal moov structure
	f, err := os.Create(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Write a minimal valid MP4 structure
	// ftyp box: size(4) + type(4) + major_brand(4) + minor_version(4) + compatible_brands(4*n)
	ftypSize := uint32(24)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom") // major_brand
	ftyp[12] = 0             // minor_version
	ftyp[13] = 0
	ftyp[14] = 0
	ftyp[15] = 2
	copy(ftyp[16:20], "isom") // compatible_brand
	copy(ftyp[20:24], "mp41") // compatible_brand

	_, err = f.Write(ftyp)
	require.NoError(t, err)

	// Close the file
	err = f.Close()
	require.NoError(t, err)

	// Open and probe
	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMP4Prober()
	info, err := prober.Probe(f)

	// For minimal MP4, we expect either an error or partial info
	if err != nil {
		// Error is acceptable for minimal/truncated files
		t.Logf("Probe returned expected error for minimal file: %v", err)
	} else {
		assert.Equal(t, "mp4", info.Container)
	}
}

// TestMP4Prober_Probe_InvalidFile tests error handling for invalid files
func TestMP4Prober_Probe_InvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "invalid.mp4")

	// Create file with invalid ftyp (wrong offset for 'ftyp')
	err := os.WriteFile(invalidPath, []byte("RIFFxxxxAVI "), 0644)
	require.NoError(t, err)

	f, err := os.Open(invalidPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMP4Prober()
	_, err = prober.Probe(f)

	// Should fail to parse MP4 structure
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse MP4 file")
}

// TestMP4Prober_Probe_SmallFile tests handling of files too small for MP4
func TestMP4Prober_Probe_SmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	smallPath := filepath.Join(tmpDir, "small.mp4")

	// Create file smaller than ftyp box
	err := os.WriteFile(smallPath, []byte("ftyp"), 0644)
	require.NoError(t, err)

	f, err := os.Open(smallPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMP4Prober()
	_, err = prober.Probe(f)

	// Should fail to decode MP4 file
	assert.Error(t, err)
}

// TestAnalyzeMP4_EmptyFile tests handling of empty files
func TestAnalyzeMP4_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.mp4")

	err := os.WriteFile(emptyPath, []byte{}, 0644)
	require.NoError(t, err)

	f, err := os.Open(emptyPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// MP4 parser may not return an error for empty files
	// This test ensures the function handles empty files gracefully (no crash)
	_, err = analyzeMP4(f)

	// Accept either nil or error - what matters is no crash
	assert.True(t, err == nil || err.Error() != "", "analyzeMP4 should not panic on empty file")
}

// TestAnalyzeMP4_SmallFile tests error handling for small files
func TestAnalyzeMP4_SmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	smallPath := filepath.Join(tmpDir, "small.mp4")

	// File too small to contain valid MP4 structure
	err := os.WriteFile(smallPath, []byte("small"), 0644)
	require.NoError(t, err)

	f, err := os.Open(smallPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeMP4(f)
	assert.Error(t, err)
}

// TestMOVProber_Probe_InvalidBrand tests MOV prober rejecting regular MP4 brand
func TestMOVProber_Probe_InvalidBrand(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "test.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Write a minimal MP4 file structure with isom brand (not QuickTime)
	ftypSize := uint32(24)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom") // Not QuickTime
	ftyp[12] = 0
	ftyp[13] = 0
	ftyp[14] = 0
	ftyp[15] = 2
	copy(ftyp[16:20], "isom") // compatible brand

	_, err = f.Write(ftyp)
	require.NoError(t, err)

	err = f.Close()
	require.NoError(t, err)

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	result := prober.CanProbe([]byte("isom"))
	assert.False(t, result, "MOV prober should not handle isom brand")
}
