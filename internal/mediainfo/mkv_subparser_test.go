package mediainfo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeEBMLUint(id uint32, val uint64) []byte {
	return writeEBMLElement(id, encodeEBMLUint(val))
}

func writeEBMLFloat(id uint32, val float64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(val))
	return writeEBMLElement(id, buf[:])
}

func writeEBMLString(id uint32, val string) []byte {
	return writeEBMLElement(id, []byte(val))
}

func writeEBMLElement(id uint32, data []byte) []byte {
	var buf bytes.Buffer
	buf.Write(encodeEBMLID(id))
	buf.Write(encodeEBMLSize(uint64(len(data))))
	buf.Write(data)
	return buf.Bytes()
}

func encodeEBMLID(id uint32) []byte {
	switch {
	case id>>24 != 0:
		return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	case id>>16 != 0:
		leading := byte(id >> 16)
		if leading&0x20 != 0 {
			return []byte{leading, byte(id >> 8), byte(id)}
		}
		panic(fmt.Sprintf("invalid 3-byte EBML element ID: 0x%X", id))
	case id>>8 != 0:
		leading := byte(id >> 8)
		if leading&0x40 != 0 {
			return []byte{leading, byte(id)}
		}
		panic(fmt.Sprintf("invalid 2-byte EBML element ID: 0x%X", id))
	default:
		leading := byte(id)
		if leading&0x80 != 0 {
			return []byte{leading}
		}
		panic(fmt.Sprintf("invalid 1-byte EBML element ID: 0x%X", id))
	}
}

func encodeEBMLSize(size uint64) []byte {
	switch {
	case size < 0x80:
		return []byte{byte(size) | 0x80}
	case size < 0x4000:
		return []byte{byte(size>>8) | 0x40, byte(size)}
	case size < 0x200000:
		return []byte{byte(size>>16) | 0x20, byte(size >> 8), byte(size)}
	default:
		return []byte{byte(size>>24) | 0x10, byte(size >> 16), byte(size >> 8), byte(size)}
	}
}

func encodeEBMLUint(val uint64) []byte {
	if val == 0 {
		return []byte{0}
	}
	var buf []byte
	for v := val; v > 0; v >>= 8 {
		buf = append([]byte{byte(v)}, buf...)
	}
	return buf
}

func TestParseSegmentInfo(t *testing.T) {
	var data bytes.Buffer

	data.Write(writeEBMLUint(0x2AD7B1, 1000000)) // TimecodeScale = 1ms
	data.Write(writeEBMLFloat(0x4489, 120000.0)) // Duration = 120000ms = 120s

	rawData := data.Bytes()
	t.Logf("Raw data (%d bytes): %x", len(rawData), rawData)

	er := newEBMLReader(bytes.NewReader(rawData))
	elem, err := er.readElement()
	require.NoError(t, err, "first element read failed")
	t.Logf("First element: id=0x%X, size=%d, data=%v", elem.id, elem.size, elem.data)

	info := &VideoInfo{}
	er2 := newEBMLReader(bytes.NewReader(rawData))
	parseSegmentInfo(er2, int64(len(rawData)), info)

	assert.InDelta(t, 120.0, info.Duration, 0.01)
}

func TestParseSegmentInfo_CustomTimecodeScale(t *testing.T) {
	var data bytes.Buffer

	data.Write(writeEBMLUint(0x2AD7B1, 500000)) // TimecodeScale = 500000ns
	data.Write(writeEBMLFloat(0x4489, 60000.0)) // Duration = 60000 * 500000ns = 30s

	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(data.Bytes()))
	parseSegmentInfo(er, int64(data.Len()), info)

	assert.InDelta(t, 30.0, info.Duration, 0.01)
}

func TestParseSegmentInfo_Empty(t *testing.T) {
	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(nil))
	parseSegmentInfo(er, 0, info)
	assert.Equal(t, 0.0, info.Duration)
}

func TestParseTracks_VideoTrack(t *testing.T) {
	var trackEntry bytes.Buffer
	trackEntry.Write(writeEBMLUint(0xD7, 1))                   // TrackNumber
	trackEntry.Write(writeEBMLUint(0x83, 1))                   // TrackType = video
	trackEntry.Write(writeEBMLString(0x86, "V_MPEG4/ISO/AVC")) // CodecID

	var video bytes.Buffer
	video.Write(writeEBMLUint(0xB0, 1920))                  // PixelWidth
	video.Write(writeEBMLUint(0xBA, 1080))                  // PixelHeight
	trackEntry.Write(writeEBMLElement(0xE0, video.Bytes())) // Video element

	var data bytes.Buffer
	data.Write(writeEBMLElement(0xAE, trackEntry.Bytes())) // TrackEntry

	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(data.Bytes()))
	parseTracks(er, int64(data.Len()), info)

	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
}

