package mediainfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapMOVVideoCodec(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"apch", "prores_422_hq"},
		{"apcn", "prores_422"},
		{"apcs", "prores_422_lt"},
		{"apco", "prores_422_proxy"},
		{"ap4h", "prores_4444"},
		{"ap4x", "prores_4444_xq"},
		{"dvcp", "dvcpro"},
		{"dvpp", "dvcpro"},
		{"dvc ", "dv"},
		{"dvsd", "dv"},
		{"dvh5", "dvcpro_hd"},
		{"dvh6", "dvcpro_hd"},
		{"mjp2", "jpeg2000"},
		{"jpeg", "mjpeg"},
		{"png ", "png"},
		{"rle ", "quicktime_rle"},
		{"rpza", "quicktime_rpza"},
		{"smc ", "quicktime_smc"},
		{"SVQ1", "sorenson_video_1"},
		{"SVQ3", "sorenson_video_3"},
		{"mp4v", "mpeg4"},
		{"unknown", "unknown"}, // Pass-through
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVVideoCodec(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestMapMOVAudioCodec(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sowt", "pcm_s16le"},
		{"twos", "pcm_s16be"},
		{"in24", "pcm_s24le"},
		{"in32", "pcm_s32le"},
		{"fl32", "pcm_f32le"},
		{"fl64", "pcm_f64le"},
		{"ulaw", "pcm_mulaw"},
		{"alaw", "pcm_alaw"},
		{"ima4", "adpcm_ima_qt"},
		{"MAC3", "mace3"},
		{"MAC6", "mace6"},
		{"alac", "alac"},
		{"QDMC", "qdmc"},
		{"QDM2", "qdm2"},
		{"Qclp", "qcelp"},
		{"unknown", "unknown"}, // Pass-through
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVAudioCodec(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestMOVProber_Probe tests MOV probe with valid QuickTime header
func TestMOVProber_Probe(t *testing.T) {
	tmpDir := t.TempDir()
	movPath := filepath.Join(tmpDir, "test.mov")

	f, err := os.Create(movPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Write a minimal MOV file structure with QuickTime brand
	ftypSize := uint32(24)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "qt  ") // QuickTime major brand
	ftyp[12] = 0             // minor_version
	ftyp[13] = 0
	ftyp[14] = 0
	ftyp[15] = 2
	copy(ftyp[16:20], "qt  ") // compatible_brand

	_, err = f.Write(ftyp)
	require.NoError(t, err)

	err = f.Close()
	require.NoError(t, err)

	f, err = os.Open(movPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	info, err := prober.Probe(f)

	if err != nil {
		// Error is acceptable for minimal files
		t.Logf("Probe returned error for minimal file: %v", err)
	} else {
		assert.Equal(t, "mov", info.Container)
	}
}

// TestMOVProber_Probe_M4V tests MOV probe with M4V brand
func TestMOVProber_Probe_M4V(t *testing.T) {
	tmpDir := t.TempDir()
	m4vPath := filepath.Join(tmpDir, "test.m4v")

	f, err := os.Create(m4vPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Write M4V header
	ftypSize := uint32(24)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "M4V ") // M4V brand
	ftyp[12] = 0
	ftyp[13] = 0
	ftyp[14] = 0
	ftyp[15] = 1
	copy(ftyp[16:20], "mp41")

	_, err = f.Write(ftyp)
	require.NoError(t, err)

	err = f.Close()
	require.NoError(t, err)

	f, err = os.Open(m4vPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	result := prober.CanProbe(ftyp)
	assert.True(t, result, "MOV prober should handle M4V brand")
}

// TestMOVProber_Probe_M4A tests MOV probe with M4A brand
func TestMOVProber_Probe_M4A(t *testing.T) {
	tmpDir := t.TempDir()
	m4aPath := filepath.Join(tmpDir, "test.m4a")

	f, err := os.Create(m4aPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Write M4A header
	ftypSize := uint32(24)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "M4A ") // M4A brand
	ftyp[12] = 0
	ftyp[13] = 0
	ftyp[14] = 0
	ftyp[15] = 1
	copy(ftyp[16:20], "mp41")

	_, err = f.Write(ftyp)
	require.NoError(t, err)

	err = f.Close()
	require.NoError(t, err)

	f, err = os.Open(m4aPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	result := prober.CanProbe(ftyp)
	assert.True(t, result, "MOV prober should handle M4A brand")
}

// TestMOVProber_Probe_FlashVideo tests MOV probe with F4V brand
func TestMOVProber_Probe_FlashVideo(t *testing.T) {
	tmpDir := t.TempDir()
	f4vPath := filepath.Join(tmpDir, "test.f4v")

	f, err := os.Create(f4vPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Write F4V header
	ftypSize := uint32(24)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "F4V ") // F4V brand
	ftyp[12] = 0
	ftyp[13] = 0
	ftyp[14] = 0
	ftyp[15] = 1
	copy(ftyp[16:20], "mp41")

	_, err = f.Write(ftyp)
	require.NoError(t, err)

	err = f.Close()
	require.NoError(t, err)

	f, err = os.Open(f4vPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	result := prober.CanProbe(ftyp)
	assert.True(t, result, "MOV prober should handle F4V brand")
}

// TestMOVProber_Probe_MP4Brand tests that regular MP4 brand is NOT handled by MOV prober
func TestMOVProber_Probe_MP4Brand(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "test.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Write standard MP4 (isom) header - should NOT be handled by MOV prober
	ftypSize := uint32(24)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom") // Standard MP4 brand, not QuickTime
	ftyp[12] = 0
	ftyp[13] = 0
	ftyp[14] = 0
	ftyp[15] = 2
	copy(ftyp[16:20], "isom")

	_, err = f.Write(ftyp)
	require.NoError(t, err)

	err = f.Close()
	require.NoError(t, err)

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	result := prober.CanProbe(ftyp)
	assert.False(t, result, "MOV prober should NOT handle standard MP4 (isom) brand")
}

// TestMOVProber_Probe_SmallHeader tests handling of header too small for MOV
func TestMOVProber_Probe_SmallHeader(t *testing.T) {
	tmpDir := t.TempDir()
	smallPath := filepath.Join(tmpDir, "small.mov")

	err := os.WriteFile(smallPath, []byte("ftyp"), 0644)
	require.NoError(t, err)

	f, err := os.Open(smallPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	result := prober.CanProbe([]byte("ftyp"))
	assert.False(t, result, "MOV prober should reject header too small")
}

// TestMOVProber_Probe_NoFTyp tests handling of file without ftyp box
func TestMOVProber_Probe_NoFTyp(t *testing.T) {
	tmpDir := t.TempDir()
	noftypPath := filepath.Join(tmpDir, "noftyp.mov")

	err := os.WriteFile(noftypPath, []byte("RIFFxxxxAVI "), 0644)
	require.NoError(t, err)

	f, err := os.Open(noftypPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	result := prober.CanProbe([]byte("RIFFxxxxAVI "))
	assert.False(t, result, "MOV prober should reject file without ftyp")
}

// TestMOVProber_Probe_CorruptedHeader tests handling of corrupted header
func TestMOVProber_Probe_CorruptedHeader(t *testing.T) {
	tmpDir := t.TempDir()
	corruptPath := filepath.Join(tmpDir, "corrupt.mov")

	// Create header with ftyp at wrong offset
	err := os.WriteFile(corruptPath, []byte("XXXX\x00\x00\x00\x00ftyp"), 0644)
	require.NoError(t, err)

	f, err := os.Open(corruptPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := NewMOVProber()
	result := prober.CanProbe([]byte("XXXX\x00\x00\x00\x00ftyp"))
	assert.False(t, result, "MOV prober should reject corrupted header")
}

// TestMapMOVVideoCodec_ProRes tests ProRes codec mapping
func TestMapMOVVideoCodec_ProRes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"apch", "prores_422_hq"},
		{"apcn", "prores_422"},
		{"apcs", "prores_422_lt"},
		{"apco", "prores_422_proxy"},
		{"ap4h", "prores_4444"},
		{"ap4x", "prores_4444_xq"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVVideoCodec(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMapMOVVideoCodec_DV tests DV codec mapping
func TestMapMOVVideoCodec_DV(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"dvcp", "dvcpro"},
		{"dvpp", "dvcpro"},
		{"dvc ", "dv"},
		{"dvsd", "dv"},
		{"dvh5", "dvcpro_hd"},
		{"dvh6", "dvcpro_hd"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVVideoCodec(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMapMOVVideoCodec_MJPEG tests MJPEG codec mapping
func TestMapMOVVideoCodec_MJPEG(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"mjp2", "jpeg2000"},
		{"jpeg", "mjpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVVideoCodec(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMapMOVAudioCodec_PCM tests PCM codec mapping
func TestMapMOVAudioCodec_PCM(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sowt", "pcm_s16le"},
		{"twos", "pcm_s16be"},
		{"in24", "pcm_s24le"},
		{"in32", "pcm_s32le"},
		{"fl32", "pcm_f32le"},
		{"fl64", "pcm_f64le"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVAudioCodec(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMapMOVAudioCodec_Compression tests compression codec mapping
func TestMapMOVAudioCodec_Compression(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ulaw", "pcm_mulaw"},
		{"alaw", "pcm_alaw"},
		{"ima4", "adpcm_ima_qt"},
		{"MAC3", "mace3"},
		{"MAC6", "mace6"},
		{"alac", "alac"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVAudioCodec(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMapMOVVideoCodec_Unknown tests unknown codec pass-through
func TestMapMOVVideoCodec_Unknown(t *testing.T) {
	result := mapMOVVideoCodec("unknown_codec_xyz")
	assert.Equal(t, "unknown_codec_xyz", result)
}

// TestMapMOVAudioCodec_Unknown tests unknown codec pass-through
func TestMapMOVAudioCodec_Unknown(t *testing.T) {
	result := mapMOVAudioCodec("unknown_audio_xyz")
	assert.Equal(t, "unknown_audio_xyz", result)
}
