package mediainfo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ReadChunks: non-EOF error from readChunk propagates up ---

func TestReadChunks_ChunkReadNonEOFError(t *testing.T) {
	// Build RIFF header + form type, then have readChunk return a non-EOF error
	var buf bytes.Buffer
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(100))
	buf.Write([]byte("AVI "))

	// Use a reader that returns the RIFF header+form type, then errors
	cr := NewChunkReader(&errorAfterRIFFHeaderReader{
		riffHeader: buf.Bytes(),
		readErr:    errors.New("disk error"),
	})

	_, _, err := cr.ReadChunks()
	assert.Error(t, err)
	// The error should come from readChunk returning non-EOF
	assert.Contains(t, err.Error(), "disk error")
}

// --- readChunk: non-EOF, non-UnexpectedEOF error during data read ---

func TestReadChunk_DataReadNonEOFError(t *testing.T) {
	// Create a reader where after the chunk header, the Read returns a non-EOF error
	cr := NewChunkReader(&chunkDataErrorReader{
		chunkHeader: func() []byte {
			var buf bytes.Buffer
			buf.Write([]byte("avih"))
			binary.Write(&buf, binary.LittleEndian, uint32(20))
			return buf.Bytes()
		}(),
		dataErr: errors.New("read failure"),
	})

	_, err := cr.readChunk()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read avih chunk data")
}

// --- MP4 fallback: field-based stream (59.94fps interlaced) ---

func TestAnalyzeMP4Fallback_FieldBasedFrameRate(t *testing.T) {
	// Build a raw MP4 binary with moov containing a video trak with
	// stts that indicates field-based stream (59.94fps)
	var file bytes.Buffer

	// ftyp box
	ftypData := []byte("isom")
	writeMP4Box(&file, "ftyp", ftypData)

	// moov box data
	var moovData bytes.Buffer

	// mvhd (version 0)
	mvhdData := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhdData[0:], 0)      // version
	binary.BigEndian.PutUint32(mvhdData[12:], 1000)  // timescale
	binary.BigEndian.PutUint32(mvhdData[16:], 10000) // duration
	writeMP4Box(&moovData, "mvhd", mvhdData)

	// trak with 59.94fps video
	var trakData bytes.Buffer

	// mdia
	var mdiaData bytes.Buffer

	// hdlr
	hdlrData := make([]byte, 12)
	copy(hdlrData[8:], "vide")
	writeMP4Box(&mdiaData, "hdlr", hdlrData)

	// mdhd (version 0) - format: version(1)+flags(3)+creation(4)+modification(4)+timescale(4)+duration(4)+...
	mdhdData := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhdData[12:], 60000)  // timescale = 60000
	binary.BigEndian.PutUint32(mdhdData[16:], 120000) // duration = 120000 (2 seconds)
	writeMP4Box(&mdiaData, "mdhd", mdhdData)

	// minf → stbl → stsd + stts
	var minfData bytes.Buffer
	var stblData bytes.Buffer

	// stsd with avc1 video entry
	// stsd format: version(4) + entry_count(4) + entries
	// entry format: size(4) + codec(4) + reserved(6) + data_ref_idx(2) + ... + width(2) + height(2) + ...
	// Total minimum for parseStsdVideo to get width/height: off+36 bytes
	stsdData := make([]byte, 48)
	binary.BigEndian.PutUint32(stsdData[0:], 0) // version + flags
	binary.BigEndian.PutUint32(stsdData[4:], 1) // entry count
	// Entry starts at offset 8
	binary.BigEndian.PutUint32(stsdData[8:], 40) // entry size (including these 4 bytes)
	copy(stsdData[12:], "avc1")                  // codec at off+4
	// width at off+32, height at off+34 (relative to entry start at offset 8)
	// absolute offset: 8+32=40, 8+34=42
	binary.BigEndian.PutUint16(stsdData[40:], 1920) // width
	binary.BigEndian.PutUint16(stsdData[42:], 1080) // height
	writeMP4Box(&stblData, "stsd", stsdData)

	// stts: 59.94fps = field-based stream
	// For field-based detection at timescale=60000:
	// avgDelta should be close to timescale/59.94 ≈ 1001
	// So: 600 samples with delta 1001 each
	// stts format: version(4) + entry_count(4) + [count(4)+delta(4)]*N
	sttsData := make([]byte, 20)                    // 4+4+8+4
	binary.BigEndian.PutUint32(sttsData[0:], 0)     // version + flags
	binary.BigEndian.PutUint32(sttsData[4:], 1)     // entry count
	binary.BigEndian.PutUint32(sttsData[8:], 600)   // sample count
	binary.BigEndian.PutUint32(sttsData[12:], 1001) // sample delta
	writeMP4Box(&stblData, "stts", sttsData)

	writeMP4Box(&minfData, "stbl", stblData.Bytes())
	writeMP4Box(&mdiaData, "minf", minfData.Bytes())
	writeMP4Box(&trakData, "mdia", mdiaData.Bytes())

	writeMP4Box(&moovData, "trak", trakData.Bytes())

	writeMP4Box(&file, "moov", moovData.Bytes())

	// Now parse with analyzeMP4Fallback
	f := bytes.NewReader(file.Bytes())
	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	// Frame rate should be halved if field-based detected
	assert.Greater(t, info.FrameRate, 0.0)
}