func TestParseTracks_AudioTrack(t *testing.T) {
	var trackEntry bytes.Buffer
	trackEntry.Write(writeEBMLUint(0xD7, 2))         // TrackNumber
	trackEntry.Write(writeEBMLUint(0x83, 2))         // TrackType = audio
	trackEntry.Write(writeEBMLString(0x86, "A_AAC")) // CodecID

	var audio bytes.Buffer
	audio.Write(writeEBMLFloat(0xB5, 48000.0))              // SamplingFrequency
	audio.Write(writeEBMLUint(0x9F, 2))                     // Channels
	trackEntry.Write(writeEBMLElement(0xE1, audio.Bytes())) // Audio element

	var data bytes.Buffer
	data.Write(writeEBMLElement(0xAE, trackEntry.Bytes())) // TrackEntry

	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(data.Bytes()))
	parseTracks(er, int64(data.Len()), info)

	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 48000, info.SampleRate)
}

func TestParseTracks_VideoAndAudio(t *testing.T) {
	var videoTrack bytes.Buffer
	videoTrack.Write(writeEBMLUint(0xD7, 1))
	videoTrack.Write(writeEBMLUint(0x83, 1))
	videoTrack.Write(writeEBMLString(0x86, "V_MPEGH/ISO/HEVC"))
	var video bytes.Buffer
	video.Write(writeEBMLUint(0xB0, 3840))
	video.Write(writeEBMLUint(0xBA, 2160))
	videoTrack.Write(writeEBMLElement(0xE0, video.Bytes()))

	var audioTrack bytes.Buffer
	audioTrack.Write(writeEBMLUint(0xD7, 2))
	audioTrack.Write(writeEBMLUint(0x83, 2))
	audioTrack.Write(writeEBMLString(0x86, "A_AC3"))
	var audio bytes.Buffer
	audio.Write(writeEBMLFloat(0xB5, 48000.0))
	audio.Write(writeEBMLUint(0x9F, 6))
	audioTrack.Write(writeEBMLElement(0xE1, audio.Bytes()))

	var data bytes.Buffer
	data.Write(writeEBMLElement(0xAE, videoTrack.Bytes()))
	data.Write(writeEBMLElement(0xAE, audioTrack.Bytes()))

	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(data.Bytes()))
	parseTracks(er, int64(data.Len()), info)

	assert.Equal(t, "hevc", info.VideoCodec)
	assert.Equal(t, 3840, info.Width)
	assert.Equal(t, 2160, info.Height)
	assert.Equal(t, "ac3", info.AudioCodec)
	assert.Equal(t, 6, info.AudioChannels)
}

func TestParseTracks_Empty(t *testing.T) {
	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(nil))
	parseTracks(er, 0, info)
	assert.Equal(t, "", info.VideoCodec)
	assert.Equal(t, "", info.AudioCodec)
}

func TestParseTrackEntry_SubtitleSkipped(t *testing.T) {
	var trackEntry bytes.Buffer
	trackEntry.Write(writeEBMLUint(0xD7, 3))
	trackEntry.Write(writeEBMLUint(0x83, 17)) // subtitle
	trackEntry.Write(writeEBMLString(0x86, "S_TEXT/UTF8"))

	var data bytes.Buffer
	data.Write(writeEBMLElement(0xAE, trackEntry.Bytes()))

	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(data.Bytes()))
	parseTracks(er, int64(data.Len()), info)

	assert.Equal(t, "", info.VideoCodec)
	assert.Equal(t, "", info.AudioCodec)
}

func TestParseVideoElement(t *testing.T) {
	var data bytes.Buffer
	data.Write(writeEBMLUint(0xB0, 1280)) // PixelWidth
	data.Write(writeEBMLUint(0xBA, 720))  // PixelHeight

	var pw, ph uint64
	er := newEBMLReader(bytes.NewReader(data.Bytes()))
	parseVideoElement(er, int64(data.Len()), &pw, &ph)

	assert.Equal(t, uint64(1280), pw)
	assert.Equal(t, uint64(720), ph)
}

func TestParseVideoElement_Empty(t *testing.T) {
	var pw, ph uint64
	er := newEBMLReader(bytes.NewReader(nil))
	parseVideoElement(er, 0, &pw, &ph)
	assert.Equal(t, uint64(0), pw)
	assert.Equal(t, uint64(0), ph)
}

func TestParseAudioElement(t *testing.T) {
	var data bytes.Buffer
	data.Write(writeEBMLFloat(0xB5, 44100.0)) // SamplingFrequency
	data.Write(writeEBMLUint(0x9F, 6))        // Channels

	var sf float64
	var ch uint64
	er := newEBMLReader(bytes.NewReader(data.Bytes()))
	parseAudioElement(er, int64(data.Len()), &sf, &ch)

	assert.InDelta(t, 44100.0, sf, 0.1)
	assert.Equal(t, uint64(6), ch)
}

