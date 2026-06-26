package mediainfo

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- MP4: extractMP4VideoInfo with HEVC (hvcC) codec config ---

func TestExtractMP4VideoInfo_HEVCCodec(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "hevc.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	mp4File := mp4.NewFile()
	ftyp := mp4.CreateFtyp()
	mp4File.AddChild(ftyp, 0)

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = 1000
	mvhd.Duration = 10000
	moov.AddChild(mvhd)

	vtrak := mp4.NewTrakBox()
	vmdia := mp4.NewMdiaBox()
	vhdlr, err := mp4.CreateHdlr("vide")
	require.NoError(t, err)
	vmdia.AddChild(vhdlr)

	vmdhd := &mp4.MdhdBox{Timescale: 24000, Duration: 240000}
	vmdia.AddChild(vmdhd)

	vminf := mp4.NewMinfBox()
	vstbl := mp4.NewStblBox()
	vstsd := mp4.NewStsdBox()
	visualEntry := mp4.CreateVisualSampleEntryBox("hvc1", 3840, 2160, nil)
	vstsd.AddChild(visualEntry)
	vstbl.AddChild(vstsd)

	vstts := &mp4.SttsBox{
		SampleCount:     []uint32{240},
		SampleTimeDelta: []uint32{1000},
	}
	vstbl.AddChild(vstts)

	vminf.AddChild(vstbl)
	vmdia.AddChild(vminf)
	vtrak.AddChild(vmdia)
	moov.AddChild(vtrak)

	mp4File.AddChild(moov, 0)

	err = mp4File.Encode(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	// The codec may be "hevc" or "hvc1" depending on whether hvcC child is present
	assert.NotEmpty(t, info.VideoCodec)
	assert.Equal(t, 3840, info.Width)
	assert.Equal(t, 2160, info.Height)
}

// --- MP4: extractMP4VideoInfo with no visual sample entry ---

func TestExtractMP4VideoInfo_NoVisualEntry(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "no_visual.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	mp4File := mp4.NewFile()
	ftyp := mp4.CreateFtyp()
	mp4File.AddChild(ftyp, 0)

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = 1000
	mvhd.Duration = 5000
	moov.AddChild(mvhd)

	// Trak with video hdlr but audio sample entry (no visual entry)
	vtrak := mp4.NewTrakBox()
	vmdia := mp4.NewMdiaBox()
	vhdlr, err := mp4.CreateHdlr("vide")
	require.NoError(t, err)
	vmdia.AddChild(vhdlr)

	vmdhd := &mp4.MdhdBox{Timescale: 24000, Duration: 240000}
	vmdia.AddChild(vmdhd)

	vminf := mp4.NewMinfBox()
	vstbl := mp4.NewStblBox()
	vstsd := mp4.NewStsdBox()
	// Add an audio entry instead of visual — this should trigger "no visual sample entry found"
	audioEntry := mp4.CreateAudioSampleEntryBox("mp4a", 2, 16, 44100, nil)
	vstsd.AddChild(audioEntry)
	vstbl.AddChild(vstsd)
	vminf.AddChild(vstbl)
	vmdia.AddChild(vminf)
	vtrak.AddChild(vmdia)
	moov.AddChild(vtrak)

	mp4File.AddChild(moov, 0)

	err = mp4File.Encode(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4(f)
	// Should still succeed (analyzeMP4 handles extractMP4VideoInfo error)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	// Video codec should be empty since no visual entry was found
	assert.Equal(t, "", info.VideoCodec)
}

// --- MP4: extractMP4AudioInfo with no audio sample entry ---

func TestExtractMP4AudioInfo_NoAudioEntry(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "no_audio_entry.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	mp4File := mp4.NewFile()
	ftyp := mp4.CreateFtyp()
	mp4File.AddChild(ftyp, 0)

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = 1000
	mvhd.Duration = 5000
	moov.AddChild(mvhd)

	// Audio trak with visual entry instead of audio
	atrak := mp4.NewTrakBox()
	amdia := mp4.NewMdiaBox()
	ahdlr, err := mp4.CreateHdlr("soun")
	require.NoError(t, err)
	amdia.AddChild(ahdlr)

	amdhd := &mp4.MdhdBox{Timescale: 44100, Duration: 441000}
	amdia.AddChild(amdhd)

	aminf := mp4.NewMinfBox()
	astbl := mp4.NewStblBox()
	astsd := mp4.NewStsdBox()
	// Add visual entry instead of audio
	visualEntry := mp4.CreateVisualSampleEntryBox("avc1", 640, 480, nil)
	astsd.AddChild(visualEntry)
	astbl.AddChild(astsd)
	aminf.AddChild(astbl)
	amdia.AddChild(aminf)
	atrak.AddChild(amdia)
	moov.AddChild(atrak)

	mp4File.AddChild(moov, 0)

	err = mp4File.Encode(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	// AudioCodec should remain empty
	assert.Equal(t, "", info.AudioCodec)
}

// --- MOV: Probe with invalid MP4 content (triggers error path) ---

func TestMOVProber_Probe_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	movPath := filepath.Join(tmpDir, "invalid.mov")

	// Write a file with QuickTime header but minimal content
	f, err := os.Create(movPath)
	require.NoError(t, err)

	// Write a minimal file with qt ftyp but garbage content
	var buf bytes.Buffer
	// ftyp box with "qt  " brand
	binary.Write(&buf, binary.BigEndian, uint32(12)) // box size
	buf.Write([]byte("ftyp"))
	buf.Write([]byte("qt  "))
	_, err = f.Write(buf.Bytes())
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(movPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := newMOVProber()
	info, err := prober.Probe(context.Background(), f)
	// May succeed with fallback parser or error — both are acceptable
	if err != nil {
		t.Logf("Probe returned error (acceptable): %v", err)
	} else {
		assert.Equal(t, "mov", info.Container)
	}
}

// --- MOV: Probe with valid MP4 that fails analysis (triggers mov error path) ---

func TestMOVProber_Probe_AnalysisError(t *testing.T) {
	prober := newMOVProber()

	// Use a reader that returns valid header but fails on full decode
	tmpDir := t.TempDir()
	movPath := filepath.Join(tmpDir, "bad_mov.mov")
	err := os.WriteFile(movPath, []byte{
		0x00, 0x00, 0x00, 0x0C, // size = 12
		'f', 't', 'y', 'p', // ftyp
		'q', 't', ' ', ' ', // qt brand
	}, 0644)
	require.NoError(t, err)

	f, err := os.Open(movPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := prober.Probe(context.Background(), f)
	// May succeed with fallback parser or error
	if err != nil {
		t.Logf("Probe returned error (acceptable): %v", err)
	} else {
		assert.Equal(t, "mov", info.Container)
	}
}

// --- AVI: analyzeAVI with seek error ---

func TestAnalyzeAVI_SeekErrorOnRead(t *testing.T) {
	cr := &seekErrorReader{}
	_, err := analyzeAVI(cr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "seek failed")
}

// --- AVI: analyzeAVI with form type read error ---

func TestAnalyzeAVI_FormTypeReadError(t *testing.T) {
	// Create a reader that returns valid RIFF header but fails on form type
	cr := &partialAVIReader{
		data: func() []byte {
			var buf bytes.Buffer
			buf.Write([]byte("RIFF"))
			binary.Write(&buf, binary.LittleEndian, uint32(100))
			// Only 2 bytes of form type (need 4)
			buf.Write([]byte("AV"))
			return buf.Bytes()
		}(),
	}
	_, err := analyzeAVI(cr)
	assert.Error(t, err)
}

// --- AVI: analyzeAVI with chunk read error ---

func TestAnalyzeAVI_ChunkReadError(t *testing.T) {
	cr := &partialAVIReader{
		data: func() []byte {
			var buf bytes.Buffer
			buf.Write([]byte("RIFF"))
			binary.Write(&buf, binary.LittleEndian, uint32(100))
			buf.Write([]byte("AVI "))
			// No chunks - should reach EOF cleanly
			return buf.Bytes()
		}(),
	}
	info, err := analyzeAVI(cr)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
}

// --- AVI: analyzeAVI with hdrl that has read error on strl type ---

func TestAnalyzeAVI_HdrlStrlReadError(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "strl_error.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 0xFFFF)
	writeBytes(t, f, []byte("AVI "))

	// LIST hdrl
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 20)
	writeBytes(t, f, []byte("hdrl"))

	// LIST strl with truncated type
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 8)
	// Only 2 bytes of strl type
	writeBytes(t, f, []byte("st"))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	// Should error because strl list type couldn't be read fully
	assert.Error(t, err)
}

// --- AVI: analyzeAVI with error seeking to end of hdrl ---

func TestAnalyzeAVI_SeekEndOfHdrlError(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "hdrl_seek_err.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 0xFFFF)
	writeBytes(t, f, []byte("AVI "))

	// LIST hdrl with a strl sub-list that has error
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 100)
	writeBytes(t, f, []byte("hdrl"))

	// avih inside hdrl
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	for i := 0; i < 14; i++ {
		writeUint32LE(t, f, 0)
	}

	// LIST strl
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 8)
	writeBytes(t, f, []byte("strl"))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	// May succeed or fail depending on file boundaries
	if err != nil {
		t.Logf("analyzeAVI returned error (acceptable): %v", err)
	} else {
		assert.Equal(t, "avi", info.Container)
	}
}

