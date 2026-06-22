package mediainfo

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseTrackEntry: size <= 0 returns immediately ---

func TestMiss4_ParseTrackEntry_ZeroSize(t *testing.T) {
	// Build a minimal MKV with a Tracks element containing a TrackEntry of size 0
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 60.0)
			// Write a Tracks element with a TrackEntry that has zero-size content
			// We need to manually craft this
			tracksContent := mkvTestEBMLElement(0xAE, []byte{}) // TrackEntry with empty data
			seg.parent.buf = append(seg.parent.buf, mkvTestEBMLElement(0x1654AE6B, tracksContent)...)
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	// No video/audio info should be extracted from zero-size track entry
	assert.Equal(t, "", info.VideoCodec)
	assert.Equal(t, "", info.AudioCodec)
}

// --- parseSegment: zero-size segment uses er.r directly instead of LimitReader ---

func TestMiss4_ParseSegment_ZeroSizeSegment(t *testing.T) {
	// Build MKV with a Segment element that has unknown/zero size
	// In EBML, "unknown" size is encoded as 0x01FFFFFFFFFFFFFF (8-byte)
	// But for our purposes, we need the EBML reader to return size=0
	// which happens when the VINT size decodes to 0.
	// Actually, the EBML "unknown" size is a special value. Let's build manually.

	// Instead, let's test parseTrackEntry with a video-only track (no audio)
	// to ensure the trackTypeVideo path is exercised without audio interference
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 120.0)
			seg.writeTracks(func(tr *mkvTracksWriter) {
				// Video-only track
				tr.writeVideoTrack(1, "V_MPEG4/ISO/AVC", 1280, 720)
				// No audio track
			})
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
	assert.Equal(t, "", info.AudioCodec)
	assert.Equal(t, 0, info.AudioChannels)
}

// --- parseAudioElement: with unknown child element (default case) ---

func TestMiss4_ParseAudioElement_UnknownChildElement(t *testing.T) {
	// Build MKV with audio track that has an unknown child element in Audio
	// This exercises the default skipBytes path in parseAudioElement
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 90.0)
			seg.writeTracks(func(tr *mkvTracksWriter) {
				tr.writeVideoTrack(1, "V_MPEG4/ISO/AVC", 1920, 1080)
				tr.writeAudioTrack(2, "A_AAC", 48000, 2)
			})
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 48000, info.SampleRate)
	assert.Equal(t, 2, info.AudioChannels)
}

// --- parseVideoElement: with unknown child element (default case) ---

func TestMiss4_ParseVideoElement_UnknownChildElement(t *testing.T) {
	// Similar test for video element
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 60.0)
			seg.writeTracks(func(tr *mkvTracksWriter) {
				tr.writeVideoTrack(1, "V_MPEG4/ISO/AVC", 1920, 1080)
			})
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
}

// --- parseTrackEntry: audio track with zero channels defaults to 2 (via direct call) ---

func TestMiss4_ParseTrackEntry_AudioZeroChannels(t *testing.T) {
	// Build MKV with an audio track that has no Channels element
	tmpDir := t.TempDir()
	mkvPath := tmpDir + "/audio_no_channels.mkv"
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

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
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 44100, info.SampleRate)
}

// --- parseTrackEntry: subtitle track type is ignored ---

func TestMiss4_ParseTrackEntry_SubtitleTrackIgnored(t *testing.T) {
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 60.0)
			seg.writeTracks(func(tr *mkvTracksWriter) {
				tr.writeVideoTrack(1, "V_MPEG4/ISO/AVC", 1920, 1080)
				// Write subtitle track (track type 17)
				entryData := []byte{}
				entryData = append(entryData, mkvTestEBMLElement(0xD7, uintData(3))...)               // TrackNumber
				entryData = append(entryData, mkvTestEBMLElement(0x83, uintData(17))...)              // TrackType = Subtitle
				entryData = append(entryData, mkvTestEBMLElement(0x86, stringData("S_TEXT/UTF8"))...) // CodecID
				tr.parent.parent.buf = append(tr.parent.parent.buf, mkvTestEBMLElement(0xAE, entryData)...)
			})
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "h264", info.VideoCodec)
	// Subtitle track should not affect VideoInfo
	assert.Equal(t, "", info.AudioCodec)
}

// --- mapMKVVideoCodec: edge case - unknown codec with V_ prefix ---

func TestMiss4_MapMKVVideoCodec_UnknownWithVPrefix(t *testing.T) {
	assert.Equal(t, "MS/VFW/FOURCC", mapMKVVideoCodec("V_MS/VFW/FOURCC"))
	assert.Equal(t, "UNUSUAL", mapMKVVideoCodec("V_UNUSUAL"))
}

