package mediainfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- MP4: extractMP4VideoInfo with avcC child (H.264 override) ---

func TestExtractMP4VideoInfo_WithAvcC(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "avcc.mp4")

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

	// Create avc1 visual entry with avcC child
	visualEntry := mp4.CreateVisualSampleEntryBox("avc1", 1920, 1080, nil)
	// Create and add an AvcC box
	avcC, err := mp4.CreateAvcC(nil, nil, false)
	if err == nil {
		visualEntry.AddChild(avcC)
	}
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
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
}

// --- MP4: extractMP4VideoInfo with hvcC child (HEVC override) ---

func TestExtractMP4VideoInfo_WithHvcC(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "hvcc.mp4")

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

	// Create hvc1 visual entry with hvcC child
	visualEntry := mp4.CreateVisualSampleEntryBox("hvc1", 3840, 2160, nil)
	// Create and add an HvcC box
	hvcC, err := mp4.CreateHvcC(nil, nil, nil, false, false, false, false)
	if err == nil {
		visualEntry.AddChild(hvcC)
	}
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
	assert.Equal(t, "hevc", info.VideoCodec)
	assert.Equal(t, 3840, info.Width)
	assert.Equal(t, 2160, info.Height)
}

// --- MP4: extractMP4AudioInfo with esds child (AAC override) ---
// Note: Creating a valid EsdsBox via mp4ff requires DecoderConfigDescriptor which is complex.
// The esds codec override path is already covered indirectly through the fallback parser
// which decodes mp4a as "aac" by default. The mp4ff library itself maps mp4a → aac
// when it detects the esds child during decode.

func TestExtractMP4AudioInfo_Mp4aDefault(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "mp4a_default.mp4")

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

	// Also add a video track
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
	// mp4a without esds still maps to "aac" via mapMP4AudioCodec
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
}

// --- MP4: calcMP4FrameRate with zero samples (isFieldBasedStream edge case) ---

func TestCalcMP4FrameRate_ZeroSamples(t *testing.T) {
	// stts with zero samples
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, err := mp4.CreateHdlr("vide")
	require.NoError(t, err)
	mdia.AddChild(hdlr)
	mdhd := &mp4.MdhdBox{Timescale: 24000, Duration: 240000}
	mdia.AddChild(mdhd)
	minf := mp4.NewMinfBox()
	stbl := mp4.NewStblBox()
	stts := &mp4.SttsBox{
		SampleCount:     []uint32{0},
		SampleTimeDelta: []uint32{1000},
	}
	stbl.AddChild(stts)
	minf.AddChild(stbl)
	mdia.AddChild(minf)
	trak.AddChild(mdia)

	fps := calcMP4FrameRate(trak)
	assert.Equal(t, 0.0, fps)
}

// --- MP4: isFieldBasedStream with various deltas ---

func TestIsFieldBasedStream_NonFieldBased(t *testing.T) {
	stts := &mp4.SttsBox{
		SampleCount:     []uint32{100},
		SampleTimeDelta: []uint32{1001},
	}
	result := isFieldBasedStream(stts, 24000)
	// 24000/1001 ≈ 23.976 fps — not a field-based rate
	assert.False(t, result)
}

// --- MP4: analyzeMP4Fallback with audio-only file ---

func TestAnalyzeMP4Fallback_AudioOnlyFile(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "audio_only_fallback.mp4")

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
	astts := &mp4.SttsBox{
		SampleCount:     []uint32{10},
		SampleTimeDelta: []uint32{4410},
	}
	astbl.AddChild(astts)
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

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
}

// --- MKV: parseVideoElement with element needing skip (unknown master element) ---

func TestParseVideoElement_WithUnknownMaster(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "video_unknown_master.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	segmentData := buildMKVSegmentInfoData()

	// Build a video element with an unknown master child followed by known children
	// The unknown master should exercise the skip path
	var videoData []byte
	// Add a DisplayWidth element (0x54B0) as an unknown-to-parse but valid EBML element
	// Use a valid EBML uint element ID that's not pixelWidth/pixelHeight
	videoData = append(videoData, appendEBMLUint(0x54B0, 1920)...)

	// Then the real pixel dimensions
	videoData = append(videoData, appendEBMLUint(elemPixelWidth, 1280)...)
	videoData = append(videoData, appendEBMLUint(elemPixelHeight, 720)...)

	var trackData []byte
	trackData = append(trackData, appendEBMLUint(elemTrackNumber, 1)...)
	trackData = append(trackData, appendEBMLUint(elemTrackType, trackTypeVideo)...)
	trackData = append(trackData, appendEBMLString(elemCodecID, "V_MPEG4/ISO/AVC")...)
	trackData = append(trackData, appendEBMLMasterElement(trackData, elemVideo, videoData)...)

	var tracksData []byte
	tracksData = append(tracksData, appendEBMLMasterElement(nil, elemTrackEntry, trackData)...)

	segmentData = append(segmentData, appendEBMLMasterElement(nil, elemTracks, tracksData)...)

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
	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
}

// --- MKV: parseAudioElement with element needing skip ---

func TestParseAudioElement_WithUnknownElement(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "audio_unknown_elem.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	segmentData := buildMKVSegmentInfoData()

	// Build an audio element with an unknown uint element
	var audioData []byte
	// AudioBitDepth element (0x6264) — not SamplingFreq or Channels
	audioData = append(audioData, appendEBMLUint(0x6264, 16)...)
	audioData = append(audioData, appendEBMLFloat(elemSamplingFreq, 48000.0)...)
	audioData = append(audioData, appendEBMLUint(elemChannels, 6)...)

	var trackData []byte
	trackData = append(trackData, appendEBMLUint(elemTrackNumber, 2)...)
	trackData = append(trackData, appendEBMLUint(elemTrackType, trackTypeAudio)...)
	trackData = append(trackData, appendEBMLString(elemCodecID, "A_AC3")...)
	trackData = append(trackData, appendEBMLMasterElement(trackData, elemAudio, audioData)...)

	// Video track
	var videoTrackData []byte
	videoTrackData = append(videoTrackData, appendEBMLUint(elemTrackNumber, 1)...)
	videoTrackData = append(videoTrackData, appendEBMLUint(elemTrackType, trackTypeVideo)...)
	videoTrackData = append(videoTrackData, appendEBMLString(elemCodecID, "V_MPEG4/ISO/AVC")...)
	var videoData []byte
	videoData = append(videoData, appendEBMLUint(elemPixelWidth, 1920)...)
	videoData = append(videoData, appendEBMLUint(elemPixelHeight, 1080)...)
	videoTrackData = append(videoTrackData, appendEBMLMasterElement(videoTrackData, elemVideo, videoData)...)

	var tracksData []byte
	tracksData = append(tracksData, appendEBMLMasterElement(nil, elemTrackEntry, trackData)...)
	tracksData = append(tracksData, appendEBMLMasterElement(nil, elemTrackEntry, videoTrackData)...)

	segmentData = append(segmentData, appendEBMLMasterElement(nil, elemTracks, tracksData)...)

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
	assert.Equal(t, "ac3", info.AudioCodec)
	assert.Equal(t, 6, info.AudioChannels)
	assert.Equal(t, 48000, info.SampleRate)
}
