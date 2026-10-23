package nfo

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func nativeAVIChunk(id string, body []byte) []byte {
	out := make([]byte, 8+len(body)+(len(body)%2))
	copy(out, id)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(body)))
	copy(out[8:], body)
	return out
}

// nativeAVIFixture is a header-only RIFF/AVI fixture for metadata edge cases.
// The playable fixture below separately exercises a complete container layout.
func nativeAVIFixture(video, audio bool, frames uint32) []byte {
	avih := make([]byte, 56)
	binary.LittleEndian.PutUint32(avih[0:4], 100000) //10fps
	binary.LittleEndian.PutUint32(avih[16:20], frames)
	streams := uint32(0)
	if video {
		streams++
		binary.LittleEndian.PutUint32(avih[32:36], 1920)
		binary.LittleEndian.PutUint32(avih[36:40], 1080)
	}
	if audio {
		streams++
	}
	binary.LittleEndian.PutUint32(avih[24:28], streams)
	hdrl := append([]byte("hdrl"), nativeAVIChunk("avih", avih)...)
	for _, kind := range []string{"vids", "auds"} {
		if kind == "vids" && !video || kind == "auds" && !audio {
			continue
		}
		strh := make([]byte, 56)
		copy(strh, kind)
		binary.LittleEndian.PutUint32(strh[20:24], 1)
		var format []byte
		if kind == "vids" {
			copy(strh[4:8], "H264")
			binary.LittleEndian.PutUint32(strh[24:28], 10)
			binary.LittleEndian.PutUint32(strh[32:36], frames)
			format = make([]byte, 40)
			binary.LittleEndian.PutUint32(format[0:4], 40)
			binary.LittleEndian.PutUint32(format[4:8], 1920)
			binary.LittleEndian.PutUint32(format[8:12], 1080)
			binary.LittleEndian.PutUint16(format[12:14], 1)
			binary.LittleEndian.PutUint16(format[14:16], 24)
			copy(format[16:20], "H264")
		} else {
			binary.LittleEndian.PutUint32(strh[24:28], 48000)
			format = make([]byte, 18)
			binary.LittleEndian.PutUint16(format[0:2], 0x00ff) //AAC
			binary.LittleEndian.PutUint16(format[2:4], 2)
			binary.LittleEndian.PutUint32(format[4:8], 48000)
		}
		list := append([]byte("strl"), nativeAVIChunk("strh", strh)...)
		list = append(list, nativeAVIChunk("strf", format)...)
		hdrl = append(hdrl, nativeAVIChunk("LIST", list)...)
	}
	body := append([]byte("AVI "), nativeAVIChunk("LIST", hdrl)...)
	return nativeAVIChunk("RIFF", body)
}

func TestDefaultAnalyzerMapsNativeAVIStreams(t *testing.T) {
	for _, tc := range []struct {
		name         string
		video, audio bool
		frames       uint32
	}{
		{"video and audio", true, true, 100}, {"video without duration", true, false, 0}, {"audio only", false, true, 0}, {"empty stream metadata", false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "metadata.avi")
			require.NoError(t, os.WriteFile(path, nativeAVIFixture(tc.video, tc.audio, tc.frames), 0600))
			details, err := (defaultMediaAnalyzer{}).Analyze(context.Background(), path)
			require.NoError(t, err)
			if !tc.video && !tc.audio {
				require.Nil(t, details)
				return
			}
			require.NotNil(t, details)
			if tc.video {
				require.Len(t, details.Video, 1)
				require.Equal(t, "h264", details.Video[0].Codec)
				require.Equal(t, 1920, details.Video[0].Width)
				require.Equal(t, 1080, details.Video[0].Height)
				require.InDelta(t, 16.0/9, details.Video[0].Aspect, 0.00001)
				require.Equal(t, int(tc.frames/10), details.Video[0].DurationInSeconds)
			} else {
				require.Empty(t, details.Video)
			}
			if tc.audio {
				require.Len(t, details.Audio, 1)
				require.Equal(t, "aac", details.Audio[0].Codec)
				require.Equal(t, 2, details.Audio[0].Channels)
			} else {
				require.Empty(t, details.Audio)
			}
		})
	}
}

func TestDefaultAnalyzerMapsPlayableAVI(t *testing.T) {
	// Checked-in synthetic black video and silent PCM audio, with actual movi
	// chunks and an index. No media executable is needed when running this test.
	details, err := (defaultMediaAnalyzer{}).Analyze(t.Context(), filepath.Join("testdata", "native-black-silence.avi"))
	require.NoError(t, err)
	require.NotNil(t, details)
	require.Len(t, details.Video, 1)
	require.Equal(t, "mjpeg", details.Video[0].Codec)
	require.Equal(t, 32, details.Video[0].Width)
	require.Equal(t, 24, details.Video[0].Height)
	require.InDelta(t, 4.0/3, details.Video[0].Aspect, 0.00001)
	require.Equal(t, 1, details.Video[0].DurationInSeconds)
	require.Len(t, details.Audio, 1)
	require.Equal(t, "pcm", details.Audio[0].Codec)
	require.Equal(t, 1, details.Audio[0].Channels)
}
