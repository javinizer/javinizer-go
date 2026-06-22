package mediainfo

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- analyzeMP4: build a real MP4 with moov/traks to exercise the mp4ff path ---

func TestAnalyzeMP4_WithFullMoov(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "full_moov.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	// Build a minimal MP4 file using mp4ff library
	mp4File := mp4.NewFile()
	ftyp := mp4.CreateFtyp()
	mp4File.AddChild(ftyp, 0)

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = 1000
	mvhd.Duration = 10000 // 10 seconds
	moov.AddChild(mvhd)

	// Video track
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
	visualEntry := mp4.CreateVisualSampleEntryBox("avc1", 1920, 1080, nil)
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

	// Audio track
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
	audioEntry := mp4.CreateAudioSampleEntryBox("mp4a", 2, 16, 44100, nil)
	astsd.AddChild(audioEntry)
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
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 44100, info.SampleRate)
	assert.Greater(t, info.Duration, 0.0)
	if info.Duration > 0 && info.Bitrate > 0 {
		assert.Greater(t, info.Bitrate, 0)
	}
	if info.Width > 0 && info.Height > 0 {
		assert.InDelta(t, 16.0/9.0, info.AspectRatio, 0.01)
	}
}

func TestAnalyzeMP4_WithMoovNoMvhd(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "no_mvhd.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	mp4File := mp4.NewFile()
	ftyp := mp4.CreateFtyp()
	mp4File.AddChild(ftyp, 0)

	moov := mp4.NewMoovBox()
	// No mvhd — should still work, duration falls back to track level
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
	visualEntry := mp4.CreateVisualSampleEntryBox("avc1", 1280, 720, nil)
	vstsd.AddChild(visualEntry)
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
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
}

func TestAnalyzeMP4_TrakWithoutHdlr(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "no_hdlr.mp4")

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

	// Trak with nil Hdlr — should be skipped
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	// No hdlr added
	mdhd := &mp4.MdhdBox{Timescale: 24000, Duration: 240000}
	mdia.AddChild(mdhd)
	trak.AddChild(mdia)
	moov.AddChild(trak)

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
	// No video/audio extracted from trak without hdlr
	assert.Equal(t, 0, info.Width)
	assert.Equal(t, "", info.VideoCodec)
}

func TestAnalyzeMP4_TrakWithNilMdhd(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "nil_mdhd.mp4")

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

	vtrak := mp4.NewTrakBox()
	vmdia := mp4.NewMdiaBox()
	vhdlr, err := mp4.CreateHdlr("vide")
	require.NoError(t, err)
	vmdia.AddChild(vhdlr)
	// No mdhd — Duration and FrameRate should remain 0
	vminf := mp4.NewMinfBox()
	vstbl := mp4.NewStblBox()
	vstsd := mp4.NewStsdBox()
	visualEntry := mp4.CreateVisualSampleEntryBox("avc1", 640, 480, nil)
	vstsd.AddChild(visualEntry)
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
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 640, info.Width)
	assert.Equal(t, 480, info.Height)
}

func TestAnalyzeMP4_AudioOnlyTrack(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "audio_only.mp4")

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
	audioEntry := mp4.CreateAudioSampleEntryBox("mp4a", 2, 16, 44100, nil)
	astsd.AddChild(audioEntry)
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
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
}

// --- parseSegment: build a valid MKV binary with Segment → Info + Tracks ---

func TestParseSegment_WithInfoAndTracks_Full(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "segment_full.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	// Build a minimal MKV file with EBML header + Segment containing Info + Tracks
	var buf []byte

	// EBML header
	buf = appendEBMLHeader(buf)

	// Segment element
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
	if info.Duration > 0 {
		assert.Greater(t, info.Duration, 0.0)
	}
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 44100, info.SampleRate)
	assert.Equal(t, 2, info.AudioChannels)
}

func TestParseSegment_WithClusterSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "segment_cluster.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Segment containing Info + Cluster (should be skipped) + Tracks
	segmentData := buildMKVSegmentInfoData()

	// Add a Cluster element that should be skipped
	clusterData := make([]byte, 32)
	for i := range clusterData {
		clusterData[i] = 0xFF
	}
	segmentData = appendEBMLMasterElement(segmentData, elemCluster, clusterData)

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
	if info.Duration > 0 {
		assert.Greater(t, info.Duration, 0.0)
	}
}

