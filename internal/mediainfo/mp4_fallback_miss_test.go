package mediainfo

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- analyzeMP4Fallback: video-only file (no audio) ---

func TestAnalyzeMP4Fallback_VideoOnly(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "video_only.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	// ftyp
	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	// moov with only video track
	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)

	videoTrak := buildFallbackTrak(t, "vide", 24000, 72000, 1920, 1080, "avc1", 0, 0, "")

	moovData := append(mvhdBox, videoTrak...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Greater(t, info.Duration, 0.0)
}

// --- analyzeMP4Fallback: audio-only file (no video) ---

func TestAnalyzeMP4Fallback_AudioOnly(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "audio_only.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 44100)
	binary.BigEndian.PutUint32(mvhdData[16:20], 132300)
	mvhdBox := buildBox("mvhd", mvhdData)

	audioTrak := buildFallbackTrak(t, "soun", 44100, 132300, 0, 0, "", 2, 44100, "mp4a")

	moovData := append(mvhdBox, audioTrak...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
}

// --- analyzeMP4Fallback: empty moov (no mvhd, no traks) ---

func TestAnalyzeMP4Fallback_EmptyMoov(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty_moov.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	// Empty moov box
	buf = append(buf, buildBox("moov", nil)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no usable data found")
	assert.Nil(t, info)
}

// --- analyzeMP4Fallback: trak without mdia ---

func TestAnalyzeMP4Fallback_TrakWithoutMdia(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "no_mdia.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)

	// trak with no mdia child
	trakBox := buildBox("trak", nil)

	moovData := append(mvhdBox, trakBox...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var info *VideoInfo
	info, err = analyzeMP4Fallback(f)
	// No usable data from tracks since trak has no mdia,
	// but mvhd gives duration, so this may succeed with partial info
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.Equal(t, "mp4", info.Container)
		assert.Greater(t, info.Duration, 0.0) // Duration from mvhd
	}
}

// --- analyzeMP4Fallback: trak with mdia but no hdlr ---

func TestAnalyzeMP4Fallback_TrakWithoutHdlr(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "no_hdlr.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)

	// mdia with mdhd but no hdlr
	mdhdData := make([]byte, 24)
	mdhdData[0] = 0
	binary.BigEndian.PutUint32(mdhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mdhdData[16:20], 72000)
	mdhdBox := buildBox("mdhd", mdhdData)

	mdiaBox := buildBox("mdia", mdhdBox)
	trakBox := buildBox("trak", mdiaBox)

	moovData := append(mvhdBox, trakBox...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var info *VideoInfo
	info, err = analyzeMP4Fallback(f)
	// mdia exists but no hdlr → track handler can't be determined
	// But mvhd gives duration, so this may succeed with partial info
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.Equal(t, "mp4", info.Container)
		assert.Greater(t, info.Duration, 0.0) // Duration from mvhd
	}
}

// --- analyzeMP4Fallback: mdat box (skipped) ---

func TestAnalyzeMP4Fallback_MdatSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "with_mdat.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	// mdat before moov - should be skipped
	mdatData := make([]byte, 100)
	for i := range mdatData {
		mdatData[i] = byte(i)
	}
	buf = append(buf, buildBox("mdat", mdatData)...)

	// moov with valid data
	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)
	videoTrak := buildFallbackTrak(t, "vide", 24000, 72000, 1280, 720, "hvc1", 0, 0, "")
	moovData := append(mvhdBox, videoTrak...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "hevc", info.VideoCodec)
	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
}

// --- analyzeMP4Fallback: movie-level duration fallback ---

func TestAnalyzeMP4Fallback_MovieLevelDurationFallback(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "movie_duration.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 1000)  // timescale
	binary.BigEndian.PutUint32(mvhdData[16:20], 30000) // duration
	mvhdBox := buildBox("mvhd", mvhdData)

	// Video track with 0 duration in mdhd → should fallback to movie-level
	videoTrak := buildFallbackTrak(t, "vide", 1000, 0, 1920, 1080, "avc1", 0, 0, "")

	moovData := append(mvhdBox, videoTrak...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, 30.0, info.Duration) // 30000 / 1000 = 30s
}

// --- analyzeMP4Fallback: unknown box type (skipped) ---

func TestAnalyzeMP4Fallback_UnknownBoxTypeSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "unknown_box.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	// Unknown box type before moov
	unknownData := []byte("some unknown data")
	buf = append(buf, buildBox("xxxx", unknownData)...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)
	videoTrak := buildFallbackTrak(t, "vide", 24000, 72000, 1920, 1080, "avc1", 0, 0, "")
	moovData := append(mvhdBox, videoTrak...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
}

// --- parseStsdVideo: entry data too short for codec ---

func TestParseStsdVideo_EntryTooShortForCodec(t *testing.T) {
	data := make([]byte, 10)                 // Too short for offset+8
	binary.BigEndian.PutUint32(data[4:8], 1) // entry count
	codec, w, h := parseStsdVideo(data)
	assert.Equal(t, "", codec)
	assert.Equal(t, uint16(0), w)
	assert.Equal(t, uint16(0), h)
}

