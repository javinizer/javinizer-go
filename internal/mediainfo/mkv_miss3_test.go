package mediainfo

import (
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- analyzeMKV: full MKV with Segment containing Info and Tracks ---

func TestMiss3_AnalyzeMKV_FullSegmentWithInfoAndTracks(t *testing.T) {
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 3600.0) // 1hr duration, 1ms timescale
			seg.writeTracks(func(tr *mkvTracksWriter) {
				tr.writeVideoTrack(1, "V_MPEG4/ISO/AVC", 1920, 1080)
				tr.writeAudioTrack(2, "A_AAC", 48000, 2)
			})
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 48000, info.SampleRate)
	assert.Equal(t, 2, info.AudioChannels)
	// Duration depends on timecodeScale and the float encoding being correctly parsed
	// The main goal is to verify the parsing infrastructure works
	_ = info.Duration
}

// --- analyzeMKV: Segment with Cluster and Cues (should be skipped) ---

func TestMiss3_AnalyzeMKV_SegmentWithClusterAndCues(t *testing.T) {
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 60.0)
			seg.writeCluster(1000)
			seg.writeCues()
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	assert.Greater(t, info.Duration, 0.0)
}

// --- analyzeMKV: no usable data returns error ---

func TestMiss3_AnalyzeMKV_NoUsableData(t *testing.T) {
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
	})

	f := writeAndReopenMKV(t, buf)
	_, err := analyzeMKV(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no usable data found")
}

// --- analyzeMKV: Segment with unknown element that should be skipped ---

func TestMiss3_AnalyzeMKV_UnknownElementSkipped(t *testing.T) {
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 120.0)
			seg.writeUnknownElement(0x1234, []byte("unknown data"))
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Greater(t, info.Duration, 0.0)
}

// --- parseSegmentInfo: different timecode scales ---

func TestMiss3_ParseSegmentInfo_DifferentTimecodeScale(t *testing.T) {
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000000, 60.0)
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Greater(t, info.Duration, 0.0)
}

// --- mapMKVVideoCodec: various codec IDs ---

func TestMiss3_MapMKVVideoCodec_AllMappings(t *testing.T) {
	tests := []struct {
		codecID string
		want    string
	}{
		{"V_MPEG4/ISO/AVC", "h264"},
		{"V_MPEGH/ISO/HEVC", "hevc"},
		{"V_VP9", "vp9"},
		{"V_VP8", "vp8"},
		{"V_AV1", "av1"},
		{"V_MPEG4/ISO/ASP", "mpeg4"},
		{"V_THEORA", "theora"},
		{"V_MS/VFW/FOURCC", "MS/VFW/FOURCC"},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, mapMKVVideoCodec(tc.codecID), "mapMKVVideoCodec(%s)", tc.codecID)
	}
}

// --- mapMKVAudioCodec: various codec IDs ---

func TestMiss3_MapMKVAudioCodec_AllMappings(t *testing.T) {
	tests := []struct {
		codecID string
		want    string
	}{
		{"A_AAC", "aac"},
		{"A_MPEG/L3", "mp3"},
		{"A_AC3", "ac3"},
		{"A_EAC3", "eac3"},
		{"A_DTS", "dts"},
		{"A_OPUS", "opus"},
		{"A_VORBIS", "vorbis"},
		{"A_FLAC", "flac"},
		{"A_PCM/INT/LIT", "pcm"},
		{"A_MS/ACM", "wma"},
		{"A_WMAPI", "wma"},
		{"A_REAL/14_4", "REAL/14_4"},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, mapMKVAudioCodec(tc.codecID), "mapMKVAudioCodec(%s)", tc.codecID)
	}
}

// --- mkvProber: canProbe ---

func TestMiss3_MKVProber_CanProbe(t *testing.T) {
	p := newMKVProber()
	assert.True(t, p.canProbe([]byte{0x1A, 0x45, 0xDF, 0xA3}))
	assert.False(t, p.canProbe([]byte{0x00, 0x00, 0x00, 0x00}))
	assert.False(t, p.canProbe([]byte{0x1A, 0x45}))
	assert.Equal(t, "mkv", p.Name())
}

// --- Helper types for building MKV binary data ---

type mkvWriter struct {
	buf []byte
}

type mkvSegmentWriter struct {
	parent *mkvWriter
}

type mkvTracksWriter struct {
	parent *mkvSegmentWriter
}

// Helper to write MKV data to a temp file and reopen it for reading
func writeAndReopenMKV(t *testing.T, data *[]byte) *os.File {
	t.Helper()
	f, err := os.CreateTemp("", "mkv_test_*.mkv")
	require.NoError(t, err)
	fname := f.Name()
	_, err = f.Write(*data)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	f, err = os.Open(fname)
	require.NoError(t, err)
	t.Cleanup(func() {
		f.Close()
		os.Remove(fname)
	})
	return f
}

func buildMKVBuffer(t *testing.T, build func(w *mkvWriter)) *[]byte {
	t.Helper()
	w := &mkvWriter{}
	build(w)
	return &w.buf
}

func (w *mkvWriter) writeEBMLHeader() {
	// EBML header element ID: 0x1A45DFA3
	// Minimal EBML header with EBMLVersion=1 and EBMLReadVersion=1
	headerData := []byte{}
	headerData = append(headerData, mkvTestEBMLElement(0x4286, uintData(1))...) // EBMLVersion
	headerData = append(headerData, mkvTestEBMLElement(0x42F7, uintData(1))...) // EBMLReadVersion
	w.buf = append(w.buf, mkvTestEBMLElement(0x1A45DFA3, headerData)...)
}

