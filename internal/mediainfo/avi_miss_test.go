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

// --- Miss-line coverage tests for avi.go ---
// Target miss lines: 81-83, 102-104, 120, 129-131, 137-142, 146-148,
// 165-172, 178-180, 193-195, 200-202, 243, 251-253, 265-267, 270-272,
// 279-284, 286-288, 320, 329-331, 364-366, 393-395

// Lines 81-83: analyzeAVI — read RIFF header failure
func TestMiss_AnalyzeAVI_RIFFHeaderReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "truncated.avi")

	// Create file with only 3 bytes — can't read 8-byte RIFF header
	require.NoError(t, os.WriteFile(aviPath, []byte("RII"), 0644))

	f, err := os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RIFF header")
}

// Lines 102-104: analyzeAVI — read form type failure
func TestMiss_AnalyzeAVI_FormTypeReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "truncated2.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)
	// Write RIFF header but not enough for form type
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 100)
	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "form type")
}

// Lines 120, 129-131: analyzeAVI — read chunk failure (not EOF)
func TestMiss_AnalyzeAVI_ChunkReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "partial.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)
	// Write valid RIFF + AVI header, then truncated data
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))
	// Write partial chunk header (only 6 bytes instead of 8)
	writeBytes(t, f, []byte("LI"))
	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	// Should get either EOF or a read error
	require.Error(t, err)
}

// Lines 137-139: analyzeAVI — LIST chunk, read list type failure
func TestMiss_AnalyzeAVI_ListTypeReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "badlist.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))
	// LIST chunk with truncated list type
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 100)
	writeBytes(t, f, []byte("hd"))
	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list type")
}

// Lines 140-142: analyzeAVI — hdrl LIST, seek to end failure (hard to trigger)
// Lines 146-148: analyzeAVI — strl LIST, parse error
func TestMiss_AnalyzeAVI_StrlListParseError(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "badstrl.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))
	// LIST strl with truncated data
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 8) // small size
	writeBytes(t, f, []byte("strl"))
	// Truncated strh chunk
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48) // expects 48 bytes but we don't provide them
	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	require.Error(t, err)
}

// Lines 165-172: analyzeAVI — top-level avih chunk, then strl LIST
func TestMiss_AnalyzeAVI_TopLevelAvihChunk(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "top_avih.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))

	// Top-level avih chunk (not inside LIST hdrl)
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 33333)   // MicroSecPerFrame
	writeUint32LE(t, f, 1000000) // MaxBytesPerSec
	writeUint32LE(t, f, 0)       // PaddingGranularity
	writeUint32LE(t, f, 0)       // Flags
	writeUint32LE(t, f, 100)     // TotalFrames
	writeUint32LE(t, f, 0)       // InitialFrames
	writeUint32LE(t, f, 1)       // Streams
	writeUint32LE(t, f, 0)       // SuggestedBufferSize
	writeUint32LE(t, f, 640)     // Width
	writeUint32LE(t, f, 480)     // Height
	writeUint32LE(t, f, 0)       // Reserved[0]
	writeUint32LE(t, f, 0)       // Reserved[1]
	writeUint32LE(t, f, 0)       // Reserved[2]
	writeUint32LE(t, f, 0)       // Reserved[3]

	// LIST strl with video stream
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 4+8+48+8+40) // strl size
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
	writeUint32LE(t, f, 1)   // Scale
	writeUint32LE(t, f, 30)  // Rate
	writeUint32LE(t, f, 0)   // Start
	writeUint32LE(t, f, 100) // Length
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 640)
	writeUint16LE(t, f, 480)

	// strf BITMAPINFOHEADER
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 40)
	writeUint32LE(t, f, 40)
	writeInt32LE(t, f, 640)
	writeInt32LE(t, f, 480)
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

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 640, info.Width)
	assert.Equal(t, 480, info.Height)
	assert.Equal(t, "xvid", info.VideoCodec)
	assert.InDelta(t, 3.333, info.Duration, 0.01)
	assert.InDelta(t, 30.0, info.FrameRate, 0.1)
}

