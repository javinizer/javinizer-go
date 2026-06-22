package mediainfo

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- analyzeAVI: full AVI with hdrl containing avih and strl ---

func TestMiss3_AnalyzeAVI_FullWithHdrlAndStrl(t *testing.T) {
	f, err := os.CreateTemp("", "avi_full_*.avi")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	buf := buildAVIBuffer(t, func(w *aviWriter) {
		w.writeRIFFHeader()
		w.writeHdrlList(func(h *aviHdrlWriter) {
			h.writeAvih(40000, 100, 640, 480) // 25fps, 100 frames, 640x480
			h.writeStrlList(func(s *aviStrlWriter) {
				s.writeStreamHeader("vids", "XVID", 25, 1) // 25fps
				s.writeVideoFormat(640, 480, "XVID")
			})
			h.writeStrlList(func(s *aviStrlWriter) {
				s.writeStreamHeader("auds", "", 0, 0)
				s.writeAudioFormat(0x0055, 2, 44100) // MP3, 2 channels, 44100Hz
			})
		})
	})

	_, err = f.Write(*buf)
	require.NoError(t, err)
	_, err = f.Seek(0, 0)
	require.NoError(t, err)

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 640, info.Width)
	assert.Equal(t, 480, info.Height)
	assert.Equal(t, "xvid", info.VideoCodec)
	assert.Equal(t, "mp3", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 44100, info.SampleRate)
	assert.InDelta(t, 25.0, info.FrameRate, 0.1)
}

// --- analyzeAVI: AVI with top-level avih chunk ---

func TestMiss3_AnalyzeAVI_TopLevelAvih(t *testing.T) {
	f, err := os.CreateTemp("", "avi_avih_*.avi")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	buf := buildAVIBuffer(t, func(w *aviWriter) {
		w.writeRIFFHeader()
		// Write avih at top level (not inside hdrl)
		w.writeAvihChunk(33333, 300, 1280, 720) // ~30fps
	})

	_, err = f.Write(*buf)
	require.NoError(t, err)
	_, err = f.Seek(0, 0)
	require.NoError(t, err)

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
}

// --- analyzeAVI: AVI with LIST strl at top level ---

func TestMiss3_AnalyzeAVI_TopLevelStrlList(t *testing.T) {
	f, err := os.CreateTemp("", "avi_strl_*.avi")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	buf := buildAVIBuffer(t, func(w *aviWriter) {
		w.writeRIFFHeader()
		w.writeStrlListDirect(func(s *aviStrlWriter) {
			s.writeStreamHeader("vids", "H264", 30, 1)
			s.writeVideoFormat(1920, 1080, "H264")
		})
	})

	_, err = f.Write(*buf)
	require.NoError(t, err)
	_, err = f.Seek(0, 0)
	require.NoError(t, err)

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
}

// --- analyzeAVI: AVI with unknown LIST type (should be skipped) ---

func TestMiss3_AnalyzeAVI_UnknownListType(t *testing.T) {
	f, err := os.CreateTemp("", "avi_unknown_list_*.avi")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	buf := buildAVIBuffer(t, func(w *aviWriter) {
		w.writeRIFFHeader()
		w.writeAvihChunk(40000, 100, 320, 240)
		w.writeUnknownList("movi")
	})

	_, err = f.Write(*buf)
	require.NoError(t, err)
	_, err = f.Seek(0, 0)
	require.NoError(t, err)

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, 320, info.Width)
}

// --- analyzeAVI: AVI with unknown top-level chunk ---

func TestMiss3_AnalyzeAVI_UnknownTopLevelChunk(t *testing.T) {
	f, err := os.CreateTemp("", "avi_unknown_chunk_*.avi")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	buf := buildAVIBuffer(t, func(w *aviWriter) {
		w.writeRIFFHeader()
		w.writeAvihChunk(40000, 100, 640, 480)
		w.writeUnknownChunk("JUNK", make([]byte, 16))
	})

	_, err = f.Write(*buf)
	require.NoError(t, err)
	_, err = f.Seek(0, 0)
	require.NoError(t, err)

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, 640, info.Width)
}

// --- analyzeAVI: AVI with odd-size chunk (word alignment) ---

func TestMiss3_AnalyzeAVI_OddSizeChunkAlignment(t *testing.T) {
	f, err := os.CreateTemp("", "avi_odd_*.avi")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	buf := buildAVIBuffer(t, func(w *aviWriter) {
		w.writeRIFFHeader()
		w.writeAvihChunk(40000, 100, 640, 480)
		w.writeUnknownChunk("JUNK", make([]byte, 7)) // Odd size
	})

	_, err = f.Write(*buf)
	require.NoError(t, err)
	_, err = f.Seek(0, 0)
	require.NoError(t, err)

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, 640, info.Width)
}

// --- parseStrlList: video with negative bitmap height ---