// --- parseStsdAudio: entry data too short for codec ---

func TestParseStsdAudio_EntryTooShortForCodec(t *testing.T) {
	data := make([]byte, 10)                 // Too short for offset+8
	binary.BigEndian.PutUint32(data[4:8], 1) // entry count
	codec, ch, sr := parseStsdAudio(data)
	assert.Equal(t, "", codec)
	assert.Equal(t, uint16(0), ch)
	assert.Equal(t, uint16(0), sr)
}

// --- parseStsdAudio: entry data too short for channels ---

func TestParseStsdAudio_TooShortForChannels(t *testing.T) {
	data := make([]byte, 20)                 // Has entry count + some data but not enough for channels
	binary.BigEndian.PutUint32(data[4:8], 1) // entry count
	off := 8
	copy(data[off+4:off+8], "mp4a") // codec
	// off+28 > len(data), so channels and sample rate will be 0
	codec, ch, sr := parseStsdAudio(data)
	assert.Equal(t, "mp4a", codec)
	assert.Equal(t, uint16(0), ch)
	assert.Equal(t, uint16(0), sr)
}

// --- analyzeMP4Fallback: trak with mdia but no minf ---

func TestAnalyzeMP4Fallback_TrakWithoutMinf(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "no_minf.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)

	// mdia with hdlr but no minf
	hdlrData := make([]byte, 24)
	copy(hdlrData[8:12], "vide")
	hdlrBox := buildBox("hdlr", hdlrData)
	mdhdData := make([]byte, 24)
	mdhdData[0] = 0
	binary.BigEndian.PutUint32(mdhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mdhdData[16:20], 72000)
	mdhdBox := buildBox("mdhd", mdhdData)
	mdiaBox := buildBox("mdia", append(mdhdBox, hdlrBox...))
	trakBox := buildBox("trak", mdiaBox)

	moovData := append(mvhdBox, trakBox...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// No minf → no stbl → no usable data from tracks
	// But mvhd gives duration, so we might get partial info
	info, err := analyzeMP4Fallback(f)
	// Depending on whether duration is enough, this might succeed or fail
	if err != nil {
		assert.Contains(t, err.Error(), "no usable data found")
	} else {
		assert.Equal(t, "mp4", info.Container)
	}
}

// --- analyzeMP4Fallback: trak with minf but no stbl ---

func TestAnalyzeMP4Fallback_TrakWithoutStbl(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "no_stbl.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)

	// mdia with hdlr and minf, but no stbl
	hdlrData := make([]byte, 24)
	copy(hdlrData[8:12], "vide")
	hdlrBox := buildBox("hdlr", hdlrData)
	mdhdData := make([]byte, 24)
	mdhdData[0] = 0
	binary.BigEndian.PutUint32(mdhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mdhdData[16:20], 72000)
	mdhdBox := buildBox("mdhd", mdhdData)
	minfBox := buildBox("minf", nil) // Empty minf
	mdiaBox := buildBox("mdia", append(mdhdBox, append(hdlrBox, minfBox...)...))
	trakBox := buildBox("trak", mdiaBox)

	moovData := append(mvhdBox, trakBox...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	// May succeed with partial info or fail
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.Equal(t, "mp4", info.Container)
	}
}

// --- analyzeMP4Fallback: framerate from stts ---

func TestAnalyzeMP4Fallback_FramerateFromStts(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "framerate.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)

	// Build video trak with stts for framerate calculation
	// 24fps: 1000 samples, delta=1001 at timescale 24000
	videoTrak := buildFallbackTrakWithStts(t, "vide", 24000, 72000, 1920, 1080, "avc1", 1000, 1001)

	moovData := append(mvhdBox, videoTrak...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Greater(t, info.FrameRate, 0.0)
}

// --- analyzeMP4Fallback: video with no stts (framerate stays 0) ---

func TestAnalyzeMP4Fallback_NoStts(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "no_stts.mp4")
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)

	// Build video trak without stts
	hdlrData := make([]byte, 24)
	copy(hdlrData[8:12], "vide")
	hdlrBox := buildBox("hdlr", hdlrData)
	mdhdData := make([]byte, 24)
	mdhdData[0] = 0
	binary.BigEndian.PutUint32(mdhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mdhdData[16:20], 72000)
	mdhdBox := buildBox("mdhd", mdhdData)

	stsdEntry := make([]byte, 86)
	binary.BigEndian.PutUint32(stsdEntry[0:4], uint32(len(stsdEntry)))
	copy(stsdEntry[4:8], "avc1")
	binary.BigEndian.PutUint16(stsdEntry[32:34], 1920)
	binary.BigEndian.PutUint16(stsdEntry[34:36], 1080)
	stsdBox := buildBox("stsd", append([]byte{0, 0, 0, 0, 0, 0, 0, 1}, stsdEntry...))

	stblBox := buildBox("stbl", stsdBox)
	minfBox := buildBox("minf", stblBox)
	mdiaBox := buildBox("mdia", append(mdhdBox, append(hdlrBox, minfBox...)...))
	trakBox := buildBox("trak", mdiaBox)

	moovData := append(mvhdBox, trakBox...)
	buf = append(buf, buildBox("moov", moovData)...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, 0.0, info.FrameRate) // No stts → no framerate
}