func TestParseAudioElement_DefaultChannels(t *testing.T) {
	var trackEntry bytes.Buffer
	trackEntry.Write(writeEBMLUint(0xD7, 2))
	trackEntry.Write(writeEBMLUint(0x83, 2))
	trackEntry.Write(writeEBMLString(0x86, "A_AAC"))

	var audio bytes.Buffer
	audio.Write(writeEBMLFloat(0xB5, 48000.0))
	trackEntry.Write(writeEBMLElement(0xE1, audio.Bytes()))

	var data bytes.Buffer
	data.Write(writeEBMLElement(0xAE, trackEntry.Bytes()))

	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(data.Bytes()))
	parseTracks(er, int64(data.Len()), info)

	assert.Equal(t, 2, info.AudioChannels) // default
}

func TestParseAudioElement_Empty(t *testing.T) {
	var sf float64
	var ch uint64
	er := newEBMLReader(bytes.NewReader(nil))
	parseAudioElement(er, 0, &sf, &ch)
	assert.Equal(t, 0.0, sf)
	assert.Equal(t, uint64(0), ch)
}

func TestParseSegment_WithInfoAndTracks(t *testing.T) {
	var segInfo bytes.Buffer
	segInfo.Write(writeEBMLUint(0x2AD7B1, 1000000))
	segInfo.Write(writeEBMLFloat(0x4489, 90000.0))

	var videoTrack bytes.Buffer
	videoTrack.Write(writeEBMLUint(0xD7, 1))
	videoTrack.Write(writeEBMLUint(0x83, 1))
	videoTrack.Write(writeEBMLString(0x86, "V_VP9"))
	var video bytes.Buffer
	video.Write(writeEBMLUint(0xB0, 3840))
	video.Write(writeEBMLUint(0xBA, 2160))
	videoTrack.Write(writeEBMLElement(0xE0, video.Bytes()))

	var tracks bytes.Buffer
	tracks.Write(writeEBMLElement(0xAE, videoTrack.Bytes()))

	var segment bytes.Buffer
	segment.Write(writeEBMLElement(0x1549A966, segInfo.Bytes())) // Info
	segment.Write(writeEBMLElement(0x1654AE6B, tracks.Bytes()))  // Tracks

	info := &VideoInfo{Container: "mkv"}
	er := newEBMLReader(bytes.NewReader(segment.Bytes()))
	parseSegment(er, int64(segment.Len()), info)

	assert.InDelta(t, 90.0, info.Duration, 0.01)
	assert.Equal(t, "vp9", info.VideoCodec)
	assert.Equal(t, 3840, info.Width)
	assert.Equal(t, 2160, info.Height)
}

func TestAnalyzeMKV_Synthetic(t *testing.T) {
	var ebmlHeader bytes.Buffer
	ebmlHeader.Write(writeEBMLUint(0x4286, 1)) // EBMLVersion
	ebmlHeader.Write(writeEBMLUint(0x42F7, 1)) // EBMLReadVersion

	var segInfo bytes.Buffer
	segInfo.Write(writeEBMLUint(0x2AD7B1, 1000000))
	segInfo.Write(writeEBMLFloat(0x4489, 60000.0))

	var videoTrack bytes.Buffer
	videoTrack.Write(writeEBMLUint(0xD7, 1))
	videoTrack.Write(writeEBMLUint(0x83, 1))
	videoTrack.Write(writeEBMLString(0x86, "V_MPEG4/ISO/AVC"))
	var video bytes.Buffer
	video.Write(writeEBMLUint(0xB0, 1920))
	video.Write(writeEBMLUint(0xBA, 1080))
	videoTrack.Write(writeEBMLElement(0xE0, video.Bytes()))

	var audioTrack bytes.Buffer
	audioTrack.Write(writeEBMLUint(0xD7, 2))
	audioTrack.Write(writeEBMLUint(0x83, 2))
	audioTrack.Write(writeEBMLString(0x86, "A_AAC"))
	var audio bytes.Buffer
	audio.Write(writeEBMLFloat(0xB5, 48000.0))
	audio.Write(writeEBMLUint(0x9F, 2))
	audioTrack.Write(writeEBMLElement(0xE1, audio.Bytes()))

	var tracks bytes.Buffer
	tracks.Write(writeEBMLElement(0xAE, videoTrack.Bytes()))
	tracks.Write(writeEBMLElement(0xAE, audioTrack.Bytes()))

	var segment bytes.Buffer
	segment.Write(writeEBMLElement(0x1549A966, segInfo.Bytes()))
	segment.Write(writeEBMLElement(0x1654AE6B, tracks.Bytes()))

	var file bytes.Buffer
	file.Write(writeEBMLElement(0x1A45DFA3, ebmlHeader.Bytes()))
	file.Write(writeEBMLElement(0x18538067, segment.Bytes()))

	tmpDir := t.TempDir()
	path := tmpDir + "/test.mkv"
	require.NoError(t, writeTestFile(path, file.Bytes()))

	f, err := openFile(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.InDelta(t, 60.0, info.Duration, 0.01)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 48000, info.SampleRate)
}

func writeTestFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

func openFile(path string) (*os.File, error) {
	return os.Open(path)
}