func TestParseSegment_WithCuesSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "segment_cues.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	segmentData := buildMKVSegmentInfoData()

	// Cues element should be skipped
	cuesData := make([]byte, 16)
	segmentData = appendEBMLMasterElement(segmentData, elemCues, cuesData)

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

// --- analyzeWithConfig: additional paths ---

func TestAnalyzeWithConfig_NilConfigUsesDefault(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := createTestAVI(t, tmpDir, "H264", 0x0055)

	info, err := analyzeWithConfig(context.Background(), aviPath, nil)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
}

func TestAnalyzeWithConfig_CLIEnabledButNoBinary(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file with unknown format so native probers fail
	binPath := filepath.Join(tmpDir, "unknown.bin")
	unknownHeader := make([]byte, 32)
	for i := range unknownHeader {
		unknownHeader[i] = 0xDE
	}
	require.NoError(t, os.WriteFile(binPath, unknownHeader, 0644))

	cfg := &mediaInfoConfig{
		CLIEnabled: true,
		CLIPath:    "/nonexistent/mediainfo_binary",
		CLITimeout: 5,
	}

	_, err := analyzeWithConfig(context.Background(), binPath, cfg)
	assert.Error(t, err)
}

func TestAnalyzeWithConfig_AspectRatioComputed(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := createTestAVI(t, tmpDir, "H264", 0x0055)

	info, err := analyzeWithConfig(context.Background(), aviPath, nil)
	require.NoError(t, err)
	assert.Greater(t, info.AspectRatio, 0.0)
}

// --- probeWithFallback: native prober error → CLI fallback ---

func TestProbeWithFallback_NativeProberFails_CLIEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a valid AVI file — native prober should succeed
	aviPath := createTestAVI(t, tmpDir, "XVID", 0x0001)

	f, err := os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	cfg := &mediaInfoConfig{
		CLIEnabled: true,
		CLIPath:    "mediainfo",
		CLITimeout: 5,
	}
	registry := newProberRegistry(cfg)

	info, err := registry.probeWithFallback(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
}

// --- parseEBMLHeader: zero size ---

func TestParseEBMLHeader_ZeroSize(t *testing.T) {
	er := newEBMLReader(bytes.NewReader(nil))
	// Zero size should return immediately
	parseEBMLHeader(er, 0)
}

// --- MKV: audio track with zero channels defaults to 2 ---

func TestAnalyzeMKV_AudioZeroChannelsDefaultsToTwo(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "zero_channels.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Build segment with audio track that has no channels element
	segmentData := buildMKVSegmentInfoData()
	segmentData = append(segmentData, buildMKVTracksDataNoChannels()...)

	buf = appendEBMLMasterElement(buf, elemSegment, segmentData)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, 2, info.AudioChannels, "zero channels should default to 2")
}

// --- MP4 fallback: parseStsdAudio with short data ---

func TestParseStsdAudio_ShortData(t *testing.T) {
	// Provide enough for codec but not for channels/sampleRate
	// Layout: box_size(4) + entry_count(4) + entry_size(4) + codec(4) + extra
	data := make([]byte, 20)
	binary.BigEndian.PutUint32(data[4:8], 1) // entry count = 1
	copy(data[12:16], "mp4a")                // codec at off+4:off+8
	codec, channels, sampleRate := parseStsdAudio(data)
	assert.Equal(t, "mp4a", codec)
	assert.Equal(t, uint16(0), channels)
	assert.Equal(t, uint16(0), sampleRate)
}

func TestParseStsdAudio_ZeroEntries(t *testing.T) {
	codec, channels, sampleRate := parseStsdAudio([]byte{0, 0, 0, 0})
	assert.Equal(t, "", codec)
	assert.Equal(t, uint16(0), channels)
	assert.Equal(t, uint16(0), sampleRate)
}

func TestParseStsdAudio_TooShort(t *testing.T) {
	codec, _, _ := parseStsdAudio([]byte{0, 0, 0})
	assert.Equal(t, "", codec)
}