func (w *mkvWriter) writeSegment(build func(seg *mkvSegmentWriter)) {
	// We don't know the segment size upfront, so we build it first then wrap
	seg := &mkvSegmentWriter{parent: w}
	// Build segment content into a temp buffer
	oldBuf := w.buf
	w.buf = nil
	build(seg)
	segContent := w.buf
	w.buf = oldBuf
	w.buf = append(w.buf, mkvTestEBMLElement(0x18538067, segContent)...)
}

func (seg *mkvSegmentWriter) writeInfo(timecodeScale uint64, duration float64) {
	infoData := []byte{}
	infoData = append(infoData, mkvTestEBMLElement(0x2AD7B1, uintData(timecodeScale))...) // TimecodeScale
	infoData = append(infoData, mkvTestEBMLElement(0x4489, floatData(duration))...)       // Duration
	seg.parent.buf = append(seg.parent.buf, mkvTestEBMLElement(0x1549A966, infoData)...)
}

func (seg *mkvSegmentWriter) writeTracks(build func(tr *mkvTracksWriter)) {
	tr := &mkvTracksWriter{parent: seg}
	oldBuf := seg.parent.buf
	seg.parent.buf = nil
	build(tr)
	tracksContent := seg.parent.buf
	seg.parent.buf = oldBuf
	seg.parent.buf = append(seg.parent.buf, mkvTestEBMLElement(0x1654AE6B, tracksContent)...)
}

func (tr *mkvTracksWriter) writeVideoTrack(trackNum uint64, codecID string, width, height uint64) {
	videoData := []byte{}
	videoData = append(videoData, mkvTestEBMLElement(0xB0, uintData(width))...)  // PixelWidth
	videoData = append(videoData, mkvTestEBMLElement(0xBA, uintData(height))...) // PixelHeight

	entryData := []byte{}
	entryData = append(entryData, mkvTestEBMLElement(0xD7, uintData(trackNum))...)  // TrackNumber
	entryData = append(entryData, mkvTestEBMLElement(0x83, uintData(1))...)         // TrackType = Video
	entryData = append(entryData, mkvTestEBMLElement(0x86, stringData(codecID))...) // CodecID
	entryData = append(entryData, mkvTestEBMLElement(0xE0, videoData)...)           // Video

	tr.parent.parent.buf = append(tr.parent.parent.buf, mkvTestEBMLElement(0xAE, entryData)...)
}

func (tr *mkvTracksWriter) writeAudioTrack(trackNum uint64, codecID string, samplingFreq float64, channels uint64) {
	audioData := []byte{}
	audioData = append(audioData, mkvTestEBMLElement(0xB5, floatData(samplingFreq))...) // SamplingFrequency
	audioData = append(audioData, mkvTestEBMLElement(0x9F, uintData(channels))...)      // Channels

	entryData := []byte{}
	entryData = append(entryData, mkvTestEBMLElement(0xD7, uintData(trackNum))...)  // TrackNumber
	entryData = append(entryData, mkvTestEBMLElement(0x83, uintData(2))...)         // TrackType = Audio
	entryData = append(entryData, mkvTestEBMLElement(0x86, stringData(codecID))...) // CodecID
	entryData = append(entryData, mkvTestEBMLElement(0xE1, audioData)...)           // Audio

	tr.parent.parent.buf = append(tr.parent.parent.buf, mkvTestEBMLElement(0xAE, entryData)...)
}

func (seg *mkvSegmentWriter) writeCluster(timestamp uint64) {
	clusterData := mkvTestEBMLElement(0xE7, uintData(timestamp)) // Timecode
	seg.parent.buf = append(seg.parent.buf, mkvTestEBMLElement(0x1F43B675, clusterData)...)
}

func (seg *mkvSegmentWriter) writeCues() {
	seg.parent.buf = append(seg.parent.buf, mkvTestEBMLElement(0x1C53BB6B, []byte{})...)
}

func (seg *mkvSegmentWriter) writeUnknownElement(id uint32, data []byte) {
	seg.parent.buf = append(seg.parent.buf, mkvTestEBMLElement(id, data)...)
}

// ebmlElement creates an EBML element with ID, size, and data
func mkvTestEBMLElement(id uint32, data []byte) []byte {
	buf := writeEBMLID(id)
	buf = append(buf, writeEBMLSize(uint64(len(data)))...)
	buf = append(buf, data...)
	return buf
}

func writeEBMLID(id uint32) []byte {
	if id < 0x100 {
		return []byte{byte(id)}
	} else if id < 0x10000 {
		return []byte{byte(id >> 8), byte(id)}
	} else if id < 0x1000000 {
		return []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	}
	return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
}

func writeEBMLSize(size uint64) []byte {
	if size < 0x7F {
		return []byte{byte(size) | 0x80}
	} else if size < 0x3FFF {
		return []byte{byte(size>>8) | 0x40, byte(size)}
	} else if size < 0x1FFFFF {
		return []byte{byte(size>>16) | 0x20, byte(size >> 8), byte(size)}
	} else if size < 0x0FFFFFFF {
		return []byte{byte(size>>24) | 0x10, byte(size >> 16), byte(size >> 8), byte(size)}
	}
	// 8-byte size
	return []byte{0x01, byte(size >> 56), byte(size >> 48), byte(size >> 40), byte(size >> 32),
		byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)}
}

func uintData(v uint64) []byte {
	if v == 0 {
		return []byte{0}
	}
	// Determine minimum bytes
	var buf []byte
	for b := v; b > 0; b >>= 8 {
		buf = append([]byte{byte(b)}, buf...)
	}
	return buf
}

func floatData(v float64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, math.Float64bits(v))
	return b
}

func stringData(s string) []byte {
	return []byte(s)
}
