package mediainfo

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/stretchr/testify/require"
)

var errParserSeek = errors.New("injected parser seek failure")

type parserSeekFailure struct {
	*bytes.Reader
	fail func(int64, int) bool
}

func (r *parserSeekFailure) Seek(offset int64, whence int) (int64, error) {
	if r.fail(offset, whence) {
		return 0, errParserSeek
	}
	return r.Reader.Seek(offset, whence)
}

func riffFailureChunk(kind string, size uint32, data []byte) []byte {
	out := make([]byte, 8, 8+len(data))
	copy(out, kind)
	binary.LittleEndian.PutUint32(out[4:], size)
	return append(out, data...)
}

func TestAVIPropagatesChunkFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunk  []byte
		offset int64
		whence int
		want   string
	}{
		{"list type truncated", riffFailureChunk("LIST", 4, nil), -1, 0, "failed to read list type: EOF"},
		{"hdrl end seek", riffFailureChunk("LIST", 4, []byte("hdrl")), 24, io.SeekStart, "failed to seek to end of hdrl list: injected parser seek failure"},
		{"stream end seek", riffFailureChunk("LIST", 4, []byte("strl")), 24, io.SeekStart, "failed to seek to end of stream list: injected parser seek failure"},
		{"unknown list seek", riffFailureChunk("LIST", 4, []byte("movi")), 24, io.SeekStart, "failed to seek over list chunk: injected parser seek failure"},
		{"main header truncated", riffFailureChunk("avih", 56, nil), -1, 0, "failed to read avih: EOF"},
		{"unknown chunk seek", riffFailureChunk("JUNK", 2, []byte{0, 0}), 22, io.SeekStart, "failed to seek over unknown chunk: injected parser seek failure"},
		{"alignment seek", riffFailureChunk("JUNK", 1, []byte{0}), 1, io.SeekCurrent, "failed to align to word boundary: injected parser seek failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append(riffFailureChunk("RIFF", 0, []byte("AVI ")), tc.chunk...)
			r := &parserSeekFailure{bytes.NewReader(data), func(o int64, w int) bool { return o == tc.offset && w == tc.whence }}
			info, err := analyzeAVI(r)
			require.Nil(t, info)
			require.EqualError(t, err, tc.want)
			if tc.offset >= 0 {
				require.ErrorIs(t, err, errParserSeek)
			}
		})
	}
}

func TestAVISubparserFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		data       []byte
		failOffset int64
		want       string
	}{
		{"truncated main header", riffFailureChunk("avih", 56, nil), -1, "failed to read avih: EOF"},
		{"truncated stream header", riffFailureChunk("LIST", 16, append([]byte("strl"), []byte("strh")...)), -1, "failed to read chunk in strl: unexpected EOF"},
		{"list end seek", riffFailureChunk("LIST", 4, []byte("movi")), 4, "failed to seek to end of list: injected parser seek failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &parserSeekFailure{bytes.NewReader(tc.data), func(o int64, w int) bool { return o == tc.failOffset && w == io.SeekStart }}
			info := &VideoInfo{}
			v, a := false, false
			err := parseHdrlList(r, info, 0, 100, &v, &a)
			require.EqualError(t, err, tc.want)
			require.Equal(t, &VideoInfo{}, info)
			require.False(t, v)
			require.False(t, a)
		})
	}
	t.Run("empty stream ends at EOF", func(t *testing.T) {
		stream, err := parseStrlList(bytes.NewReader(nil), 0, 10)
		require.NoError(t, err)
		require.Equal(t, &streamInfo{}, stream)
	})
	t.Run("format end seek", func(t *testing.T) {
		r := &parserSeekFailure{bytes.NewReader(riffFailureChunk("strf", 0, nil)), func(o int64, w int) bool { return o == 8 && w == io.SeekStart }}
		stream, err := parseStrlList(r, 0, 8)
		require.Nil(t, stream)
		require.EqualError(t, err, "failed to seek to end of strl chunk: injected parser seek failure")
		require.ErrorIs(t, err, errParserSeek)
	})
}