func TestParseStsdVideo_TooShort(t *testing.T) {
	codec, _, _ := parseStsdVideo([]byte{0, 0, 0})
	assert.Equal(t, "", codec)
}

func TestParseStsdVideo_ZeroEntries(t *testing.T) {
	codec, _, _ := parseStsdVideo([]byte{0, 0, 0, 0})
	assert.Equal(t, "", codec)
}

func TestParseStsdVideo_ShortAfterCodec(t *testing.T) {
	// Enough data for codec but not enough for width/height
	data := make([]byte, 20)
	binary.BigEndian.PutUint32(data[4:8], 1) // entry count = 1
	copy(data[12:16], "avc1")                // codec at off+4:off+8
	codec, w, h := parseStsdVideo(data)
	assert.Equal(t, "avc1", codec)
	assert.Equal(t, uint16(0), w)
	assert.Equal(t, uint16(0), h)
}

// --- AVI: top-level avih chunk (not inside hdrl) ---

func TestAnalyzeAVI_TopLevelAvihChunk(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "top_avih.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))

	// Top-level avih chunk (outside hdrl)
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 40000) // 25fps
	writeUint32LE(t, f, 0)     // MaxBytesPerSec
	writeUint32LE(t, f, 0)     // PaddingGranularity
	writeUint32LE(t, f, 0)     // Flags
	writeUint32LE(t, f, 100)   // TotalFrames
	writeUint32LE(t, f, 0)     // InitialFrames
	writeUint32LE(t, f, 1)     // Streams
	writeUint32LE(t, f, 0)     // SuggestedBufferSize
	writeUint32LE(t, f, 640)   // Width
	writeUint32LE(t, f, 480)   // Height
	writeUint32LE(t, f, 0)     // Reserved[0]
	writeUint32LE(t, f, 0)     // Reserved[1]
	writeUint32LE(t, f, 0)     // Reserved[2]
	writeUint32LE(t, f, 0)     // Reserved[3]

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 640, info.Width)
	assert.Equal(t, 480, info.Height)
	assert.InDelta(t, 25.0, info.FrameRate, 0.1)
}

// --- AVI: parseStrlList with video stream and negative height ---

func TestParseStrlList_NegativeBitmapHeight(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "neg_height.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 100)
	writeBytes(t, f, []byte("strl"))

	// strh
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("vids"))
	writeBytes(t, f, []byte("XVID"))
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 25)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 640)
	writeUint16LE(t, f, 480)

	// strf with negative height
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 40)
	writeUint32LE(t, f, 40)
	writeInt32LE(t, f, 640)
	writeInt32LE(t, f, -480) // Negative height
	writeUint16LE(t, f, 1)
	writeUint16LE(t, f, 24)
	writeBytes(t, f, []byte("XVID"))
	writeUint32LE(t, f, 0)
	writeInt32LE(t, f, 0)
	writeInt32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0) // Skip LIST + size + type

	stream, err := parseStrlList(f, 12, 100)
	require.NoError(t, err)
	assert.True(t, stream.isVideo)
	assert.Equal(t, 640, stream.width)
	assert.Equal(t, 480, stream.height, "negative height should be converted to absolute value")
}

// --- AVI: parseHdrlList with strl sub-list ---

func TestParseHdrlList_WithStrlSubList(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "hdrl_with_strl.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST hdrl
	writeBytes(t, f, []byte("LIST"))
	hdrlSize := uint32(4 + 8 + 56 + 8 + 4 + 8 + 48 + 8 + 40 + 8 + 18)
	writeUint32LE(t, f, hdrlSize)
	writeBytes(t, f, []byte("hdrl"))

	// avih
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 33333)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 2)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1920)
	writeUint32LE(t, f, 1080)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)

	// LIST strl inside hdrl
	strlSize := uint32(4 + 8 + 48 + 8 + 40)
	writeBytes(t, f, []byte("LIST"))
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
	writeUint16LE(t, f, 1920)
	writeUint16LE(t, f, 1080)

	// strf video
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 40)
	writeUint32LE(t, f, 40)
	writeInt32LE(t, f, 1920)
	writeInt32LE(t, f, 1080)
	writeUint16LE(t, f, 1)
	writeUint16LE(t, f, 24)
	writeBytes(t, f, []byte("H264"))
	writeUint32LE(t, f, 0)
	writeInt32LE(t, f, 0)
	writeInt32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info := &VideoInfo{}
	_, _ = f.Seek(12, 0)

	videoFound := false
	audioFound := false
	err = parseHdrlList(f, info, 12, hdrlSize, &videoFound, &audioFound)
	require.NoError(t, err)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, "h264", info.VideoCodec)
}