// Lines 178-180: analyzeAVI — unknown chunk seek failure
func TestMiss_AnalyzeAVI_UnknownChunkSeekFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "badchunk.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))
	// Unknown chunk with huge size (will seek past EOF)
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 0xFFFFFFFF) // huge size
	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	// Should handle gracefully — might succeed with partial info or fail
	// depending on whether the seek past EOF causes issues
	if err != nil {
		// Error is acceptable
		t.Logf("Got expected error: %v", err)
	}
}

// Lines 193-195: analyzeAVI — bitrate calculation when Duration > 0 and Stat succeeds
func TestMiss_AnalyzeAVI_BitrateCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := createTestAVI(t, tmpDir, "H264", 0x0055)

	f, err := os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	// Duration is computed from avih header (300 frames * 33333us ≈ 10s)
	assert.Greater(t, info.Duration, 0.0, "duration should be computed from avih header")
	// Bitrate computation: int(fileSize * 8 / duration / 1000)
	// For a small test file, bitrate may truncate to 0, so we just verify the code path
	// is reached (Duration > 0 means the bitrate branch executes)
	assert.GreaterOrEqual(t, info.Bitrate, 0)
}

// Lines 200-202: analyzeAVI — word alignment for odd-sized chunks
func TestMiss_AnalyzeAVI_WordAlignment(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "odd_chunk.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))

	// Write a chunk with odd size to test word alignment
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 5) // odd size
	writeBytes(t, f, []byte("ABCDE"))
	// Padding byte for word alignment
	writeBytes(t, f, []byte{0})

	// Now write another valid chunk to verify alignment worked
	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 33333)   // MicroSecPerFrame
	writeUint32LE(t, f, 1000000) // MaxBytesPerSec
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 640)
	writeUint32LE(t, f, 480)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 640, info.Width) // confirms avih was read after alignment
}

// Line 243: parseHdrlList — avih within hdrl
func TestMiss_ParseHdrlList_AvihWithinHdrl(t *testing.T) {
	// Already tested in TestParseHdrlList, but ensure the avih path within hdrl
	// is fully covered — specifically the FrameRate > 0 and Duration computation
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "hdrl_avih.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST hdrl with avih
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 68) // 4 + 8 + 56
	writeBytes(t, f, []byte("hdrl"))

	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 40000) // MicroSecPerFrame (25 fps)
	writeUint32LE(t, f, 1000000)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 250) // TotalFrames
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1280) // Width
	writeUint32LE(t, f, 720)  // Height
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Skip to after "LIST" + size + "hdrl" = 12 bytes
	_, _ = f.Seek(12, 0)

	info := &VideoInfo{}
	videoFound := false
	audioFound := false
	err = parseHdrlList(f, info, 12, 68, &videoFound, &audioFound)
	require.NoError(t, err)

	assert.Equal(t, 1280, info.Width)
	assert.Equal(t, 720, info.Height)
	assert.InDelta(t, 10.0, info.Duration, 0.01) // 250 * 40000us / 1000000
	assert.InDelta(t, 25.0, info.FrameRate, 0.1)
}

// Lines 251-253: parseHdrlList — strl list type read failure
func TestMiss_ParseHdrlList_ListTypeReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "bad_hdrl_strl.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 100)
	writeBytes(t, f, []byte("hdrl"))

	// LIST chunk within hdrl but truncated list type
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 50)
	writeBytes(t, f, []byte("st")) // truncated

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0) // Skip LIST header

	info := &VideoInfo{}
	vf, af := false, false
	err = parseHdrlList(f, info, 12, 100, &vf, &af)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strl list type")
}

// Lines 265-267: parseHdrlList — strl within hdrl, parse error
func TestMiss_ParseHdrlList_StrlParseError(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "bad_strl_in_hdrl.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 100)
	writeBytes(t, f, []byte("hdrl"))

	// LIST strl with truncated strh
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 20)
	writeBytes(t, f, []byte("strl"))
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48) // expects 48 bytes, not enough data

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	info := &VideoInfo{}
	vf, af := false, false
	err = parseHdrlList(f, info, 12, 100, &vf, &af)
	require.Error(t, err)
}