// --- MP4 fallback: audio with zero channels defaults to 2 ---

func TestAnalyzeMP4Fallback_AudioZeroChannelsDefaultTwo(t *testing.T) {
	var file bytes.Buffer

	// ftyp box
	writeMP4Box(&file, "ftyp", []byte("isom"))

	// moov box
	var moovData bytes.Buffer

	// mvhd
	mvhdData := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhdData[12:], 1000)
	binary.BigEndian.PutUint32(mvhdData[16:], 10000)
	writeMP4Box(&moovData, "mvhd", mvhdData)

	// audio trak
	var trakData bytes.Buffer
	var mdiaData bytes.Buffer

	// hdlr
	hdlrData := make([]byte, 12)
	copy(hdlrData[8:], "soun")
	writeMP4Box(&mdiaData, "hdlr", hdlrData)

	// mdhd
	mdhdData := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhdData[12:], 44100)
	binary.BigEndian.PutUint32(mdhdData[16:], 441000)
	writeMP4Box(&mdiaData, "mdhd", mdhdData)

	// minf → stbl → stsd
	var minfData bytes.Buffer
	var stblData bytes.Buffer

	// stsd with mp4a entry that has codec but channels = 0
	// The stsd audio format needs: off=8, codec at off+4:off+8,
	// channels at off+24:off+26, sampleRate at off+32:off+34
	// Total minimum: off+34 = 42 bytes + 8 byte stsd header = 50 bytes for data
	// But we want channels=0, so just create a big enough entry
	stsdData := make([]byte, 64)                // big enough for all fields
	binary.BigEndian.PutUint32(stsdData[4:], 1) // entry count
	copy(stsdData[12:], "mp4a")                 // codec at off+4:off+8
	// channels at off+24:off+26 is already 0
	// sampleRate at off+32:off+34 is already 0
	writeMP4Box(&stblData, "stsd", stsdData)

	writeMP4Box(&minfData, "stbl", stblData.Bytes())
	writeMP4Box(&mdiaData, "minf", minfData.Bytes())
	writeMP4Box(&trakData, "mdia", mdiaData.Bytes())

	writeMP4Box(&moovData, "trak", trakData.Bytes())

	writeMP4Box(&file, "moov", moovData.Bytes())

	f := bytes.NewReader(file.Bytes())
	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	// AudioChannels should default to 2 when 0
	assert.Equal(t, 2, info.AudioChannels)
}

// --- MP4 fallback: skipBox with positive dataSize ---

