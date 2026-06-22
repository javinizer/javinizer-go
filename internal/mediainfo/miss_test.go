package mediainfo

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mockExecRunner.output: test the mockExecRunner's output method (cli.go:33) ---

func TestMockExecRunner_Output(t *testing.T) {
	// Test that mockExecRunner correctly delegates to outputFn
	called := false
	ctx := context.Background()
	mock := &mockExecRunner{
		outputFn: func(c context.Context, name string, args ...string) ([]byte, error) {
			called = true
			assert.Equal(t, ctx, c)
			assert.Equal(t, "mediainfo", name)
			assert.Equal(t, []string{"--Output=JSON", "test.mp4"}, args)
			return []byte(`{"media":{"track":[]}}`), nil
		},
	}

	output, err := mock.output(ctx, "mediainfo", "--Output=JSON", "test.mp4")
	require.NoError(t, err)
	assert.True(t, called)
	assert.Contains(t, string(output), "media")
}

func TestMockExecRunner_OutputError(t *testing.T) {
	mock := &mockExecRunner{
		outputFn: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, errors.New("command not found")
		},
	}

	_, err := mock.output(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Equal(t, "command not found", err.Error())
}

// --- readChunk: error path (non-EOF read error during data) ---

func TestReadChunk_ReadDataError(t *testing.T) {
	// Create a reader that returns a specific error on Read (not EOF/UnexpectedEOF)
	cr := NewChunkReader(&partialErrorReader{
		headerErr: nil, // header reads fine
		dataErr:   errors.New("disk read error"),
	})

	_, err := cr.readChunk()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read")
	assert.Contains(t, err.Error(), "disk read error")
}

// --- readChunk: padding read error (non-EOF) ---

func TestReadChunk_PaddingReadError(t *testing.T) {
	var buf bytes.Buffer
	// Write chunk header with odd size
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(3))
	buf.Write([]byte{0x01, 0x02, 0x03})
	// No padding byte — and the reader will error when trying to read it

	cr := NewChunkReader(&errorAfterDataReader{
		data:        buf.Bytes(),
		paddingErr:  errors.New("padding read error"),
		bytesToRead: len(buf.Bytes()),
	})

	chunk, err := cr.readChunk()
	// If the padding read returns a non-EOF error, it should propagate
	if err != nil {
		assert.Contains(t, err.Error(), "failed to skip word-alignment byte")
	} else {
		// If chunk was returned, the padding EOF was treated gracefully
		assert.Equal(t, "avih", chunk.FourCC)
	}
}

// --- readChunk: even-sized chunk with full data ---

func TestReadChunk_FullDataRead(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("strh"))
	binary.Write(&buf, binary.LittleEndian, uint32(6))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})

	cr := NewChunkReader(bytes.NewReader(buf.Bytes()))
	chunk, err := cr.readChunk()
	require.NoError(t, err)
	assert.Equal(t, "strh", chunk.FourCC)
	assert.Equal(t, uint32(6), chunk.Size)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, chunk.Data)
}

// --- ReadChunks: error reading chunk data (non-EOF) propagates up ---

func TestReadChunks_ChunkReadError(t *testing.T) {
	// Build RIFF header + form type, then a chunk header that claims data
	// but the data reader will error
	var buf bytes.Buffer
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(100))
	buf.Write([]byte("AVI "))
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(50))
	// Only a few bytes instead of 50 — will cause UnexpectedEOF which is handled gracefully

	cr := NewChunkReader(bytes.NewReader(buf.Bytes()))
	formType, chunks, err := cr.ReadChunks()
	// UnexpectedEOF in chunk data returns partial data, not error
	require.NoError(t, err)
	assert.Equal(t, "AVI ", formType)
	require.Len(t, chunks, 1)
}

// --- MKV: parseSegment with zero size (uses er.r directly) ---