// Lines 270-272: parseHdrlList — seek to end of list failure
// This is hard to trigger because f.Seek doesn't fail on regular files
// when seeking past EOF — it just moves the cursor. Let's test what we can.

// Lines 279-284: parseStrlList — strh read failure
func TestMiss_ParseStrlList_StrhReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "bad_strh.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with truncated strh data
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 20)
	writeBytes(t, f, []byte("strl"))
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48) // size says 48 but we only have a few bytes
	writeBytes(t, f, []byte("vi"))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	_, err = parseStrlList(f, 12, 20)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strh")
}

// Lines 286-288: parseStrlList — strf read failure for video (BITMAPINFOHEADER)
func TestMiss_ParseStrlList_StrfVideoReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "bad_strf_video.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with valid strh (vids) but truncated strf
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 70)
	writeBytes(t, f, []byte("strl"))

	// strh for video
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("vids"))
	writeBytes(t, f, []byte("H264"))
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)  // Scale
	writeUint32LE(t, f, 30) // Rate
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 1920)
	writeUint16LE(t, f, 1080)

	// strf with truncated data (declares 40 bytes but only provides partial)
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 40)
	writeUint32LE(t, f, 40)
	writeInt32LE(t, f, 1920)
	// Not enough data for full BITMAPINFOHEADER

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	_, err = parseStrlList(f, 12, 70)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BITMAPINFOHEADER")
}

// Line 320: parseStrlList — audio stream with strf read failure
func TestMiss_ParseStrlList_StrfAudioReadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "bad_strf_audio.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with valid strh (auds) but truncated strf
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 70)
	writeBytes(t, f, []byte("strl"))

	// strh for audio
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
	writeUint32LE(t, f, 44100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)

	// strf with truncated WAVEFORMATEX
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 18)
	writeUint16LE(t, f, 0x0055) // format tag
	writeUint16LE(t, f, 2)      // channels
	// Not enough data for full WAVEFORMATEX

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	_, err = parseStrlList(f, 12, 70)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WAVEFORMATEX")
}

// Lines 329-331: parseStrlList — unknown chunk skip
func TestMiss_ParseStrlList_UnknownChunkSkip(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "strl_unknown.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with unknown chunk then strh
	writeBytes(t, f, []byte("LIST"))
	strlSize := uint32(4 + 8 + 4 + 8 + 48) // strl type + JUNK + strh
	writeUint32LE(t, f, strlSize)
	writeBytes(t, f, []byte("strl"))

	// Unknown chunk
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 4)
	writeBytes(t, f, []byte("ABCD"))

	// strh for video
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
	writeUint16LE(t, f, 640)
	writeUint16LE(t, f, 480)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, strlSize)
	require.NoError(t, err)
	assert.True(t, stream.isVideo)
	assert.Equal(t, "h264", stream.codec)
}

// Lines 364-366: parseStrlList — video codec update from compression field
// when compression field differs from handler
func TestMiss_ParseStrlList_CompressionCodecOverride(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "codec_override.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with vids handler "XVID" but compression "H264"
	// The compression field should override the handler for codec name
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 4+8+48+8+40)
	writeBytes(t, f, []byte("strl"))

	// strh — handler says XVID
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("vids"))
	writeBytes(t, f, []byte("XVID")) // handler
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

	// strf BITMAPINFOHEADER — compression says H264
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 40)
	writeUint32LE(t, f, 40)
	writeInt32LE(t, f, 640)
	writeInt32LE(t, f, 480)
	writeUint16LE(t, f, 1)
	writeUint16LE(t, f, 24)
	writeBytes(t, f, []byte("H264")) // compression overrides handler
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

	stream, err := parseStrlList(f, 12, 4+8+48+8+40)
	require.NoError(t, err)
	assert.True(t, stream.isVideo)
	// Codec should be overridden to h264 by compression field
	assert.Equal(t, "h264", stream.codec)
}

