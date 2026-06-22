package mediainfo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewChunkReader ---

func TestNewChunkReader(t *testing.T) {
	r := bytes.NewReader(nil)
	cr := NewChunkReader(r)
	assert.NotNil(t, cr)
	assert.Equal(t, r, cr.r)
}

// --- ReadChunks: valid RIFF with form type AVI ---

func TestReadChunks_ValidAVI(t *testing.T) {
	var buf bytes.Buffer
	// RIFF header
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(100))
	buf.Write([]byte("AVI "))

	// One chunk: "avih" with 8 bytes of data
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(8))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	formType, chunks, err := cr.ReadChunks()
	require.NoError(t, err)
	assert.Equal(t, "AVI ", formType)
	require.Len(t, chunks, 1)
	assert.Equal(t, "avih", chunks[0].FourCC)
	assert.Equal(t, uint32(8), chunks[0].Size)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, chunks[0].Data)
}

// --- ReadChunks: invalid RIFF signature ---

func TestReadChunks_InvalidRIFFSignature(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("XXXX"))
	binary.Write(&buf, binary.LittleEndian, uint32(100))
	buf.Write([]byte("AVI "))

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	_, _, err := cr.ReadChunks()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid RIFF signature")
}

// --- ReadChunks: failed to read RIFF header (empty stream) ---

func TestReadChunks_EmptyStream(t *testing.T) {
	reader := bytes.NewReader(nil)
	cr := NewChunkReader(reader)

	_, _, err := cr.ReadChunks()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read RIFF header")
}

// --- ReadChunks: failed to read form type ---

func TestReadChunks_TruncatedAfterRIFF(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(100))
	// No form type bytes

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	_, _, err := cr.ReadChunks()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read form type")
}

// --- ReadChunks: multiple chunks ---

func TestReadChunks_MultipleChunks(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(200))
	buf.Write([]byte("AVI "))

	// Chunk 1: "avih" with 4 bytes
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.Write([]byte{0xAA, 0xBB, 0xCC, 0xDD})

	// Chunk 2: "LIST" with 12 bytes (type + sub-chunk)
	buf.Write([]byte("LIST"))
	binary.Write(&buf, binary.LittleEndian, uint32(12))
	buf.Write([]byte("strl"))                                         // 4 bytes list type
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}) // 8 bytes data

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	formType, chunks, err := cr.ReadChunks()
	require.NoError(t, err)
	assert.Equal(t, "AVI ", formType)
	require.Len(t, chunks, 2)
	assert.Equal(t, "avih", chunks[0].FourCC)
	assert.Equal(t, "LIST", chunks[1].FourCC)
}

// --- ReadChunks: chunk with odd size (word alignment) ---

func TestReadChunks_OddSizeChunkWordAlignment(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(200))
	buf.Write([]byte("AVI "))

	// Odd-sized chunk: 5 bytes of data, needs 1 padding byte
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(5))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05})
	// Padding byte
	buf.WriteByte(0x00)

	// Second chunk after padding
	buf.Write([]byte("strh"))
	binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.Write([]byte{0x11, 0x22, 0x33, 0x44})

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	formType, chunks, err := cr.ReadChunks()
	require.NoError(t, err)
	assert.Equal(t, "AVI ", formType)
	require.Len(t, chunks, 2)
	assert.Equal(t, "avih", chunks[0].FourCC)
	assert.Equal(t, uint32(5), chunks[0].Size)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04, 0x05}, chunks[0].Data)
	assert.Equal(t, "strh", chunks[1].FourCC)
}

// --- ReadChunks: chunk with zero data size ---

func TestReadChunks_ZeroSizeChunk(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(200))
	buf.Write([]byte("AVI "))

	// Zero-size chunk
	buf.Write([]byte("JUNK"))
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	// Another chunk after it
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04})

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	formType, chunks, err := cr.ReadChunks()
	require.NoError(t, err)
	assert.Equal(t, "AVI ", formType)
	require.Len(t, chunks, 2)
	assert.Equal(t, "JUNK", chunks[0].FourCC)
	assert.Equal(t, uint32(0), chunks[0].Size)
	assert.Empty(t, chunks[0].Data)
	assert.Equal(t, "avih", chunks[1].FourCC)
}

// --- readChunk: truncated chunk (EOF before all data) ---

func TestReadChunk_TruncatedEOF(t *testing.T) {
	var buf bytes.Buffer
	// Write chunk header claiming 8 bytes but only provide 4
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(8))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04}) // Only 4 of 8 bytes

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	chunk, err := cr.readChunk()
	require.NoError(t, err)
	assert.Equal(t, "avih", chunk.FourCC)
	assert.Equal(t, uint32(4), chunk.Size) // Actual read bytes
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, chunk.Data)
}