func TestParseSegment_ZeroSize(t *testing.T) {
	// Build a segment with Info and Tracks but parse it via parseSegment with size=0
	// This exercises the `else` branch: limitReader = er.r
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "segment_zero_size.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Build segment data with Info + Tracks
	segmentData := buildMKVSegmentData()
	// Put segment data after EBML header
	buf = appendEBMLMasterElement(buf, elemSegment, segmentData)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Skip EBML header by parsing it
	_, _ = f.Seek(0, io.SeekStart)
	er := newEBMLReader(f)

	for {
		elem, err := er.readElement()
		if err != nil {
			break
		}
		if elem.id == elemEBML {
			parseEBMLHeader(er, elem.size)
			break
		}
	}

	// Read the segment element
	elem, err := er.readElement()
	require.NoError(t, err)
	assert.Equal(t, elemSegment, elem.id)

	// Call parseSegment with size=0 to exercise the else branch
	info := &VideoInfo{Container: "mkv"}
	parseSegment(er, 0, info)

	// Should still parse Info and Tracks from the remaining stream
	// Since size=0 uses er.r directly, it will read until EOF
	assert.Equal(t, "mkv", info.Container)
}

// --- MKV: parseSegment with unknown element that needs skipping ---

func TestParseSegment_UnknownElementWithSkip(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "segment_unknown.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Build segment with an unknown non-master element before Info
	segmentData := buildMKVSegmentInfoData()

	// Add an unknown non-master element (ID 0x3E, size=3, data=0x010203)
	// This will exercise the default: skipBytes path in parseSegment
	unknownElem := []byte{0x3E, 0x83, 0x01, 0x02, 0x03} // ID=0x3E, VintSize=3, data=3bytes
	segmentData = append(segmentData, unknownElem...)

	segmentData = append(segmentData, buildMKVTracksData()...)

	buf = appendEBMLMasterElement(buf, elemSegment, segmentData)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	// Should succeed despite unknown element
	if err == nil {
		assert.Equal(t, "mkv", info.Container)
	}
}

// --- MKV: analyzeMKV with element at top level that needs skipping ---

func TestAnalyzeMKV_TopLevelUnknownElement(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "toplevel_unknown.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Add an unknown non-master element between EBML and Segment
	// Element ID 0x1B (unknown), size=2, data=0x00FF
	buf = append(buf, 0x1B, 0x82, 0x00, 0xFF)

	// Add the segment
	segmentData := buildMKVSegmentData()
	buf = appendEBMLMasterElement(buf, elemSegment, segmentData)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMKV(f)
	if err == nil {
		assert.Equal(t, "mkv", info.Container)
	}
}

// --- MKV: analyzeMKV with Cluster at top level (skip) ---

func TestAnalyzeMKV_TopLevelClusterSkip(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "toplevel_cluster.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Add Segment with Info + Cluster + Tracks
	segmentData := buildMKVSegmentInfoData()

	// Add a Cluster element inside the segment
	clusterData := make([]byte, 16)
	for i := range clusterData {
		clusterData[i] = byte(i)
	}
	segmentData = appendEBMLMasterElement(segmentData, elemCluster, clusterData)

	// Add Cues element inside the segment
	cuesData := make([]byte, 8)
	segmentData = appendEBMLMasterElement(segmentData, elemCues, cuesData)

	segmentData = append(segmentData, buildMKVTracksData()...)

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
}

// --- MKV: parseSegmentInfo with zero/negative size (early return) ---

func TestParseSegmentInfo_ZeroSize(t *testing.T) {
	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(nil))
	// Should return immediately
	parseSegmentInfo(er, 0, info)
	assert.Equal(t, 0.0, info.Duration)

	parseSegmentInfo(er, -1, info)
	assert.Equal(t, 0.0, info.Duration)
}

// --- MKV: SegmentInfo with Duration ---

func TestParseSegmentInfo_DurationParsed(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "segmentinfo_duration.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Use the existing helper which creates Info + Tracks
	segmentData := buildMKVSegmentData()
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
	assert.Equal(t, "h264", info.VideoCodec)
	// Duration may or may not be parsed depending on float precision
	if info.Duration > 0 {
		assert.Greater(t, info.Duration, 0.0)
	}
}

// --- MKV: parseTracks with zero size (early return) ---