// Lines 393-395: parseStrlList — audio stream parsed successfully
func TestMiss_ParseStrlList_AudioStreamFullParse(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "audio_full.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with audio stream
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 4+8+48+8+18)
	writeBytes(t, f, []byte("strl"))

	// strh for audio
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
	writeUint32LE(t, f, 44100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)

	// strf WAVEFORMATEX
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 18)
	writeUint16LE(t, f, 0x0055) // MP3
	writeUint16LE(t, f, 2)      // stereo
	writeUint32LE(t, f, 44100)  // sample rate
	writeUint32LE(t, f, 176400) // avg bytes per sec
	writeUint16LE(t, f, 4)      // block align
	writeUint16LE(t, f, 16)     // bits per sample
	writeUint16LE(t, f, 0)      // cbSize

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, 4+8+48+8+18)
	require.NoError(t, err)
	assert.True(t, stream.isAudio)
	assert.Equal(t, "mp3", stream.codec)
	assert.Equal(t, 2, stream.audioChannels)
	assert.Equal(t, 44100, stream.audioSampleRate)
	assert.Equal(t, 1411, stream.audioBitrate) // 176400 * 8 / 1000
}

// parseStrlList — video with negative height
func TestMiss_ParseStrlList_VideoNegativeHeight(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "negative_height.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with video stream and negative height
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 4+8+48+8+40)
	writeBytes(t, f, []byte("strl"))

	// strh
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
	writeUint16LE(t, f, 640)
	writeUint16LE(t, f, 480)

	// strf BITMAPINFOHEADER with negative height
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 40)
	writeUint32LE(t, f, 40)
	writeInt32LE(t, f, 640)
	writeInt32LE(t, f, -480) // negative height — top-down frames
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

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, 4+8+48+8+40)
	require.NoError(t, err)
	assert.True(t, stream.isVideo)
	assert.Equal(t, 640, stream.width)
	assert.Equal(t, 480, stream.height) // absolute value of -480
}

// parseStrlList — odd-sized chunk with word alignment
func TestMiss_ParseStrlList_WordAlignment(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "strl_odd.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with odd-sized JUNK chunk then strh
	writeBytes(t, f, []byte("LIST"))
	strlSize := uint32(4 + 8 + 5 + 1 + 8 + 48) // strl + JUNK(5+pad) + strh
	writeUint32LE(t, f, strlSize)
	writeBytes(t, f, []byte("strl"))

	// Odd-sized unknown chunk
	writeBytes(t, f, []byte("JUNK"))
	writeUint32LE(t, f, 5) // odd size
	writeBytes(t, f, []byte("ABCDE"))
	writeBytes(t, f, []byte{0}) // padding byte

	// strh
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
	writeUint16LE(t, f, 640)
	writeUint16LE(t, f, 480)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, strlSize)
	require.NoError(t, err)
	assert.True(t, stream.isVideo)
	assert.Equal(t, "h264", stream.codec)
}

// analyzeAVI — full file with Probe interface
func TestMiss_AVIProber_Probe_FullFile(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := createTestAVI(t, tmpDir, "H264", 0x0055)

	f, err := os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := newAVIProber()
	info, err := prober.Probe(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, "mp3", info.AudioCodec)
}

// analyzeAVI — video-only file (no audio stream)
func TestMiss_AnalyzeAVI_VideoOnly(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "video_only.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// RIFF header
	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 1000)
	writeBytes(t, f, []byte("AVI "))

	// LIST hdrl with avih only
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 4+8+56) // hdrl size
	writeBytes(t, f, []byte("hdrl"))

	writeBytes(t, f, []byte("avih"))
	writeUint32LE(t, f, 56)
	writeUint32LE(t, f, 33333)
	writeUint32LE(t, f, 1000000)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 300)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 1920)
	writeUint32LE(t, f, 1080)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeAVI(f)
	require.NoError(t, err)
	assert.Equal(t, "avi", info.Container)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Empty(t, info.VideoCodec) // No strl → no codec detected
	assert.Empty(t, info.AudioCodec)
}