func TestMKVTruncatedUnrecognizedMasterChildren(t *testing.T) {
	// A Video master is not valid in these contexts. Its payload is intentionally
	// absent, forcing the default master-skip path rather than scalar decoding.
	data := []byte{0xe0, 0x81}
	for _, tc := range []struct {
		name  string
		parse func(*ebmlReader, int64, *VideoInfo)
	}{
		{"segment", parseSegment}, {"info", parseSegmentInfo}, {"tracks", parseTracks}, {"entry", parseTrackEntry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// TrackEntry recognizes Video; use Segment there instead.
			input := data
			if tc.name == "entry" {
				input = []byte{0x18, 0x53, 0x80, 0x67, 0x81}
			}
			info := &VideoInfo{Duration: 12, Width: 640}
			r := bytes.NewReader(input)
			tc.parse(newEBMLReader(r), 100, info)
			require.Equal(t, &VideoInfo{Duration: 12, Width: 640}, info)
			require.Zero(t, r.Len())
		})
	}
	t.Run("video", func(t *testing.T) {
		var width, height uint64 = 640, 480
		parseVideoElement(newEBMLReader(bytes.NewReader(data)), 100, &width, &height)
		require.Equal(t, uint64(640), width)
		require.Equal(t, uint64(480), height)
	})
	t.Run("audio", func(t *testing.T) {
		freq, channels := 44100.0, uint64(2)
		parseAudioElement(newEBMLReader(bytes.NewReader(data)), 100, &freq, &channels)
		require.Equal(t, 44100.0, freq)
		require.Equal(t, uint64(2), channels)
	})
	t.Run("cluster in segment", func(t *testing.T) {
		info := &VideoInfo{Width: 640}
		parseSegment(newEBMLReader(bytes.NewReader([]byte{0x1f, 0x43, 0xb6, 0x75, 0x81})), 100, info)
		require.Equal(t, &VideoInfo{Width: 640}, info)
	})
}

func TestMKVTopLevelSkipFailure(t *testing.T) {
	for _, id := range [][]byte{{0x1f, 0x43, 0xb6, 0x75}, {0xe0}} {
		data := append(append([]byte{}, id...), 0x81)
		r := &parserSeekFailure{bytes.NewReader(data), func(o int64, w int) bool { return w == io.SeekCurrent && o == 1 }}
		info, err := analyzeMKV(r)
		require.Nil(t, info)
		require.EqualError(t, err, "failed to extract MKV metadata: no usable data found")
	}
}

func TestMP4CodecConfigurationOverrides(t *testing.T) {
	for _, tc := range []struct{ box, codec string }{{"avcC", "h264"}, {"hvcC", "hevc"}, {"vpcC", "vp9"}} {
		t.Run(tc.box, func(t *testing.T) {
			entry := mp4.CreateVisualSampleEntryBox("mp4v", 1280, 720, nil)
			switch tc.box {
			case "avcC":
				entry.AddChild(&mp4.AvcCBox{})
			case "hvcC":
				entry.AddChild(&mp4.HvcCBox{})
			case "vpcC":
				entry.AddChild(&mp4.VppCBox{})
			}
			trak := &mp4.TrakBox{Mdia: &mp4.MdiaBox{Minf: &mp4.MinfBox{Stbl: &mp4.StblBox{Stsd: &mp4.StsdBox{Children: []mp4.Box{entry}}}}}}
			info := &VideoInfo{}
			require.NoError(t, extractMP4VideoInfo(trak, info))
			require.Equal(t, &VideoInfo{VideoCodec: tc.codec, Width: 1280, Height: 720}, info)
		})
	}
	entry := mp4.CreateAudioSampleEntryBox("ac-3", 2, 16, 48000, nil)
	entry.AddChild(&mp4.EsdsBox{})
	trak := &mp4.TrakBox{Mdia: &mp4.MdiaBox{Minf: &mp4.MinfBox{Stbl: &mp4.StblBox{Stsd: &mp4.StsdBox{Children: []mp4.Box{entry}}}}}}
	info := &VideoInfo{}
	require.NoError(t, extractMP4AudioInfo(trak, info))
	require.Equal(t, &VideoInfo{AudioCodec: "aac", AudioChannels: 2, SampleRate: 48000}, info)
	require.False(t, isFieldBasedStream(&mp4.SttsBox{SampleCount: []uint32{0}, SampleTimeDelta: []uint32{1000}}, 30000))
}