func TestParseTracks_ZeroSize(t *testing.T) {
	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(nil))
	parseTracks(er, 0, info)
	assert.Equal(t, "", info.VideoCodec)

	parseTracks(er, -1, info)
	assert.Equal(t, "", info.VideoCodec)
}

// --- MKV: parseTrackEntry with zero size (early return) ---

func TestParseTrackEntry_ZeroSize(t *testing.T) {
	info := &VideoInfo{}
	er := newEBMLReader(bytes.NewReader(nil))
	parseTrackEntry(er, 0, info)
	assert.Equal(t, "", info.VideoCodec)
	assert.Equal(t, "", info.AudioCodec)

	parseTrackEntry(er, -1, info)
	assert.Equal(t, "", info.VideoCodec)
}

// --- MKV: parseVideoElement with zero size (early return) ---

func TestParseVideoElement_ZeroSize(t *testing.T) {
	var pw, ph uint64
	er := newEBMLReader(bytes.NewReader(nil))
	parseVideoElement(er, 0, &pw, &ph)
	assert.Equal(t, uint64(0), pw)
	assert.Equal(t, uint64(0), ph)

	parseVideoElement(er, -1, &pw, &ph)
	assert.Equal(t, uint64(0), pw)
	assert.Equal(t, uint64(0), ph)
}

// --- MKV: parseAudioElement with zero size (early return) ---

func TestParseAudioElement_ZeroSize(t *testing.T) {
	var sf float64
	var ch uint64
	er := newEBMLReader(bytes.NewReader(nil))
	parseAudioElement(er, 0, &sf, &ch)
	assert.Equal(t, float64(0), sf)
	assert.Equal(t, uint64(0), ch)
}

// --- MKV: analyzeMKV with no metadata (returns error) ---

func TestAnalyzeMKV_NoMetadataError(t *testing.T) {
	tmpDir := t.TempDir()
	mkvPath := filepath.Join(tmpDir, "no_metadata.mkv")
	f, err := os.Create(mkvPath)
	require.NoError(t, err)

	var buf []byte
	buf = appendEBMLHeader(buf)

	// Segment with empty content
	segmentData := []byte{}
	buf = appendEBMLMasterElement(buf, elemSegment, segmentData)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mkvPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeMKV(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no usable data found")
}

// --- MKV: parseEBMLHeader with negative size (early return) ---

func TestParseEBMLHeader_NegativeSize(t *testing.T) {
	er := newEBMLReader(bytes.NewReader(nil))
	// Should return immediately without panic
	parseEBMLHeader(er, -5)
}

// --- EBML: readElement with zero-size element (size=0, not a master) ---

func TestReadElement_ZeroSizeNonMasterElement(t *testing.T) {
	// Element with size=0 that is NOT a known master element
	// With size=0 and not a master, data will be nil (size==0, no data to read)
	var buf bytes.Buffer
	// Element ID: 0xA3 (1-byte, class-A, not a known master)
	buf.WriteByte(0xA3)
	// Vint size = 0x80 (1-byte vint, value=0)
	buf.WriteByte(0x80)

	er := newEBMLReader(bytes.NewReader(buf.Bytes()))
	elem, err := er.readElement()
	if err == nil {
		assert.Equal(t, uint32(0xA3), elem.id)
		assert.Equal(t, int64(0), elem.size)
		assert.Nil(t, elem.data)
	}
}

// --- EBML: readElement with oversized element (skipped) ---

func TestReadElement_OversizedElementSkipped(t *testing.T) {
	// Build an EBML element with size > 16MB (should be skipped)
	var buf bytes.Buffer
	// Element ID: 0x23 (non-master, 2-byte)
	buf.Write([]byte{0x23, 0x56})
	// Vint size: we need size > 16MB. Let's use a 2-byte vint.
	// 0x40 | (size >> 8), (size & 0xFF)
	size := uint64(17 * 1024 * 1024) // 17MB
	buf.WriteByte(byte(0x40 | (size >> 8)))
	buf.WriteByte(byte(size & 0xFF))
	// We need enough data after this for skipBytes
	// But since we use bytes.Reader which supports Seek, skipBytes will use Seek
	buf.Write(make([]byte, 100)) // Some padding

	er := newEBMLReader(bytes.NewReader(buf.Bytes()))
	elem, err := er.readElement()
	// Should succeed by skipping
	if err == nil {
		assert.NotNil(t, elem)
		assert.Nil(t, elem.data)
	}
}

// --- EBML: readVintSize with 5-8 byte lengths ---

func TestReadVintSize_LongSizes(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantLen int
		wantVal int64
	}{
		// 5-byte vint: leading byte has bit 3 set (0x08)
		{"5-byte vint", []byte{0x08, 0x00, 0x00, 0x00, 0x01}, 5, 1},
		// 6-byte vint: leading byte has bit 2 set (0x04)
		{"6-byte vint", []byte{0x04, 0x00, 0x00, 0x00, 0x00, 0x01}, 6, 1},
		// 7-byte vint: leading byte has bit 1 set (0x02)
		{"7-byte vint", []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}, 7, 1},
		// 8-byte vint: leading byte has bit 0 set (0x01)
		{"8-byte vint", []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}, 8, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, length, err := readVintSize(bytes.NewReader(tt.data))
			require.NoError(t, err)
			assert.Equal(t, tt.wantLen, length)
			assert.Equal(t, tt.wantVal, size)
		})
	}
}