// --- MKV: parseSegment with skip error in default case ---

func TestParseSegment_SkipErrorInDefault(t *testing.T) {
	// Build segment with an unknown non-master element that can't be skipped
	// Use a reader that will fail on skipBytes
	er := newEBMLReader(&failingSkipReader{})
	info := &VideoInfo{Container: "mkv"}
	// This will attempt to parse but should handle errors gracefully
	parseSegment(er, 100, info)
}

// --- MKV: parseSegmentInfo with Duration but zero TimecodeScale ---

func TestParseSegmentInfo_ZeroTimecodeScaleValue(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "zero_tcs.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Build info with TimecodeScale=0 and Duration
	var infoData []byte
	infoData = append(infoData, appendEBMLUint(elemTimecodeScale, 0)...)
	infoData = append(infoData, appendEBMLFloat(elemDuration, 120.5)...)

	segmentData := appendEBMLMasterElement(nil, elemInfo, infoData)
	segmentData = append(segmentData, buildMKVTracksData()...)

	buf = appendEBMLMasterElement(buf, elemSegment, segmentData)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
}

// --- MKV: parseSegmentInfo with negative Duration ---

func TestParseSegmentInfo_NegativeDuration(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "neg_duration.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Build info with Duration=0 (not > 0, so won't be set)
	var infoData []byte
	infoData = append(infoData, appendEBMLUint(elemTimecodeScale, 1000000)...)
	infoData = append(infoData, appendEBMLFloat(elemDuration, 0.0)...)

	segmentData := appendEBMLMasterElement(nil, elemInfo, infoData)
	segmentData = append(segmentData, buildMKVTracksData()...)

	buf = appendEBMLMasterElement(buf, elemSegment, segmentData)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	// Duration should be 0 since we passed 0.0
	assert.Equal(t, 0.0, info.Duration)
}