func TestMP4FallbackMalformedBoxes(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		fail bool
		want error
	}{
		{"extended size truncated", []byte{0, 0, 0, 1, 'f', 'r', 'e', 'e'}, false, io.EOF},
		{"payload truncated", []byte{0, 0, 0, 10, 'f', 'r', 'e', 'e', 0}, false, io.ErrUnexpectedEOF},
		{"size to EOF seek failure", []byte{0, 0, 0, 0, 'f', 'r', 'e', 'e'}, true, errParserSeek},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &parserSeekFailure{bytes.NewReader(tc.data), func(o int64, w int) bool { return tc.fail && w == io.SeekCurrent }}
			box, err := newMP4BoxReader(r).readBox()
			require.Nil(t, box)
			require.ErrorIs(t, err, tc.want)
			require.EqualError(t, err, tc.want.Error())
		})
	}
	require.Empty(t, findBoxes([]byte{0, 0, 0, 7, 't', 'r', 'a', 'k'}, "trak"))
	codec, channels, rate := parseStsdAudio([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	require.Empty(t, codec)
	require.Zero(t, channels)
	require.Zero(t, rate)
}

func TestMediaAnalysisRejectsNilContext(t *testing.T) {
	info, err := Analyze(nil, "nonexistent.mp4") //nolint:staticcheck // Deliberately exercise the public nil-context guard.
	require.Nil(t, info)
	require.EqualError(t, err, "context cannot be nil")
}

func TestMOVPropagatesMalformedMP4(t *testing.T) {
	info, err := newMOVProber().Probe(context.Background(), bytes.NewReader([]byte{0}))
	require.Nil(t, info)
	require.EqualError(t, err, "failed to extract MP4/MOV metadata: no usable data found")
}

func TestProberPropagatesResetFailure(t *testing.T) {
	r := &parserSeekFailure{bytes.NewReader(make([]byte, 16)), func(o int64, w int) bool { return w == io.SeekStart }}
	info, err := newProberRegistry(&mediaInfoConfig{}).probeWithFallback(context.Background(), r)
	require.Nil(t, info)
	require.ErrorIs(t, err, errParserSeek)
	require.EqualError(t, err, "failed to reset file pointer: injected parser seek failure")
}

func TestAVIRejectsUndersizedLists(t *testing.T) {
	for size := uint32(0); size < 4; size++ {
		chunk := riffFailureChunk("LIST", size, nil)
		info, err := analyzeAVI(bytes.NewReader(append(riffFailureChunk("RIFF", 0, []byte("AVI ")), chunk...)))
		require.Nil(t, info)
		require.EqualError(t, err, fmt.Sprintf("invalid LIST chunk size %d", size))
		v, a := false, false
		out := &VideoInfo{}
		err = parseHdrlList(bytes.NewReader(chunk), out, 0, uint32(len(chunk)), &v, &a)
		require.EqualError(t, err, fmt.Sprintf("invalid LIST chunk size %d", size))
		require.Equal(t, &VideoInfo{}, out)
		require.False(t, v)
		require.False(t, a)
	}
}

func TestMP4EmptyAudioDescription(t *testing.T) {
	codec, channels, rate := parseStsdAudio(make([]byte, 8))
	require.Empty(t, codec)
	require.Zero(t, channels)
	require.Zero(t, rate)
}

func TestMediaAnalysisRejectsCanceledContextBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info, err := Analyze(ctx, "nonexistent.mp4")
	require.Nil(t, info)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualError(t, err, "media analysis canceled before opening file: context canceled")
}

// Document the existing VINT boundary behavior without changing it for coverage.
// The unknown-size sentinel in readVintSize exceeds the maximum encodable value
// at every supported length, so neither that branch nor readElement's negative
// scalar size guard can be reached through a byte stream.
func TestEBMLMaximumEncodedVintSizes(t *testing.T) {
	for length := 1; length <= 8; length++ {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			data := bytes.Repeat([]byte{0xff}, length)
			data[0] = byte(0xff >> uint(length-1))
			size, n, err := readVintSize(bytes.NewReader(data))
			require.NoError(t, err)
			require.Equal(t, length, n)
			require.Equal(t, int64(uint64(1)<<uint(7*length)-1), size)
		})
	}
}
