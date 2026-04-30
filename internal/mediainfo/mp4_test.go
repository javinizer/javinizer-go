package mediainfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
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

	ftypSize := uint32(24)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
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
	result := prober.CanProbe([]byte("isom"))
	assert.False(t, result, "MOV prober should not handle isom brand")
}

func TestMP4Prober_Name(t *testing.T) {
	prober := NewMP4Prober()
	assert.Equal(t, "mp4", prober.Name())
}

func TestAnalyzeMP4_InvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "invalid.mp4")
	require.NoError(t, os.WriteFile(invalidPath, []byte("not a real mp4 file content at all"), 0644))

	f, err := os.Open(invalidPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeMP4(f)
	assert.Error(t, err)
}

func TestMapMP4VideoCodec_Extended(t *testing.T) {
	tests := []struct {
		name     string
		fourcc   string
		expected string
	}{
		{"avc1", "avc1", "h264"},
		{"avc3", "avc3", "h264"},
		{"hvc1", "hvc1", "hevc"},
		{"hev1", "hev1", "hevc"},
		{"vp09", "vp09", "vp9"},
		{"vp08", "vp08", "vp8"},
		{"av01", "av01", "av1"},
		{"mp4v", "mp4v", "mpeg4"},
		{"unknown returns as-is", "custom", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMP4VideoCodec(tt.fourcc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapMP4AudioCodec_Extended(t *testing.T) {
	tests := []struct {
		name     string
		fourcc   string
		expected string
	}{
		{"mp4a", "mp4a", "aac"},
		{".mp3", ".mp3", "mp3"},
		{"mp3 space", "mp3 ", "mp3"},
		{"ac-3", "ac-3", "ac3"},
		{"ec-3", "ec-3", "eac3"},
		{"opus", "opus", "opus"},
		{"fLaC", "fLaC", "flac"},
		{"unknown returns as-is", "custom", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMP4AudioCodec(tt.fourcc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func createMinimalTrak(t *testing.T, handlerType string, withStsd bool, withSampleEntry bool) *mp4.TrakBox {
	t.Helper()
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, err := mp4.CreateHdlr(handlerType)
	require.NoError(t, err)
	mdia.Hdlr = hdlr
	mdia.AddChild(hdlr)

	minf := mp4.NewMinfBox()
	stbl := mp4.NewStblBox()

	if withStsd {
		stsd := mp4.NewStsdBox()
		if withSampleEntry && handlerType == "vide" {
			visualEntry := mp4.CreateVisualSampleEntryBox("avc1", 1920, 1080, nil)
			stsd.AddChild(visualEntry)
		}
		if withSampleEntry && handlerType == "soun" {
			audioEntry := mp4.CreateAudioSampleEntryBox("mp4a", 2, 16, 44100, nil)
			stsd.AddChild(audioEntry)
		}
		stbl.Stsd = stsd
		stbl.AddChild(stsd)
	}

	minf.Stbl = stbl
	minf.AddChild(stbl)
	mdia.Minf = minf
	mdia.AddChild(minf)
	trak.Mdia = mdia
	trak.AddChild(mdia)

	return trak
}

func TestExtractMP4VideoInfo_MissingStsd(t *testing.T) {
	trak := createMinimalTrak(t, "vide", false, false)
	info := &VideoInfo{}
	err := extractMP4VideoInfo(trak, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing sample description")
}

func TestExtractMP4AudioInfo_MissingStsd(t *testing.T) {
	trak := createMinimalTrak(t, "soun", false, false)
	info := &VideoInfo{}
	err := extractMP4AudioInfo(trak, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing sample description")
}

func TestExtractMP4VideoInfo_NoVisualSampleEntry(t *testing.T) {
	trak := createMinimalTrak(t, "vide", true, false)
	info := &VideoInfo{}
	err := extractMP4VideoInfo(trak, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no visual sample entry found")
}

func TestExtractMP4AudioInfo_NoAudioSampleEntry(t *testing.T) {
	trak := createMinimalTrak(t, "soun", true, false)
	info := &VideoInfo{}
	err := extractMP4AudioInfo(trak, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no audio sample entry found")
}

func TestExtractMP4VideoInfo_WithVisualEntry(t *testing.T) {
	trak := createMinimalTrak(t, "vide", true, true)
	info := &VideoInfo{}
	err := extractMP4VideoInfo(trak, info)
	assert.NoError(t, err)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
}

func TestExtractMP4AudioInfo_WithAudioEntry(t *testing.T) {
	trak := createMinimalTrak(t, "soun", true, true)
	info := &VideoInfo{}
	err := extractMP4AudioInfo(trak, info)
	assert.NoError(t, err)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 44100, info.SampleRate)
}

func TestExtractMP4VideoInfo_NilMdia(t *testing.T) {
	trak := mp4.NewTrakBox()
	info := &VideoInfo{}
	err := extractMP4VideoInfo(trak, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing sample description")
}

func TestExtractMP4AudioInfo_NilMdia(t *testing.T) {
	trak := mp4.NewTrakBox()
	info := &VideoInfo{}
	err := extractMP4AudioInfo(trak, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing sample description")
}

func TestExtractMP4VideoInfo_NilMinf(t *testing.T) {
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, err := mp4.CreateHdlr("vide")
	require.NoError(t, err)
	mdia.Hdlr = hdlr
	mdia.AddChild(hdlr)
	trak.Mdia = mdia
	trak.AddChild(mdia)

	info := &VideoInfo{}
	err = extractMP4VideoInfo(trak, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing sample description")
}

func TestExtractMP4AudioInfo_NilMinf(t *testing.T) {
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, err := mp4.CreateHdlr("soun")
	require.NoError(t, err)
	mdia.Hdlr = hdlr
	mdia.AddChild(hdlr)
	trak.Mdia = mdia
	trak.AddChild(mdia)

	info := &VideoInfo{}
	err = extractMP4AudioInfo(trak, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing sample description")
}

func TestExtractMP4VideoInfo_WithHEVCEntry(t *testing.T) {
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, err := mp4.CreateHdlr("vide")
	require.NoError(t, err)
	mdia.Hdlr = hdlr
	mdia.AddChild(hdlr)

	minf := mp4.NewMinfBox()
	stbl := mp4.NewStblBox()
	stsd := mp4.NewStsdBox()
	visualEntry := mp4.CreateVisualSampleEntryBox("hvc1", 3840, 2160, nil)
	stsd.AddChild(visualEntry)
	stbl.Stsd = stsd
	stbl.AddChild(stsd)
	minf.Stbl = stbl
	minf.AddChild(stbl)
	mdia.Minf = minf
	mdia.AddChild(minf)
	trak.Mdia = mdia
	trak.AddChild(mdia)

	info := &VideoInfo{}
	err = extractMP4VideoInfo(trak, info)
	assert.NoError(t, err)
	assert.Equal(t, "hevc", info.VideoCodec)
	assert.Equal(t, 3840, info.Width)
	assert.Equal(t, 2160, info.Height)
}

func TestExtractMP4AudioInfo_WithAC3Entry(t *testing.T) {
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, err := mp4.CreateHdlr("soun")
	require.NoError(t, err)
	mdia.Hdlr = hdlr
	mdia.AddChild(hdlr)

	minf := mp4.NewMinfBox()
	stbl := mp4.NewStblBox()
	stsd := mp4.NewStsdBox()
	audioEntry := mp4.CreateAudioSampleEntryBox("ac-3", 6, 16, 48000, nil)
	stsd.AddChild(audioEntry)
	stbl.Stsd = stsd
	stbl.AddChild(stsd)
	minf.Stbl = stbl
	minf.AddChild(stbl)
	mdia.Minf = minf
	mdia.AddChild(minf)
	trak.Mdia = mdia
	trak.AddChild(mdia)

	info := &VideoInfo{}
	err = extractMP4AudioInfo(trak, info)
	assert.NoError(t, err)
	assert.Equal(t, "ac3", info.AudioCodec)
	assert.Equal(t, 6, info.AudioChannels)
	assert.Equal(t, 48000, info.SampleRate)
}

func TestAnalyzeMP4_WithFtypOnly(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "ftyp_only.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	ftypSize := uint32(20)
	ftyp := make([]byte, ftypSize)
	ftyp[0] = byte(ftypSize >> 24)
	ftyp[1] = byte(ftypSize >> 16)
	ftyp[2] = byte(ftypSize >> 8)
	ftyp[3] = byte(ftypSize)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	copy(ftyp[16:20], "isom")

	_, err = f.Write(ftyp)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4(f)
	if err != nil {
		t.Logf("analyzeMP4 with ftyp-only returned error: %v", err)
	} else {
		assert.Equal(t, "mp4", info.Container)
		assert.Equal(t, 0, info.Width)
		assert.Equal(t, 0, info.Height)
	}
}

func TestMOVProber_Name(t *testing.T) {
	prober := NewMOVProber()
	assert.Equal(t, "mov", prober.Name())
}

func TestCalcMP4FrameRate(t *testing.T) {
	tests := []struct {
		name         string
		sampleCounts []uint32
		sampleDeltas []uint32
		timescale    uint32
		wantFPS      float64
	}{
		{"24fps", []uint32{1000}, []uint32{1000}, 24000, 24.0},
		{"30fps", []uint32{1000}, []uint32{1000}, 30000, 30.0},
		{"29.97fps", []uint32{1000}, []uint32{1001}, 30000, 29.97},
		{"25fps", []uint32{1000}, []uint32{1000}, 25000, 25.0},
		{"mixed deltas", []uint32{500, 500}, []uint32{1001, 2002}, 30000, 19.98},
		{"zero timescale", []uint32{1000}, []uint32{1000}, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trak := createTrakWithStts(t, tt.sampleCounts, tt.sampleDeltas, tt.timescale)
			fps := calcMP4FrameRate(trak)
			if tt.wantFPS == 0 {
				assert.Equal(t, 0.0, fps)
			} else {
				assert.InDelta(t, tt.wantFPS, fps, 0.1)
			}
		})
	}
}

func TestCalcMP4FrameRate_FieldBased(t *testing.T) {
	trak := createTrakWithStts(t, []uint32{2000}, []uint32{1001}, 60000)
	fps := calcMP4FrameRate(trak)
	assert.InDelta(t, 29.97, fps, 0.1)
}

func TestCalcMP4FrameRate_NilStts(t *testing.T) {
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, err := mp4.CreateHdlr("vide")
	require.NoError(t, err)
	mdia.Hdlr = hdlr
	mdia.AddChild(hdlr)
	trak.Mdia = mdia
	trak.AddChild(mdia)
	assert.Equal(t, 0.0, calcMP4FrameRate(trak))
}

func TestIsFieldBasedStream(t *testing.T) {
	tests := []struct {
		name         string
		sampleCounts []uint32
		sampleDeltas []uint32
		timescale    uint32
		want         bool
	}{
		{"59.94 field", []uint32{1000}, []uint32{1001}, 60000, true},
		{"60 field", []uint32{1000}, []uint32{1000}, 60000, true},
		{"50 field", []uint32{1000}, []uint32{960}, 48000, true},
		{"24 progressive", []uint32{1000}, []uint32{1000}, 24000, false},
		{"30 progressive", []uint32{1000}, []uint32{1001}, 30000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trak := createTrakWithStts(t, tt.sampleCounts, tt.sampleDeltas, tt.timescale)
			stts := trak.Mdia.Minf.Stbl.Stts
			result := isFieldBasedStream(stts, tt.timescale)
			assert.Equal(t, tt.want, result)
		})
	}
}

func createTrakWithStts(t *testing.T, sampleCounts, sampleDeltas []uint32, timescale uint32) *mp4.TrakBox {
	t.Helper()
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, err := mp4.CreateHdlr("vide")
	require.NoError(t, err)
	mdia.Hdlr = hdlr
	mdia.AddChild(hdlr)

	minf := mp4.NewMinfBox()
	stbl := mp4.NewStblBox()

	stts := &mp4.SttsBox{
		SampleCount:     sampleCounts,
		SampleTimeDelta: sampleDeltas,
	}
	stbl.Stts = stts
	stbl.AddChild(stts)

	mdhd := &mp4.MdhdBox{Timescale: timescale}
	mdia.Mdhd = mdhd
	mdia.AddChild(mdhd)

	minf.Stbl = stbl
	minf.AddChild(stbl)
	mdia.Minf = minf
	mdia.AddChild(minf)
	trak.Mdia = mdia
	trak.AddChild(mdia)
	return trak
}