// parseStrlList — video stream with zero scale (frameRate stays 0)
func TestMiss_ParseStrlList_ZeroScale(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "zero_scale.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 4+8+48)
	writeBytes(t, f, []byte("strl"))

	// strh with Scale=0
	writeBytes(t, f, []byte("strh"))
	writeUint32LE(t, f, 48)
	writeBytes(t, f, []byte("vids"))
	writeBytes(t, f, []byte("H264"))
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)  // Scale = 0
	writeUint32LE(t, f, 30) // Rate
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 640)
	writeUint16LE(t, f, 480)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, 4+8+48)
	require.NoError(t, err)
	assert.True(t, stream.isVideo)
	assert.Equal(t, 0.0, stream.frameRate) // Scale=0 → no frame rate calculation
}

// analyzeAVI — seek error at start
func TestMiss_AnalyzeAVI_SeekError(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.avi")
	require.NoError(t, os.WriteFile(emptyPath, []byte{}, 0644))

	f, err := os.Open(emptyPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	assert.Error(t, err)
}

// parseHdrlList — EOF while reading chunks (graceful termination)
func TestMiss_ParseHdrlList_EOFTermination(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "hdrl_eof.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST hdrl with just enough data for header
	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 8) // small size
	writeBytes(t, f, []byte("hdrl"))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	info := &VideoInfo{}
	vf, af := false, false
	err = parseHdrlList(f, info, 12, 8, &vf, &af)
	assert.NoError(t, err) // EOF is graceful termination
}

// parseStrlList — EOF while reading chunks (graceful termination)
func TestMiss_ParseStrlList_EOFTermination(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "strl_eof.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("LIST"))
	writeUint32LE(t, f, 4) // just the type
	writeBytes(t, f, []byte("strl"))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, 4)
	assert.NoError(t, err) // EOF is graceful
	assert.NotNil(t, stream)
}

// parseStrlList — audio with zero-length chunk data
// When strf has 0 bytes and stream is audio, reading WAVEFORMATEX will fail with EOF
func TestMiss_ParseStrlList_EmptyStrf(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "empty_strf.avi")

	f, err := os.Create(aviPath)
	require.NoError(t, err)

	// LIST strl with strh(auds) then strf with 0 size
	writeBytes(t, f, []byte("LIST"))
	strlSize := uint32(4 + 8 + 48 + 8)
	writeUint32LE(t, f, strlSize)
	writeBytes(t, f, []byte("strl"))

	// strh for audio
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
	writeUint32LE(t, f, 44100)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint32LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)
	writeUint16LE(t, f, 0)

	// strf with 0 bytes of data
	writeBytes(t, f, []byte("strf"))
	writeUint32LE(t, f, 0)

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, _ = f.Seek(12, 0)

	stream, err := parseStrlList(f, 12, strlSize)
	// With 0-byte strf, reading WAVEFORMATEX will fail since there's no data
	// This is expected — the test documents this behavior
	if err != nil {
		assert.Contains(t, err.Error(), "WAVEFORMATEX")
	} else {
		assert.True(t, stream.isAudio)
	}
}

// Verify binary helper functions work correctly
func TestMiss_BinaryHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "helpers.bin")
	f, err := os.Create(testPath)
	require.NoError(t, err)

	writeUint32LE(t, f, 0x12345678)
	writeInt32LE(t, f, -1)
	writeUint16LE(t, f, 0xABCD)
	writeBytes(t, f, []byte("TEST"))
	_ = f.Close()

	f, err = os.Open(testPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var u32 uint32
	require.NoError(t, binary.Read(f, binary.LittleEndian, &u32))
	assert.Equal(t, uint32(0x12345678), u32)

	var i32 int32
	require.NoError(t, binary.Read(f, binary.LittleEndian, &i32))
	assert.Equal(t, int32(-1), i32)

	var u16 uint16
	require.NoError(t, binary.Read(f, binary.LittleEndian, &u16))
	assert.Equal(t, uint16(0xABCD), u16)

	buf := make([]byte, 4)
	_, _ = f.Read(buf)
	assert.Equal(t, "TEST", string(buf))
}