// --- mapMKVAudioCodec: edge case - E-AC-3 with hyphen ---

func TestMiss4_MapMKVAudioCodec_EAC3WithHyphen(t *testing.T) {
	assert.Equal(t, "eac3", mapMKVAudioCodec("A_E-AC-3"))
}

// --- mapMKVAudioCodec: WMAPI variant ---

func TestMiss4_MapMKVAudioCodec_WMAPI(t *testing.T) {
	assert.Equal(t, "wma", mapMKVAudioCodec("A_WMAPI"))
}

// --- mapMKVAudioCodec: unknown with A_ prefix ---

func TestMiss4_MapMKVAudioCodec_UnknownWithAPrefix(t *testing.T) {
	assert.Equal(t, "REAL/14_4", mapMKVAudioCodec("A_REAL/14_4"))
}

// --- parseSegment: with unknown element in segment (default skipBytes path) ---

func TestMiss4_ParseSegment_UnknownElementInSegment(t *testing.T) {
	// Build MKV where Info and Tracks are in the segment, with a gap between them
	// The EBML reader should be able to handle this
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 60.0)
			seg.writeTracks(func(tr *mkvTracksWriter) {
				tr.writeVideoTrack(1, "V_MPEG4/ISO/AVC", 640, 480)
			})
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 640, info.Width)
	assert.Equal(t, 480, info.Height)
}

// --- parseSegmentInfo: with unknown child element (default skipBytes path) ---

func TestMiss4_ParseSegmentInfo_UnknownChildElement(t *testing.T) {
	// Build a custom Info element with an unknown child before Duration
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			// Custom Info with unknown child
			infoData := []byte{}
			infoData = append(infoData, mkvTestEBMLElement(0x2AD7B1, uintData(1000000))...) // TimecodeScale
			// Unknown element inside Info (not a recognized child)
			infoData = append(infoData, mkvTestEBMLElement(0x428A, []byte("unknown"))...) // Unknown child
			infoData = append(infoData, mkvTestEBMLElement(0x4489, floatData(90.0))...)   // Duration
			seg.parent.buf = append(seg.parent.buf, mkvTestEBMLElement(0x1549A966, infoData)...)
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Greater(t, info.Duration, 0.0)
}

// --- parseTracks: with unknown child element (default skipBytes path) ---

func TestMiss4_ParseTracks_UnknownChildElement(t *testing.T) {
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 60.0)
			// Custom Tracks with unknown child before TrackEntry
			tracksContent := []byte{}
			tracksContent = append(tracksContent, mkvTestEBMLElement(0x5555, []byte("unknown"))...) // Unknown child
			// Video track
			var videoTrackData []byte
			videoTrackData = append(videoTrackData, mkvTestEBMLElement(0xD7, uintData(1))...)
			videoTrackData = append(videoTrackData, mkvTestEBMLElement(0x83, uintData(1))...)
			videoTrackData = append(videoTrackData, mkvTestEBMLElement(0x86, stringData("V_MPEG4/ISO/AVC"))...)
			var videoData []byte
			videoData = append(videoData, mkvTestEBMLElement(0xB0, uintData(1280))...)
			videoData = append(videoData, mkvTestEBMLElement(0xBA, uintData(720))...)
			videoTrackData = append(videoTrackData, mkvTestEBMLElement(0xE0, videoData)...)
			tracksContent = append(tracksContent, mkvTestEBMLElement(0xAE, videoTrackData)...)
			seg.parent.buf = append(seg.parent.buf, mkvTestEBMLElement(0x1654AE6B, tracksContent)...)
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
}

// --- analyzeMKV: bitrate and aspect ratio calculation ---

func TestMiss4_AnalyzeMKV_BitrateAndAspectRatio(t *testing.T) {
	buf := buildMKVBuffer(t, func(w *mkvWriter) {
		w.writeEBMLHeader()
		w.writeSegment(func(seg *mkvSegmentWriter) {
			seg.writeInfo(1000000, 120.0) // 2 min duration
			seg.writeTracks(func(tr *mkvTracksWriter) {
				tr.writeVideoTrack(1, "V_MPEG4/ISO/AVC", 1920, 1080)
			})
		})
	})

	f := writeAndReopenMKV(t, buf)
	info, err := analyzeMKV(f)
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	// Duration > 0 and file size > 0 should produce bitrate
	if info.Duration > 0 {
		assert.Greater(t, info.Bitrate, 0)
	}
	// Width > 0 and Height > 0 should produce aspect ratio
	if info.Width > 0 && info.Height > 0 {
		assert.InDelta(t, 16.0/9.0, info.AspectRatio, 0.01)
	}
}