// --- readChunk: truncated chunk (ErrUnexpectedEOF) ---

func TestReadChunk_UnexpectedEOF(t *testing.T) {
	var buf bytes.Buffer
	// Write chunk header claiming 100 bytes but provide much fewer
	buf.Write([]byte("strf"))
	binary.Write(&buf, binary.LittleEndian, uint32(100))
	buf.Write([]byte{0xAA, 0xBB, 0xCC}) // Only 3 of 100 bytes

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	chunk, err := cr.readChunk()
	require.NoError(t, err)
	assert.Equal(t, "strf", chunk.FourCC)
	assert.Equal(t, uint32(3), chunk.Size)
	assert.Equal(t, []byte{0xAA, 0xBB, 0xCC}, chunk.Data)
}

// --- readChunk: EOF on header read (normal stream end) ---

func TestReadChunk_EOFOnHeader(t *testing.T) {
	reader := bytes.NewReader(nil)
	cr := NewChunkReader(reader)

	_, err := cr.readChunk()
	assert.Equal(t, io.EOF, err)
}

// --- readChunk: odd-sized chunk with word alignment padding ---

func TestReadChunk_OddSizeWithPadding(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(3))
	buf.Write([]byte{0x01, 0x02, 0x03})
	// Padding byte
	buf.WriteByte(0x00)

	// Another chunk to verify correct positioning after padding
	buf.Write([]byte("strh"))
	binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.Write([]byte{0x11, 0x22, 0x33, 0x44})

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	chunk1, err := cr.readChunk()
	require.NoError(t, err)
	assert.Equal(t, "avih", chunk1.FourCC)
	assert.Equal(t, uint32(3), chunk1.Size)

	chunk2, err := cr.readChunk()
	require.NoError(t, err)
	assert.Equal(t, "strh", chunk2.FourCC)
	assert.Equal(t, uint32(4), chunk2.Size)
}

// --- readChunk: odd-sized chunk at stream end (padding byte EOF, acceptable) ---

func TestReadChunk_OddSizePaddingEOF(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(3))
	buf.Write([]byte{0x01, 0x02, 0x03})
	// No padding byte — stream ends here

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	chunk, err := cr.readChunk()
	require.NoError(t, err)
	assert.Equal(t, "avih", chunk.FourCC)
	assert.Equal(t, uint32(3), chunk.Size)
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, chunk.Data)
}

// --- readChunk: even-sized chunk (no word alignment needed) ---

func TestReadChunk_EvenSizeNoAlignment(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("avih"))
	binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04})

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	chunk, err := cr.readChunk()
	require.NoError(t, err)
	assert.Equal(t, "avih", chunk.FourCC)
	assert.Equal(t, uint32(4), chunk.Size)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, chunk.Data)
}

// --- readChunk: read error (not EOF or UnexpectedEOF) ---

func TestReadChunk_ReadError(t *testing.T) {
	cr := NewChunkReader(&errorReadSeeker{err: errors.New("read failure")})

	_, err := cr.readChunk()
	assert.Error(t, err)
}

// --- ReadChunks: full RIFF stream with LIST sub-chunk data ---

func TestReadChunks_LISTChunkWithData(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(500))
	buf.Write([]byte("AVI "))

	// LIST chunk with strl type + sub-chunks inside
	subData := []byte("strl")
	subData = append(subData, []byte("strh")...)
	subData = append(subData, []byte{0x00, 0x00, 0x00, 0x00}...) // dummy strh data

	buf.Write([]byte("LIST"))
	binary.Write(&buf, binary.LittleEndian, uint32(len(subData)))
	buf.Write(subData)

	reader := bytes.NewReader(buf.Bytes())
	cr := NewChunkReader(reader)

	formType, chunks, err := cr.ReadChunks()
	require.NoError(t, err)
	assert.Equal(t, "AVI ", formType)
	require.Len(t, chunks, 1)
	assert.Equal(t, "LIST", chunks[0].FourCC)
	// Data includes the "strl" type + sub-chunk data
	assert.Equal(t, subData, chunks[0].Data)
}

// --- Helper: errorReadSeeker always returns error ---

type errorReadSeeker struct {
	err error
}

func (e *errorReadSeeker) Read(_ []byte) (int, error) {
	return 0, e.err
}

func (e *errorReadSeeker) Seek(_ int64, _ int) (int64, error) {
	return 0, e.err
}

// --- RIFFChunk struct access ---

func TestRIFFChunk_Fields(t *testing.T) {
	chunk := RIFFChunk{
		FourCC: "avih",
		Size:   42,
		Data:   []byte{1, 2, 3},
	}
	assert.Equal(t, "avih", chunk.FourCC)
	assert.Equal(t, uint32(42), chunk.Size)
	assert.Equal(t, []byte{1, 2, 3}, chunk.Data)
}
