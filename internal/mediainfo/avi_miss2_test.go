package mediainfo

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- analyzeAVI: full AVI with hdrl containing avih + strl ---

func TestMiss2_AnalyzeAVI_FullStructure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "test.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// Write RIFF header
	writeBytes(t, f, []byte("RIFF"))
	sizePos, _ := f.Seek(0, 1)
	writeUint32LE(t, f, 0) // placeholder
	writeBytes(t, f, []byte("AVI "))

	// Build hdrl LIST content
	hdrlContent := buildAVIHdrlContent(t)
	// Write hdrl LIST
	writeBytes(t, f, buildRIFFListChunk("hdrl", hdrlContent))

	// Fill in RIFF size
	totalSize, _ := f.Seek(0, 1)
	f.Seek(sizePos, 0)
	writeUint32LE(t, f, uint32(totalSize-8))
	f.Seek(0, 2) // seek to end

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, "aac", info.AudioCodec)
}

func buildAVIHdrlContent(t *testing.T) []byte {
	t.Helper()

	// avih chunk
	avihData := makeAVIMainHeader(33333, 0, 100, 1920, 1080)
	result := buildRIFFDataChunk("avih", avihData)

	// strl LIST with video stream
	result = append(result, buildRIFFListChunk("strl", buildVideoStrl(t))...)

	// strl LIST with audio stream
	result = append(result, buildRIFFListChunk("strl", buildAudioStrl(t))...)

	return result
}

func makeAVIMainHeader(microSecPerFrame, flags, totalFrames, width, height uint32) []byte {
	data := make([]byte, 56)
	binary.LittleEndian.PutUint32(data[0:4], microSecPerFrame)
	binary.LittleEndian.PutUint32(data[12:16], flags)
	binary.LittleEndian.PutUint32(data[16:20], totalFrames)
	binary.LittleEndian.PutUint32(data[24:28], 2) // Streams
	binary.LittleEndian.PutUint32(data[32:36], width)
	binary.LittleEndian.PutUint32(data[36:40], height)
	return data
}

func buildVideoStrl(t *testing.T) []byte {
	t.Helper()

	// strh chunk (stream header)
	strhData := make([]byte, 56)
	copy(strhData[0:4], []byte("vids"))
	copy(strhData[4:8], []byte("H264"))
	binary.LittleEndian.PutUint32(strhData[20:24], 1)   // Scale
	binary.LittleEndian.PutUint32(strhData[24:28], 30)  // Rate
	binary.LittleEndian.PutUint32(strhData[32:36], 100) // Length
	result := buildRIFFDataChunk("strh", strhData)

	// strf chunk (BITMAPINFOHEADER)
	strfData := make([]byte, 40)
	binary.LittleEndian.PutUint32(strfData[0:4], 40)
	binary.LittleEndian.PutUint32(strfData[4:8], 1920)
	binary.LittleEndian.PutUint32(strfData[8:12], 1080)
	binary.LittleEndian.PutUint16(strfData[12:14], 1)
	binary.LittleEndian.PutUint16(strfData[14:16], 24)
	copy(strfData[16:20], []byte("H264"))
	result = append(result, buildRIFFDataChunk("strf", strfData)...)

	return result
}

func buildAudioStrl(t *testing.T) []byte {
	t.Helper()

	// strh chunk
	strhData := make([]byte, 56)
	copy(strhData[0:4], []byte("auds"))
	binary.LittleEndian.PutUint32(strhData[20:24], 1)
	binary.LittleEndian.PutUint32(strhData[24:28], 48000)
	result := buildRIFFDataChunk("strh", strhData)

	// strf chunk (WAVEFORMATEX)
	strfData := make([]byte, 18)
	binary.LittleEndian.PutUint16(strfData[0:2], 0x00FF) // AAC
	binary.LittleEndian.PutUint16(strfData[2:4], 2)
	binary.LittleEndian.PutUint32(strfData[4:8], 48000)
	binary.LittleEndian.PutUint32(strfData[8:12], 192000)
	binary.LittleEndian.PutUint16(strfData[12:14], 4)
	binary.LittleEndian.PutUint16(strfData[14:16], 16)
	result = append(result, buildRIFFDataChunk("strf", strfData)...)

	return result
}

func buildRIFFDataChunk(fourCC string, data []byte) []byte {
	var buf []byte
	buf = append(buf, []byte(fourCC)...)
	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, uint32(len(data)))
	buf = append(buf, sizeBytes...)
	buf = append(buf, data...)
	if len(data)%2 != 0 {
		buf = append(buf, 0)
	}
	return buf
}

func buildRIFFListChunk(listType string, content []byte) []byte {
	var buf []byte
	buf = append(buf, []byte("LIST")...)
	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, uint32(len(content)+4))
	buf = append(buf, sizeBytes...)
	buf = append(buf, []byte(listType)...)
	buf = append(buf, content...)
	return buf
}

// --- analyzeAVI: with strl stream directly at top level ---

func TestMiss2_AnalyzeAVI_TopLevelStrl(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "strl.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("RIFF"))
	sizePos, _ := f.Seek(0, 1)
	writeUint32LE(t, f, 0)
	writeBytes(t, f, []byte("AVI "))

	avihData := makeAVIMainHeader(33333, 0, 100, 1280, 720)
	writeBytes(t, f, buildRIFFDataChunk("avih", avihData))
	writeBytes(t, f, buildRIFFListChunk("strl", buildVideoStrl(t)))

	totalSize, _ := f.Seek(0, 1)
	f.Seek(sizePos, 0)
	writeUint32LE(t, f, uint32(totalSize-8))
	f.Seek(0, 2)

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
}