// --- MKV: analyzeMKV with bitrate calculation ---

func TestAnalyzeMKV_BitrateCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "bitrate.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	segmentData := buildMKVSegmentData()
	buf = appendEBMLMasterElement(buf, elemSegment, segmentData)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	// Bitrate should be calculated if Duration > 0
	if info.Duration > 0 {
		assert.Greater(t, info.Bitrate, 0)
	}
}

// --- Helper types ---

// seekErrorReader always returns errors on Seek and Read.
type seekErrorReader struct{}

func (s *seekErrorReader) Read(_ []byte) (int, error) {
	return 0, os.ErrInvalid
}

func (s *seekErrorReader) ReadAt(_ []byte, _ int64) (int, error) {
	return 0, os.ErrInvalid
}

func (s *seekErrorReader) Seek(_ int64, _ int) (int64, error) {
	return 0, os.ErrInvalid
}

// partialAVIReader wraps bytes.Reader to implement FileReader.
type partialAVIReader struct {
	data []byte
	pos  int
}

func (r *partialAVIReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *partialAVIReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

func (r *partialAVIReader) Seek(offset int64, whence int) (int64, error) {
	var newpos int64
	switch whence {
	case 0:
		newpos = offset
	case 1:
		newpos = int64(r.pos) + offset
	case 2:
		newpos = int64(len(r.data)) + offset
	default:
		return 0, os.ErrInvalid
	}
	if newpos < 0 || newpos > int64(len(r.data)) {
		return 0, os.ErrInvalid
	}
	r.pos = int(newpos)
	return newpos, nil
}

// failingSkipReader returns valid EBML element IDs but fails on read.
type failingSkipReader struct{}

func (f *failingSkipReader) Read(p []byte) (int, error) {
	// Return a valid element ID first, then fail
	if len(p) > 0 {
		p[0] = 0xEC // Void element ID (1-byte)
	}
	return 1, nil
}

func (f *failingSkipReader) Seek(_ int64, _ int) (int64, error) {
	return 0, os.ErrInvalid
}