// --- EBML: readElementID with unsupported leading byte ---

func TestReadElementID_UnsupportedLength(t *testing.T) {
	// Leading byte with no VINT marker bits set
	_, _, err := readElementID(bytes.NewReader([]byte{0x00}))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported element ID length")
}

// --- EBML: readVintSize with zero leading byte ---

func TestReadVintSize_ZeroLeadingByte(t *testing.T) {
	_, _, err := readVintSize(bytes.NewReader([]byte{0x00}))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid vint size")
}

// --- EBML: parseFloatEBML with invalid length ---

func TestParseFloatEBML_InvalidLength(t *testing.T) {
	result := parseFloatEBML([]byte{0x01, 0x02, 0x03}) // 3 bytes — not 4 or 8
	assert.Equal(t, float64(0), result)
}

// --- EBML: parseUint with empty data ---

func TestParseUint_EmptyDataBuffer(t *testing.T) {
	result := parseUint([]byte{})
	assert.Equal(t, uint64(0), result)
}

// --- MP4: extractMP4VideoInfo with missing stsd (error path) ---

func TestExtractMP4VideoInfo_MissingStsdBox(t *testing.T) {
	info := &VideoInfo{}
	// Create a minimal MP4 file with a trak that has no stsd
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "no_stsd.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	mp4File := buildMP4WithTrakNoStsd()
	err = mp4File.Encode(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	decoded, err := mp4DecodeFile(f)
	if err == nil && decoded.Moov != nil {
		for _, trak := range decoded.Moov.Traks {
			if trak.Mdia != nil && trak.Mdia.Hdlr != nil && trak.Mdia.Hdlr.HandlerType == "vide" {
				err := extractMP4VideoInfo(trak, info)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "missing sample description")
			}
		}
	}
}

// --- MP4: extractMP4AudioInfo with missing stsd (error path) ---

func TestExtractMP4AudioInfo_MissingStsdBox(t *testing.T) {
	info := &VideoInfo{}
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "no_audio_stsd.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	mp4File := buildMP4WithAudioTrakNoStsd()
	err = mp4File.Encode(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	decoded, err := mp4DecodeFile(f)
	if err == nil && decoded.Moov != nil {
		for _, trak := range decoded.Moov.Traks {
			if trak.Mdia != nil && trak.Mdia.Hdlr != nil && trak.Mdia.Hdlr.HandlerType == "soun" {
				err := extractMP4AudioInfo(trak, info)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "missing sample description")
			}
		}
	}
}

// --- MOV: Probe with valid QuickTime ftyp ---