// --- AVI: parseHdrlList with unknown chunk (default case) ---

func TestParseHdrlList_UnknownChunkDefault(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "hdrl_unknown.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 24)
	writeBytes(t, f, []byte("hdrl"))

	// Unknown chunk
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 8)
	writeBytes(t, f, []byte("testdata"))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info := &VideoInfo{}
	_, _ = f.Seek(12, 0)

	videoFound := false
	audioFound := false
	err = parseHdrlList(f, info, 12, 24, &videoFound, &audioFound)
	assert.NoError(t, err)
}

// --- AVI: parseHdrlList with odd-sized chunk (word alignment) ---

func TestParseHdrlList_OddSizeChunkAlignment(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "hdrl_odd.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 23)
	writeBytes(t, f, []byte("hdrl"))

	// Odd-sized unknown chunk
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 7)               // Odd size
	writeBytes(t, f, []byte("testdata")) // 8 bytes (1 more than size, but will be padded)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info := &VideoInfo{}
	_, _ = f.Seek(12, 0)

	videoFound := false
	audioFound := false
	err = parseHdrlList(f, info, 12, 23, &videoFound, &audioFound)
	assert.NoError(t, err)
}

// --- parseStrlList: video stream with compression codec override ---

func TestParseStrlList_CompressionOverridesHandler(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "compression_override.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 100)
	writeBytes(t, f, []byte("strl"))

	// strh with unknown handler
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("vids"))
	writeBytes(t, f, []byte("XXXX")) // Unknown handler
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

	// strf with H264 compression — should override handler
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 40)
	writeUint32LE(t, f, 40)
	writeInt32LE(t, f, 640)
	writeInt32LE(t, f, 480)
	writeUint16LE(t, f, 1)
	writeUint16LE(t, f, 24)
	writeBytes(t, f, []byte("H264")) // Compression overrides handler
	writeUint32LE(t, f, 0)
	writeInt32LE(t, f, 0)
	writeInt32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, 100)
	require.NoError(t, err)
	assert.Equal(t, "h264", stream.codec, "compression field should override handler")
}

// --- parseStrlList: odd-sized strf chunk alignment ---

func TestParseStrlList_OddSizeStrfAlignment(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "odd_strf.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 80)
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

	// strf with odd size (19 instead of 18)
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 19) // Odd size
	writeUint16LE(t, f, 0x0055)
	writeUint16LE(t, f, 2)
	writeUint32LE(t, f, 44100)
	writeUint32LE(t, f, 176400)
	writeUint16LE(t, f, 4)
	writeUint16LE(t, f, 16)
	// 18 bytes for WAVEFORMATEX + 1 padding byte
	writeByte(t, f, 0)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, 80)
	require.NoError(t, err)
	assert.True(t, stream.isAudio)
}

// --- MKV helper functions for building binary MKV data ---

// appendEBMLHeader appends a minimal EBML header element to buf.
func appendEBMLHeader(buf []byte) []byte {
	// EBML Version = 1
	ebmlVersionData := appendEBMLUint(0x4286, 1)
	// EBML ReadVersion = 1
	ebmlReadVersionData := appendEBMLUint(0x42F7, 1)
	// EBML MaxIDLength = 4
	ebmlMaxIDLengthData := appendEBMLUint(0x42F2, 4)
	// EBML MaxSizeLength = 8
	ebmlMaxSizeLengthData := appendEBMLUint(0x42F3, 8)
	// DocType = "matroska"
	docTypeData := appendEBMLString(0x4282, "matroska")
	// DocTypeVersion = 4
	docTypeVersionData := appendEBMLUint(0x4287, 4)
	// DocTypeReadVersion = 2
	docTypeReadVersionData := appendEBMLUint(0x4285, 2)

	var headerData []byte
	headerData = append(headerData, ebmlVersionData...)
	headerData = append(headerData, ebmlReadVersionData...)
	headerData = append(headerData, ebmlMaxIDLengthData...)
	headerData = append(headerData, ebmlMaxSizeLengthData...)
	headerData = append(headerData, docTypeData...)
	headerData = append(headerData, docTypeVersionData...)
	headerData = append(headerData, docTypeReadVersionData...)

	return appendEBMLMasterElement(buf, elemEBML, headerData)
}