// --- analyzeAVI: with hdrl containing nested strl ---

func TestMiss2_AnalyzeAVI_HdrlWithStrl(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "hdrl.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("RIFF"))
	sizePos, _ := f.Seek(0, 1)
	writeUint32LE(t, f, 0)
	writeBytes(t, f, []byte("AVI "))

	hdrlContent := buildAVIHdrlContent(t)
	writeBytes(t, f, buildRIFFListChunk("hdrl", hdrlContent))

	totalSize, _ := f.Seek(0, 1)
	f.Seek(sizePos, 0)
	writeUint32LE(t, f, uint32(totalSize-8))
	f.Seek(0, 2)

	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, 1920, info.Width)
}

// --- mapAVIVideoCodec: more codec mappings ---

func TestMiss2_MapAVIVideoCodec(t *testing.T) {
	assert.Equal(t, "xvid", mapAVIVideoCodec("XVID"))
	assert.Equal(t, "divx", mapAVIVideoCodec("DIVX"))
	assert.Equal(t, "divx", mapAVIVideoCodec("DX50"))
	assert.Equal(t, "mpeg4", mapAVIVideoCodec("MP42"))
	assert.Equal(t, "mpeg4_v3", mapAVIVideoCodec("MP43"))
	assert.Equal(t, "wmv1", mapAVIVideoCodec("WMV1"))
	assert.Equal(t, "wmv2", mapAVIVideoCodec("WMV2"))
	assert.Equal(t, "wmv3", mapAVIVideoCodec("WMV3"))
	assert.Equal(t, "vp8", mapAVIVideoCodec("VP80"))
	assert.Equal(t, "vp9", mapAVIVideoCodec("VP90"))
	assert.Equal(t, "mjpeg", mapAVIVideoCodec("MJPG"))
	assert.Equal(t, "dv", mapAVIVideoCodec("dvsd"))
	assert.Equal(t, "ffv1", mapAVIVideoCodec("FFV1"))
	assert.Equal(t, "h265", mapAVIVideoCodec("HEVC"))
	assert.Equal(t, "h265", mapAVIVideoCodec("HVC1"))
	assert.Equal(t, codecUnknown, mapAVIVideoCodec(""))
	assert.Equal(t, "custom", mapAVIVideoCodec("custom"))
}

// --- mapAVIAudioCodec: more codec mappings ---

func TestMiss2_MapAVIAudioCodec(t *testing.T) {
	assert.Equal(t, "pcm", mapAVIAudioCodec(0x0001))
	assert.Equal(t, "adpcm", mapAVIAudioCodec(0x0002))
	assert.Equal(t, "pcm_float", mapAVIAudioCodec(0x0003))
	assert.Equal(t, "mp2", mapAVIAudioCodec(0x0050))
	assert.Equal(t, "mp3", mapAVIAudioCodec(0x0055))
	assert.Equal(t, "wmav1", mapAVIAudioCodec(0x0161))
	assert.Equal(t, "wmav2", mapAVIAudioCodec(0x0162))
	assert.Equal(t, "wmav3", mapAVIAudioCodec(0x0163))
	assert.Equal(t, "ac3", mapAVIAudioCodec(0x2000))
	assert.Equal(t, "dts", mapAVIAudioCodec(0x2001))
	assert.Equal(t, "aac", mapAVIAudioCodec(0x00FF))
	assert.Equal(t, "aac", mapAVIAudioCodec(0xFFFE))
	assert.Equal(t, "vorbis", mapAVIAudioCodec(0x0674))
	assert.Equal(t, "opus", mapAVIAudioCodec(0x6750))
	assert.Equal(t, "flac", mapAVIAudioCodec(0xF1AC))
	assert.Equal(t, codecUnknown, mapAVIAudioCodec(0x9999))
}

// --- canProbe ---

func TestMiss2_AVI_CanProbe(t *testing.T) {
	p := newAVIProber()
	assert.True(t, p.canProbe([]byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'A', 'V', 'I', ' '}))
	assert.False(t, p.canProbe([]byte{'R', 'I', 'F'}))
	assert.False(t, p.canProbe([]byte{0, 0, 0, 0, 0, 0, 0, 0, 'A', 'V', 'I', ' '}))
}

// --- Probe method ---

func TestMiss2_AVI_Probe(t *testing.T) {
	p := newAVIProber()
	assert.Equal(t, "avi", p.Name())

	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "probe.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("RIFF"))
	sizePos, _ := f.Seek(0, 1)
	writeUint32LE(t, f, 0)
	writeBytes(t, f, []byte("AVI "))
	avihData := makeAVIMainHeader(40000, 0, 50, 640, 480)
	writeBytes(t, f, buildRIFFDataChunk("avih", avihData))

	totalSize, _ := f.Seek(0, 1)
	f.Seek(sizePos, 0)
	writeUint32LE(t, f, uint32(totalSize-8))
	f.Seek(0, 2)
	require.NoError(t, f.Close())

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := p.Probe(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, 640, info.Width)
	assert.Equal(t, 480, info.Height)
}