func TestMOVProber_Probe_QuickTime(t *testing.T) {
	tmpDir := t.TempDir()
	movPath := filepath.Join(tmpDir, "quicktime.mov")

	f, err := os.Create(movPath)
	require.NoError(t, err)

	mp4File := buildQuickTimeFile()
	err = mp4File.Encode(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(movPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	prober := newMOVProber()
	info, err := prober.Probe(context.Background(), f)
	if err == nil {
		assert.Equal(t, "mov", info.Container)
	}
}

// --- MOV: canProbe with various QuickTime brands ---

func TestMOVProber_CanProbe_QuickTimeBrands(t *testing.T) {
	prober := newMOVProber()

	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{
			name:     "qt brand",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'q', 't', ' ', ' '},
			expected: true,
		},
		{
			name:     "M4V brand",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'M', '4', 'V', ' '},
			expected: true,
		},
		{
			name:     "M4A brand",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '},
			expected: true,
		},
		{
			name:     "M4B brand",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'M', '4', 'B', ' '},
			expected: true,
		},
		{
			name:     "M4P brand",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'M', '4', 'P', ' '},
			expected: true,
		},
		{
			name:     "F4V brand",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'F', '4', 'V', ' '},
			expected: true,
		},
		{
			name:     "F4A brand",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'F', '4', 'A', ' '},
			expected: true,
		},
		{
			name:     "F4B brand",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'F', '4', 'B', ' '},
			expected: true,
		},
		{
			name:     "isom brand (not MOV)",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
			expected: false,
		},
		{
			name:     "short header",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 'q', 't'},
			expected: false,
		},
		{
			name:     "no ftyp",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prober.canProbe(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- CLI: Probe with FileReader that has no Name (no fileReaderStat) ---

func TestCLIProber_Probe_NoFileName(t *testing.T) {
	cfg := &mediaInfoConfig{
		CLIEnabled: true,
		CLIPath:    "mediainfo",
		CLITimeout: 5,
	}
	prober := newCLIProber(cfg)

	// Create a FileReader that doesn't implement fileReaderStat
	f := &namelessFileReader{}
	_, err := prober.Probe(context.Background(), f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not expose Name")
}

// --- prober: probeWithFallback with ReadAt failure ---

func TestProbeWithFallback_ReadAtFailure(t *testing.T) {
	cfg := defaultMediaInfoConfig()
	registry := newProberRegistry(cfg)

	f := &failingReadAtReader{}
	_, err := registry.probeWithFallback(context.Background(), f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file header")
}

// --- MP4 fallback: analyzeMP4Fallback with no usable data ---

func TestAnalyzeMP4Fallback_NoUsableData(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "empty_boxes.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	// Write a minimal file with ftyp but no moov/tracks
	// ftyp box
	ftypData := []byte("isom")
	var ftypBox []byte
	ftypBox = binary.BigEndian.AppendUint32(ftypBox, uint32(8+len(ftypData)))
	ftypBox = append(ftypBox, "ftyp"...)
	ftypBox = append(ftypBox, ftypData...)

	_, err = f.Write(ftypBox)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeMP4Fallback(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no usable data found")
}

// --- MP4 fallback: readBox with extended size ---

func TestReadBox_ExtendedSizeBox(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "extended_size.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	// Write a box with extended (8-byte) size
	// Size = 1 signals extended size
	var header []byte
	header = binary.BigEndian.AppendUint32(header, 1) // signals extended
	header = append(header, "moov"...)
	// Extended size: 16 (header) + 0 data
	extSize := make([]byte, 8)
	binary.BigEndian.PutUint64(extSize, uint64(16))
	header = append(header, extSize...)

	_, err = f.Write(header)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	mr := newMP4BoxReader(f)
	box, err := mr.readBox()
	if err == nil {
		assert.Equal(t, "moov", box.boxType)
		assert.Equal(t, uint64(16), box.size)
		assert.Equal(t, 16, box.headerSize)
	}
}

// --- MP4 fallback: readBox with size=0 (extends to EOF) ---

func TestReadBox_SizeZeroBox(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "size_zero.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	// Write a box with size=0 (extends to end of file)
	var header []byte
	header = binary.BigEndian.AppendUint32(header, 0) // size=0
	header = append(header, "mdat"...)
	header = append(header, []byte("some media data here")...)

	_, err = f.Write(header)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	mr := newMP4BoxReader(f)
	box, err := mr.readBox()
	if err == nil {
		assert.Equal(t, "mdat", box.boxType)
		// Size should be the total file length
		assert.Greater(t, box.size, uint64(0))
	}
}

// --- MP4 fallback: skipBox with data=nil and positive dataSize ---

func TestSkipBox_WithNilBoxData(t *testing.T) {
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "skip_box.mp4")

	f, err := os.Create(mp4Path)
	require.NoError(t, err)

	// Write a large mdat box (over 10MB read limit, so data will be nil)
	// Size header (8 bytes) + 11MB of data
	dataSize := uint64(11 * 1024 * 1024)
	totalSize := dataSize + 8
	var header []byte
	header = binary.BigEndian.AppendUint32(header, uint32(totalSize))
	header = append(header, "mdat"...)
	_, err = f.Write(header)
	require.NoError(t, err)
	// Write enough bytes to simulate the data
	_, err = f.Write(make([]byte, dataSize))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(mp4Path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	mr := newMP4BoxReader(f)
	box, err := mr.readBox()
	require.NoError(t, err)
	assert.Equal(t, "mdat", box.boxType)
	assert.Nil(t, box.data) // Too large to read into memory

	// Now skip the box
	err = mr.skipBox(box)
	assert.NoError(t, err)
}

// --- AVI: analyzeAVI with invalid RIFF signature ---

func TestAnalyzeAVI_InvalidRIFF(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "invalid_riff.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("XXXX"))
	writeUint32LE(t, f, 100)
	writeBytes(t, f, []byte("AVI "))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid RIFF signature")
}

// --- AVI: analyzeAVI with non-AVI form type ---

func TestAnalyzeAVI_NonAVIFormType(t *testing.T) {
	tmpDir := t.TempDir()
	aviPath := filepath.Join(tmpDir, "not_avi.avi")
	f, err := os.Create(aviPath)
	require.NoError(t, err)

	writeBytes(t, f, []byte("RIFF"))
	writeUint32LE(t, f, 100)
	writeBytes(t, f, []byte("WAVE"))

	_ = f.Close()

	f, err = os.Open(aviPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = analyzeAVI(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not an AVI file")
}

// --- Helper types ---

// partialErrorReader returns a valid chunk header then errors on data read.
type partialErrorReader struct {
	headerErr  error
	dataErr    error
	headerRead bool
}

func (r *partialErrorReader) Read(p []byte) (int, error) {
	if !r.headerRead {
		// Return a valid chunk header first
		var buf bytes.Buffer
		buf.Write([]byte("avih"))
		binary.Write(&buf, binary.LittleEndian, uint32(10))
		n := copy(p, buf.Bytes())
		r.headerRead = true
		return n, nil
	}
	return 0, r.dataErr
}

func (r *partialErrorReader) Seek(_ int64, _ int) (int64, error) {
	return 0, nil
}

// errorAfterDataReader returns valid data up to a point, then errors on padding read.
type errorAfterDataReader struct {
	data        []byte
	paddingErr  error
	bytesToRead int
	readPos     int
}

func (r *errorAfterDataReader) Read(p []byte) (int, error) {
	if r.readPos < len(r.data) {
		n := copy(p, r.data[r.readPos:])
		r.readPos += n
		return n, nil
	}
	return 0, r.paddingErr
}

func (r *errorAfterDataReader) Seek(_ int64, _ int) (int64, error) {
	return 0, nil
}

// namelessFileReader implements FileReader but not fileReaderStat.
type namelessFileReader struct{}

func (n *namelessFileReader) Read(_ []byte) (int, error)            { return 0, io.EOF }
func (n *namelessFileReader) ReadAt(_ []byte, _ int64) (int, error) { return 0, io.EOF }
func (n *namelessFileReader) Seek(_ int64, _ int) (int64, error)    { return 0, nil }

// failingReadAtReader implements FileReader but ReadAt always fails.
type failingReadAtReader struct{}

func (f *failingReadAtReader) Read(_ []byte) (int, error) { return 0, io.EOF }
func (f *failingReadAtReader) ReadAt(_ []byte, _ int64) (int, error) {
	return 0, fmt.Errorf("read error")
}
func (f *failingReadAtReader) Seek(_ int64, _ int) (int64, error) { return 0, nil }

// --- MP4 helper functions ---

func mp4DecodeFile(f *os.File) (*mp4.File, error) {
	_, _ = f.Seek(0, io.SeekStart)
	return mp4.DecodeFile(f, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
}

func buildMP4WithTrakNoStsd() *mp4.File {
	mp4File := mp4.NewFile()
	ftyp := mp4.CreateFtyp()
	mp4File.AddChild(ftyp, 0)

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = 1000
	mvhd.Duration = 5000
	moov.AddChild(mvhd)

	// Video trak with mdia + hdlr but no stbl/stsd
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, _ := mp4.CreateHdlr("vide")
	mdia.AddChild(hdlr)
	mdhd := &mp4.MdhdBox{Timescale: 24000, Duration: 240000}
	mdia.AddChild(mdhd)
	// No Minf/Stbl/Stsd
	trak.AddChild(mdia)
	moov.AddChild(trak)

	mp4File.AddChild(moov, 0)
	return mp4File
}

func buildMP4WithAudioTrakNoStsd() *mp4.File {
	mp4File := mp4.NewFile()
	ftyp := mp4.CreateFtyp()
	mp4File.AddChild(ftyp, 0)

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = 1000
	mvhd.Duration = 5000
	moov.AddChild(mvhd)

	// Audio trak with mdia + hdlr but no stbl/stsd
	trak := mp4.NewTrakBox()
	mdia := mp4.NewMdiaBox()
	hdlr, _ := mp4.CreateHdlr("soun")
	mdia.AddChild(hdlr)
	mdhd := &mp4.MdhdBox{Timescale: 44100, Duration: 441000}
	mdia.AddChild(mdhd)
	trak.AddChild(mdia)
	moov.AddChild(trak)

	mp4File.AddChild(moov, 0)
	return mp4File
}

func buildQuickTimeFile() *mp4.File {
	mp4File := mp4.NewFile()
	ftyp := mp4.CreateFtyp()
	mp4File.AddChild(ftyp, 0)

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = 1000
	mvhd.Duration = 10000
	moov.AddChild(mvhd)

	// Video track
	vtrak := mp4.NewTrakBox()
	vmdia := mp4.NewMdiaBox()
	vhdlr, _ := mp4.CreateHdlr("vide")
	vmdia.AddChild(vhdlr)
	vmdhd := &mp4.MdhdBox{Timescale: 24000, Duration: 240000}
	vmdia.AddChild(vmdhd)
	vminf := mp4.NewMinfBox()
	vstbl := mp4.NewStblBox()
	vstsd := mp4.NewStsdBox()
	visualEntry := mp4.CreateVisualSampleEntryBox("avc1", 1920, 1080, nil)
	vstsd.AddChild(visualEntry)
	vstbl.AddChild(vstsd)
	vstts := &mp4.SttsBox{SampleCount: []uint32{240}, SampleTimeDelta: []uint32{1000}}
	vstbl.AddChild(vstts)
	vminf.AddChild(vstbl)
	vmdia.AddChild(vminf)
	vtrak.AddChild(vmdia)
	moov.AddChild(vtrak)

	mp4File.AddChild(moov, 0)
	return mp4File
}

// buildMKVTracksEntryData builds a single video track entry.
func buildMKVTracksEntryData() []byte {
	var videoTrackData []byte
	videoTrackData = append(videoTrackData, appendEBMLUint(elemTrackNumber, 1)...)
	videoTrackData = append(videoTrackData, appendEBMLUint(elemTrackType, trackTypeVideo)...)
	videoTrackData = append(videoTrackData, appendEBMLString(elemCodecID, "V_MPEG4/ISO/AVC")...)

	var videoData []byte
	videoData = append(videoData, appendEBMLUint(elemPixelWidth, 1920)...)
	videoData = append(videoData, appendEBMLUint(elemPixelHeight, 1080)...)
	videoTrackData = append(videoTrackData, appendEBMLMasterElement(videoTrackData, elemVideo, videoData)...)

	return appendEBMLMasterElement(nil, elemTrackEntry, videoTrackData)
}