func TestMiss3_ParseStrlList_NegativeBitmapHeight(t *testing.T) {
	f, err := os.CreateTemp("", "avi_neg_height_*.avi")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	buf := buildAVIBuffer(t, func(w *aviWriter) {
		w.writeRIFFHeader()
		w.writeHdrlList(func(h *aviHdrlWriter) {
			h.writeAvih(40000, 100, 640, 480)
			h.writeStrlList(func(s *aviStrlWriter) {
				s.writeStreamHeader("vids", "H264", 30, 1)
				s.writeVideoFormatNegHeight(640, -480, "H264")
			})
		})
	})

	_, err = f.Write(*buf)
	require.NoError(t, err)
	_, err = f.Seek(0, 0)
	require.NoError(t, err)

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, 480, info.Height) // Should be positive despite negative in file
}

// --- mapAVIVideoCodec: various fourCC values ---

func TestMiss3_MapAVIVideoCodec_AllMappings(t *testing.T) {
	tests := []struct {
		fourCC string
		want   string
	}{
		{"H264", "h264"},
		{"h264", "h264"},
		{"X264", "h264"},
		{"AVC1", "h264"},
		{"H265", "h265"},
		{"HEVC", "h265"},
		{"XVID", "xvid"},
		{"DIVX", "divx"},
		{"DX50", "divx"},
		{"MP42", "mpeg4"},
		{"MP43", "mpeg4_v3"},
		{"WMV1", "wmv1"},
		{"WMV2", "wmv2"},
		{"WMV3", "wmv3"},
		{"VP80", "vp8"},
		{"VP90", "vp9"},
		{"MJPG", "mjpeg"},
		{"dvsd", "dv"},
		{"FFV1", "ffv1"},
		{"", "unknown"},
		{"CUSTOM", "CUSTOM"},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, mapAVIVideoCodec(tc.fourCC), "mapAVIVideoCodec(%q)", tc.fourCC)
	}
}

// --- mapAVIAudioCodec: various format tags ---

func TestMiss3_MapAVIAudioCodec_AllMappings(t *testing.T) {
	tests := []struct {
		tag  uint16
		want string
	}{
		{0x0001, "pcm"},
		{0x0002, "adpcm"},
		{0x0003, "pcm_float"},
		{0x0050, "mp2"},
		{0x0055, "mp3"},
		{0x0161, "wmav1"},
		{0x0162, "wmav2"},
		{0x0163, "wmav3"},
		{0x2000, "ac3"},
		{0x2001, "dts"},
		{0x00FF, "aac"},
		{0xFFFE, "aac"},
		{0x0674, "vorbis"},
		{0x6750, "opus"},
		{0xF1AC, "flac"},
		{0x9999, "unknown"},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, mapAVIAudioCodec(tc.tag), "mapAVIAudioCodec(0x%04X)", tc.tag)
	}
}

// --- aviProber: canProbe ---

func TestMiss3_AVIProber_CanProbe(t *testing.T) {
	p := newAVIProber()
	assert.True(t, p.canProbe([]byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'A', 'V', 'I', ' '}))
	assert.False(t, p.canProbe([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}))
	assert.False(t, p.canProbe([]byte{}))
	assert.Equal(t, "avi", p.Name())
}

// --- AVI helper types for building binary data ---

type aviWriter struct {
	buf []byte
}

type aviHdrlWriter struct {
	parent *aviWriter
}

type aviStrlWriter struct {
	buf *[]byte
}

func buildAVIBuffer(t *testing.T, build func(w *aviWriter)) *[]byte {
	t.Helper()
	w := &aviWriter{}
	build(w)
	return &w.buf
}

func (w *aviWriter) writeRIFFHeader() {
	// RIFF header + "AVI " form type
	// Size includes everything after the first 8 bytes
	// We'll compute it later; for now, write a placeholder and fix it
	// Actually, for testing, let's just write the header and form type
	// The size field will be wrong but the parser only needs the signature
	data := make([]byte, 4)
	copy(data, "AVI ")
	w.buf = append(w.buf, aviTestRiffChunk("RIFF", data)...)
}

func (w *aviWriter) writeHdrlList(build func(h *aviHdrlWriter)) {
	h := &aviHdrlWriter{parent: w}
	oldBuf := w.buf
	w.buf = nil
	build(h)
	hdrlContent := w.buf
	w.buf = oldBuf

	// Prepend "hdrl" type to content
	listContent := make([]byte, 4)
	copy(listContent, "hdrl")
	listContent = append(listContent, hdrlContent...)
	w.buf = append(w.buf, aviTestRiffChunk("LIST", listContent)...)
}