// appendEBMLMasterElement appends an EBML master element (ID + size + children data).
func appendEBMLMasterElement(buf []byte, id uint32, data []byte) []byte {
	buf = appendEBMLID(buf, id)
	buf = appendEBMLVintSize(buf, uint64(len(data)))
	buf = append(buf, data...)
	return buf
}

// appendEBMLID encodes an EBML element ID.
func appendEBMLID(buf []byte, id uint32) []byte {
	switch {
	case id&0xFF000000 != 0:
		return binary.BigEndian.AppendUint32(buf, id)
	case id&0x00FF0000 != 0:
		return binary.BigEndian.AppendUint16(buf, uint16(id))
	case id&0x0000FF00 != 0:
		return append(buf, byte(id>>8), byte(id))
	default:
		return append(buf, byte(id))
	}
}

// appendEBMLVintSize encodes an EBML variable-length size.
func appendEBMLVintSize(buf []byte, size uint64) []byte {
	switch {
	case size < 0x7F:
		return append(buf, byte(0x80|size))
	case size < 0x3FFF:
		b1 := byte(0x40 | (size >> 8))
		b2 := byte(size)
		return append(buf, b1, b2)
	case size < 0x1FFFFF:
		b1 := byte(0x20 | (size >> 16))
		b2 := byte(size >> 8)
		b3 := byte(size)
		return append(buf, b1, b2, b3)
	case size < 0x0FFFFFFF:
		b1 := byte(0x10 | (size >> 24))
		b2 := byte(size >> 16)
		b3 := byte(size >> 8)
		b4 := byte(size)
		return append(buf, b1, b2, b3, b4)
	default:
		// 8-byte vint for very large sizes
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, size)
		b[0] = 0x01
		return append(buf, b...)
	}
}

// appendEBMLUint appends an EBML unsigned integer element.
func appendEBMLUint(id uint32, value uint64) []byte {
	var data []byte
	switch {
	case value <= 0xFF:
		data = []byte{byte(value)}
	case value <= 0xFFFF:
		data = binary.BigEndian.AppendUint16(nil, uint16(value))
	case value <= 0xFFFFFF:
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(value))
		data = b[1:]
	default:
		data = binary.BigEndian.AppendUint32(nil, uint32(value))
	}
	var buf []byte
	buf = appendEBMLID(buf, uint32(id))
	buf = appendEBMLVintSize(buf, uint64(len(data)))
	buf = append(buf, data...)
	return buf
}

// appendEBMLFloat appends an EBML float element.
func appendEBMLFloat(id uint32, value float64) []byte {
	data := binary.BigEndian.AppendUint64(nil, math.Float64bits(value))

	var buf []byte
	buf = appendEBMLID(buf, uint32(id))
	buf = appendEBMLVintSize(buf, uint64(len(data)))
	buf = append(buf, data...)
	return buf
}

// appendEBMLString appends an EBML string element.
func appendEBMLString(id uint32, s string) []byte {
	var buf []byte
	buf = appendEBMLID(buf, uint32(id))
	buf = appendEBMLVintSize(buf, uint64(len(s)))
	buf = append(buf, s...)
	return buf
}

// buildMKVSegmentData builds the inner data of a Segment element with Info + Tracks.
func buildMKVSegmentData() []byte {
	data := buildMKVSegmentInfoData()
	data = append(data, buildMKVTracksData()...)
	return data
}