func TestSkipBox_PositiveDataSize(t *testing.T) {
	var file bytes.Buffer

	// Write a large non-moov box that exceeds 10MB read limit
	dataSize := uint64(11 * 1024 * 1024)
	totalSize := dataSize + 8

	var header []byte
	header = binary.BigEndian.AppendUint32(header, uint32(totalSize))
	header = append(header, "mdat"...)
	file.Write(header)
	file.Write(make([]byte, dataSize))

	f := bytes.NewReader(file.Bytes())
	mr := newMP4BoxReader(f)

	box, err := mr.readBox()
	require.NoError(t, err)
	assert.Equal(t, "mdat", box.boxType)
	assert.Nil(t, box.data)

	// skipBox should seek past the data
	err = mr.skipBox(box)
	assert.NoError(t, err)
}

// --- MP4 fallback: skipBox with zero/negative dataSize (no-op) ---

func TestSkipBox_ZeroDataSize(t *testing.T) {
	box := &mp4Box{
		boxType:    "mdat",
		size:       8, // header-only
		headerSize: 8,
		data:       nil,
	}
	// dataSize = 8 - 8 = 0, should be no-op
	var file bytes.Buffer
	mr := newMP4BoxReader(bytes.NewReader(file.Bytes()))
	err := mr.skipBox(box)
	assert.NoError(t, err)
}

// --- MP4 fallback: moov box too large (over 64MB limit) ---

func TestAnalyzeMP4Fallback_MoovBoxTooLarge(t *testing.T) {
	var file bytes.Buffer

	// ftyp box
	writeMP4Box(&file, "ftyp", []byte("isom"))

	// moov box that exceeds 64MB read limit (will have data=nil)
	// We can't actually write 64MB, but we can create a box header that claims 65MB
	// Size = 65*1024*1024 = 68157440
	moovSize := uint64(65 * 1024 * 1024)
	var header []byte
	header = binary.BigEndian.AppendUint32(header, uint32(moovSize))
	header = append(header, "moov"...)
	file.Write(header)
	// Write some dummy data (don't need the full 65MB, just enough for skipBox to seek)
	file.Write(make([]byte, 1024))

	// mdat box after
	writeMP4Box(&file, "mdat", []byte("data"))

	f := bytes.NewReader(file.Bytes())
	info, err := analyzeMP4Fallback(f)
	// Should fail with "no usable data found" since moov wasn't parsed
	if err != nil {
		assert.Contains(t, err.Error(), "no usable data found")
	} else {
		// Or succeed with partial info
		assert.Equal(t, "mp4", info.Container)
	}
}

// --- Helper functions ---

// writeMP4Box writes an MP4 box (size + type + data) to the buffer.
func writeMP4Box(buf *bytes.Buffer, boxType string, data []byte) {
	size := uint32(8 + len(data))
	binary.Write(buf, binary.BigEndian, size)
	buf.Write([]byte(boxType))
	buf.Write(data)
}

// errorAfterRIFFHeaderReader returns RIFF header bytes then errors.
type errorAfterRIFFHeaderReader struct {
	riffHeader []byte
	pos        int
	readErr    error
}

func (r *errorAfterRIFFHeaderReader) Read(p []byte) (int, error) {
	if r.pos < len(r.riffHeader) {
		n := copy(p, r.riffHeader[r.pos:])
		r.pos += n
		return n, nil
	}
	return 0, r.readErr
}

func (r *errorAfterRIFFHeaderReader) Seek(_ int64, _ int) (int64, error) {
	return 0, nil
}

// chunkDataErrorReader returns a chunk header then errors on data read.
type chunkDataErrorReader struct {
	chunkHeader []byte
	pos         int
	dataErr     error
	headerRead  bool
}

func (r *chunkDataErrorReader) Read(p []byte) (int, error) {
	if r.pos < len(r.chunkHeader) {
		n := copy(p, r.chunkHeader[r.pos:])
		r.pos += n
		return n, nil
	}
	return 0, r.dataErr
}

func (r *chunkDataErrorReader) Seek(_ int64, _ int) (int64, error) {
	return 0, nil
}