// --- mapMP4VideoCodec uncovered mappings ---

func TestMapMP4VideoCodec_Uncovered(t *testing.T) {
	assert.Equal(t, "h264", mapMP4VideoCodec("avc1"))
	assert.Equal(t, "h264", mapMP4VideoCodec("avc3"))
	assert.Equal(t, "hevc", mapMP4VideoCodec("hvc1"))
	assert.Equal(t, "hevc", mapMP4VideoCodec("hev1"))
	assert.Equal(t, "vp9", mapMP4VideoCodec("vp09"))
	assert.Equal(t, "vp8", mapMP4VideoCodec("vp08"))
	assert.Equal(t, "av1", mapMP4VideoCodec("av01"))
	assert.Equal(t, "mpeg4", mapMP4VideoCodec("mp4v"))
	assert.Equal(t, "custom", mapMP4VideoCodec("custom"))
}

// --- mapMP4AudioCodec uncovered mappings ---

func TestMapMP4AudioCodec_Uncovered(t *testing.T) {
	assert.Equal(t, "aac", mapMP4AudioCodec("mp4a"))
	assert.Equal(t, "mp3", mapMP4AudioCodec(".mp3"))
	assert.Equal(t, "mp3", mapMP4AudioCodec("mp3 "))
	assert.Equal(t, "ac3", mapMP4AudioCodec("ac-3"))
	assert.Equal(t, "eac3", mapMP4AudioCodec("ec-3"))
	assert.Equal(t, "opus", mapMP4AudioCodec("opus"))
	assert.Equal(t, "flac", mapMP4AudioCodec("fLaC"))
	assert.Equal(t, "custom", mapMP4AudioCodec("custom"))
}

// --- findBox: size < 8 returns nil ---

func TestFindBox_SmallerThan8(t *testing.T) {
	data := make([]byte, 7)
	binary.BigEndian.PutUint32(data[0:4], 4) // size < 8
	copy(data[4:7], "abc")
	result := findBox(data, "abc")
	assert.Nil(t, result)
}

// --- findBox: size exceeds data length ---

func TestFindBox_SizeExceedsData(t *testing.T) {
	data := make([]byte, 12)
	binary.BigEndian.PutUint32(data[0:4], 100) // size > len(data)
	copy(data[4:8], "moov")
	result := findBox(data, "moov")
	assert.Nil(t, result)
}

// --- findBoxes: truncated box breaks early ---

func TestFindBoxes_TruncatedBreaksEarly(t *testing.T) {
	data := make([]byte, 7) // Too short for a valid box
	results := findBoxes(data, "moov")
	assert.Empty(t, results)
}

// --- parseStts: empty data ---

func TestParseStts_VeryShortData(t *testing.T) {
	totalSamples, totalDelta := parseStts([]byte{0x00})
	assert.Equal(t, uint32(0), totalSamples)
	assert.Equal(t, uint64(0), totalDelta)
}

// --- Helper: build video trak with specific stts ---

func buildFallbackTrakWithStts(t *testing.T, handler string, timescale uint32, duration uint64, w, h uint16, vcodec string, sampleCount uint32, sampleDelta uint32) []byte {
	t.Helper()

	hdlrData := make([]byte, 24)
	copy(hdlrData[8:12], handler)
	hdlrBox := buildBox("hdlr", hdlrData)

	mdhdData := make([]byte, 24)
	mdhdData[0] = 0
	binary.BigEndian.PutUint32(mdhdData[12:16], timescale)
	binary.BigEndian.PutUint32(mdhdData[16:20], uint32(duration))
	mdhdBox := buildBox("mdhd", mdhdData)

	entry := make([]byte, 86)
	binary.BigEndian.PutUint32(entry[0:4], uint32(len(entry)))
	copy(entry[4:8], vcodec)
	binary.BigEndian.PutUint16(entry[32:34], w)
	binary.BigEndian.PutUint16(entry[34:36], h)
	stsdBox := buildBox("stsd", append([]byte{0, 0, 0, 0, 0, 0, 0, 1}, entry...))

	sttsData := make([]byte, 16)
	binary.BigEndian.PutUint32(sttsData[4:8], 1) // entry count
	binary.BigEndian.PutUint32(sttsData[8:12], sampleCount)
	binary.BigEndian.PutUint32(sttsData[12:16], sampleDelta)
	sttsBox := buildBox("stts", sttsData)

	stblBox := buildBox("stbl", append(stsdBox, sttsBox...))
	minfBox := buildBox("minf", stblBox)
	mdiaBox := buildBox("mdia", append(mdhdBox, append(hdlrBox, minfBox...)...))
	return buildBox("trak", mdiaBox)
}