// buildMKVSegmentInfoData builds SegmentInfo with TimecodeScale + Duration.
func buildMKVSegmentInfoData() []byte {
	var infoData []byte
	// TimecodeScale = 1000000 (default, nanoseconds)
	infoData = append(infoData, appendEBMLUint(elemTimecodeScale, 1000000)...)
	// Duration = 120.5 seconds (as float64)
	infoData = append(infoData, appendEBMLFloat(elemDuration, 120.5)...)

	var data []byte
	data = appendEBMLMasterElement(data, elemInfo, infoData)
	return data
}

// buildMKVTracksData builds Tracks with one video and one audio track.
func buildMKVTracksData() []byte {
	var tracksData []byte

	// Video track
	var videoTrackData []byte
	videoTrackData = append(videoTrackData, appendEBMLUint(elemTrackNumber, 1)...)
	videoTrackData = append(videoTrackData, appendEBMLUint(elemTrackType, trackTypeVideo)...)
	videoTrackData = append(videoTrackData, appendEBMLString(elemCodecID, "V_MPEG4/ISO/AVC")...)

	var videoData []byte
	videoData = append(videoData, appendEBMLUint(elemPixelWidth, 1920)...)
	videoData = append(videoData, appendEBMLUint(elemPixelHeight, 1080)...)
	videoTrackData = append(videoTrackData, appendEBMLMasterElement(videoTrackData, elemVideo, videoData)...)

	tracksData = append(tracksData, appendEBMLMasterElement(nil, elemTrackEntry, videoTrackData)...)

	// Audio track
	var audioTrackData []byte
	audioTrackData = append(audioTrackData, appendEBMLUint(elemTrackNumber, 2)...)
	audioTrackData = append(audioTrackData, appendEBMLUint(elemTrackType, trackTypeAudio)...)
	audioTrackData = append(audioTrackData, appendEBMLString(elemCodecID, "A_AAC")...)

	var audioData []byte
	audioData = append(audioData, appendEBMLFloat(elemSamplingFreq, 44100.0)...)
	audioData = append(audioData, appendEBMLUint(elemChannels, 2)...)
	audioTrackData = append(audioTrackData, appendEBMLMasterElement(audioTrackData, elemAudio, audioData)...)

	tracksData = append(tracksData, appendEBMLMasterElement(nil, elemTrackEntry, audioTrackData)...)

	return appendEBMLMasterElement(nil, elemTracks, tracksData)
}

// buildMKVTracksDataNoChannels builds Tracks with audio track that has no Channels element.
func buildMKVTracksDataNoChannels() []byte {
	var tracksData []byte

	// Video track
	var videoTrackData []byte
	videoTrackData = append(videoTrackData, appendEBMLUint(elemTrackNumber, 1)...)
	videoTrackData = append(videoTrackData, appendEBMLUint(elemTrackType, trackTypeVideo)...)
	videoTrackData = append(videoTrackData, appendEBMLString(elemCodecID, "V_MPEG4/ISO/AVC")...)

	var videoData []byte
	videoData = append(videoData, appendEBMLUint(elemPixelWidth, 1920)...)
	videoData = append(videoData, appendEBMLUint(elemPixelHeight, 1080)...)
	videoTrackData = append(videoTrackData, appendEBMLMasterElement(videoTrackData, elemVideo, videoData)...)

	tracksData = append(tracksData, appendEBMLMasterElement(nil, elemTrackEntry, videoTrackData)...)

	// Audio track without Channels element
	var audioTrackData []byte
	audioTrackData = append(audioTrackData, appendEBMLUint(elemTrackNumber, 2)...)
	audioTrackData = append(audioTrackData, appendEBMLUint(elemTrackType, trackTypeAudio)...)
	audioTrackData = append(audioTrackData, appendEBMLString(elemCodecID, "A_AAC")...)

	var audioData []byte
	audioData = append(audioData, appendEBMLFloat(elemSamplingFreq, 44100.0)...)
	// No Channels element
	audioTrackData = append(audioTrackData, appendEBMLMasterElement(audioTrackData, elemAudio, audioData)...)

	tracksData = append(tracksData, appendEBMLMasterElement(nil, elemTrackEntry, audioTrackData)...)

	return appendEBMLMasterElement(nil, elemTracks, tracksData)
}