func (h *aviHdrlWriter) writeAvih(microSecPerFrame, totalFrames, width, height uint32) {
	data := make([]byte, 56) // aviMainHeader is 56 bytes
	binary.LittleEndian.PutUint32(data[0:4], microSecPerFrame)
	binary.LittleEndian.PutUint32(data[16:20], totalFrames)
	binary.LittleEndian.PutUint32(data[32:36], width)
	binary.LittleEndian.PutUint32(data[36:40], height)
	h.parent.buf = append(h.parent.buf, aviTestRiffChunk("avih", data)...)
}

func (h *aviHdrlWriter) writeStrlList(build func(s *aviStrlWriter)) {
	var strlContent []byte
	s := &aviStrlWriter{buf: &strlContent}
	build(s)

	listContent := make([]byte, 4)
	copy(listContent, "strl")
	listContent = append(listContent, strlContent...)
	h.parent.buf = append(h.parent.buf, aviTestRiffChunk("LIST", listContent)...)
}

func (w *aviWriter) writeStrlListDirect(build func(s *aviStrlWriter)) {
	var strlContent []byte
	s := &aviStrlWriter{buf: &strlContent}
	build(s)

	listContent := make([]byte, 4)
	copy(listContent, "strl")
	listContent = append(listContent, strlContent...)
	w.buf = append(w.buf, aviTestRiffChunk("LIST", listContent)...)
}

func (s *aviStrlWriter) writeStreamHeader(streamType, handler string, rate, scale uint32) {
	data := make([]byte, 56) // aviStreamHeader is 56 bytes
	copy(data[0:4], streamType)
	copy(data[4:8], handler)
	binary.LittleEndian.PutUint32(data[20:24], scale)
	binary.LittleEndian.PutUint32(data[24:28], rate)
	*s.buf = append(*s.buf, aviTestRiffChunk("strh", data)...)
}

func (s *aviStrlWriter) writeVideoFormat(width, height int32, compression string) {
	data := make([]byte, 40)                     // BITMAPINFOHEADER is 40 bytes
	binary.LittleEndian.PutUint32(data[0:4], 40) // Size
	binary.LittleEndian.PutUint32(data[4:8], uint32(width))
	binary.LittleEndian.PutUint32(data[8:12], uint32(height))
	binary.LittleEndian.PutUint16(data[12:14], 1)  // Planes
	binary.LittleEndian.PutUint16(data[14:16], 24) // BitCount
	copy(data[16:20], compression)
	*s.buf = append(*s.buf, aviTestRiffChunk("strf", data)...)
}

func (s *aviStrlWriter) writeVideoFormatNegHeight(width, height int32, compression string) {
	data := make([]byte, 40) // BITMAPINFOHEADER
	binary.LittleEndian.PutUint32(data[0:4], 40)
	binary.LittleEndian.PutUint32(data[4:8], uint32(width))
	binary.LittleEndian.PutUint32(data[8:12], uint32(height)) // Negative height
	binary.LittleEndian.PutUint16(data[12:14], 1)
	binary.LittleEndian.PutUint16(data[14:16], 24)
	copy(data[16:20], compression)
	*s.buf = append(*s.buf, aviTestRiffChunk("strf", data)...)
}

func (s *aviStrlWriter) writeAudioFormat(formatTag uint16, channels uint16, samplesPerSec uint32) {
	data := make([]byte, 18) // WAVEFORMATEX
	binary.LittleEndian.PutUint16(data[0:2], formatTag)
	binary.LittleEndian.PutUint16(data[2:4], channels)
	binary.LittleEndian.PutUint32(data[4:8], samplesPerSec)
	binary.LittleEndian.PutUint32(data[8:12], samplesPerSec*2) // AvgBytesPerSec (approx)
	binary.LittleEndian.PutUint16(data[12:14], 2)              // BlockAlign
	binary.LittleEndian.PutUint16(data[14:16], 16)             // BitsPerSample
	*s.buf = append(*s.buf, aviTestRiffChunk("strf", data)...)
}

func (w *aviWriter) writeAvihChunk(microSecPerFrame, totalFrames, width, height uint32) {
	data := make([]byte, 56)
	binary.LittleEndian.PutUint32(data[0:4], microSecPerFrame)
	binary.LittleEndian.PutUint32(data[16:20], totalFrames)
	binary.LittleEndian.PutUint32(data[32:36], width)
	binary.LittleEndian.PutUint32(data[36:40], height)
	w.buf = append(w.buf, aviTestRiffChunk("avih", data)...)
}

func (w *aviWriter) writeUnknownList(listType string) {
	listContent := make([]byte, 4)
	copy(listContent, listType)
	w.buf = append(w.buf, aviTestRiffChunk("LIST", listContent)...)
}

func (w *aviWriter) writeUnknownChunk(fourCC string, data []byte) {
	w.buf = append(w.buf, aviTestRiffChunk(fourCC, data)...)
}

func aviTestRiffChunk(fourCC string, data []byte) []byte {
	buf := make([]byte, 8)
	copy(buf[0:4], fourCC)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(data)))
	buf = append(buf, data...)
	return buf
}
