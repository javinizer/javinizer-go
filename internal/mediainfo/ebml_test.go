package mediainfo

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadElementID(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantID  uint32
		wantLen int
		wantErr bool
	}{
		{"1-byte ID 0xAE", []byte{0xAE}, 0xAE, 1, false},
		{"1-byte ID 0x83", []byte{0x83}, 0x83, 1, false},
		{"2-byte ID Duration", []byte{0x44, 0x89}, 0x4489, 2, false},
		{"2-byte ID TrackType", []byte{0x83, 0x00}, 0x83, 1, false},
		{"3-byte ID TimecodeScale", []byte{0x2A, 0xD7, 0xB1}, 0x2AD7B1, 3, false},
		{"4-byte ID EBML header", []byte{0x1A, 0x45, 0xDF, 0xA3}, 0x1A45DFA3, 4, false},
		{"4-byte ID Segment", []byte{0x18, 0x53, 0x80, 0x67}, 0x18538067, 4, false},
		{"empty reader", []byte{}, 0, 0, true},
		{"truncated 2-byte", []byte{0x1A}, 0, 0, true},
		{"truncated 4-byte", []byte{0x1A, 0x45, 0xDF}, 0, 0, true},
		{"invalid leading byte 0x00", []byte{0x00}, 0, 0, true},
		{"invalid leading byte 0x01", []byte{0x01}, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, n, err := readElementID(bytes.NewReader(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantLen, n)
		})
	}
}

func TestReadVintSize(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantVal int64
		wantLen int
		wantErr bool
	}{
		{"1-byte size 1", []byte{0x81}, 1, 1, false},
		{"1-byte size 127", []byte{0xFF}, 127, 1, false},
		{"2-byte size 128", []byte{0x40, 0x80}, 128, 2, false},
		{"2-byte size 0x3FFF", []byte{0x7F, 0xFF}, 0x3FFF, 2, false},
		{"3-byte size", []byte{0x20, 0x01, 0x00}, 256, 3, false},
		{"4-byte size", []byte{0x10, 0x00, 0x01, 0x00}, 256, 4, false},
		{"5-byte size", []byte{0x08, 0x00, 0x00, 0x01, 0x00}, 256, 5, false},
		{"6-byte size", []byte{0x04, 0x00, 0x00, 0x00, 0x01, 0x00}, 256, 6, false},
		{"7-byte size", []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00}, 256, 7, false},
		{"8-byte size", []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00}, 256, 8, false},
		{"unknown size 1-byte", []byte{0xFF}, 127, 1, false},
		{"empty reader", []byte{}, 0, 0, true},
		{"truncated 2-byte", []byte{0x40}, 0, 0, true},
		{"invalid leading byte 0x00", []byte{0x00}, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, n, err := readVintSize(bytes.NewReader(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVal, val)
			assert.Equal(t, tt.wantLen, n)
		})
	}
}

func TestReadVintSize_UnknownSize(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(0x83)                                               // non-master element
	buf.Write([]byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}) // 8-byte VInt

	er := newEBMLReader(&buf)
	_, err := er.readElement()
	assert.Error(t, err)
}

func TestReadVintSize_MaskBug(t *testing.T) {
	input := []byte{0x43, 0x00}
	val, n, err := readVintSize(bytes.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, int64(0x0300), val)

	input2 := []byte{0x21, 0x00, 0x00}
	val2, n2, err2 := readVintSize(bytes.NewReader(input2))
	require.NoError(t, err2)
	assert.Equal(t, 3, n2)
	assert.Equal(t, int64(0x010000), val2)
}

func TestParseUint(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint64
	}{
		{"empty", []byte{}, 0},
		{"single byte", []byte{0x42}, 0x42},
		{"two bytes", []byte{0x01, 0x00}, 256},
		{"four bytes", []byte{0x00, 0x01, 0x00, 0x00}, 65536},
		{"eight bytes", []byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00}, 4294967296},
		{"max uint64", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, uint64(0xFFFFFFFFFFFFFFFF)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseUint(tt.data))
		})
	}
}

func TestParseFloatEBML(t *testing.T) {
	t.Run("empty data", func(t *testing.T) {
		assert.Equal(t, 0.0, parseFloatEBML([]byte{}))
	})

	t.Run("invalid length", func(t *testing.T) {
		assert.Equal(t, 0.0, parseFloatEBML([]byte{0x01, 0x02, 0x03}))
	})

	t.Run("float32", func(t *testing.T) {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], math.Float32bits(3.14))
		result := parseFloatEBML(buf[:])
		assert.InDelta(t, 3.14, result, 0.001)
	})

	t.Run("float64", func(t *testing.T) {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(2.718281828))
		result := parseFloatEBML(buf[:])
		assert.InDelta(t, 2.718281828, result, 0.0001)
	})

	t.Run("float32 zero", func(t *testing.T) {
		assert.Equal(t, 0.0, parseFloatEBML([]byte{0, 0, 0, 0}))
	})

	t.Run("float64 zero", func(t *testing.T) {
		assert.Equal(t, 0.0, parseFloatEBML([]byte{0, 0, 0, 0, 0, 0, 0, 0}))
	})
}

func TestParseString(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", []byte{}, ""},
		{"V_MPEG4/ISO/AVC", []byte("V_MPEG4/ISO/AVC"), "V_MPEG4/ISO/AVC"},
		{"A_AAC", []byte("A_AAC"), "A_AAC"},
		{"with null padding", []byte("test\x00\x00"), "test\x00\x00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseString(tt.data))
		})
	}
}

func TestSkipBytes_Seeker(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	r := bytes.NewReader(data)

	er := newEBMLReader(r)
	require.NoError(t, er.skipBytes(2))

	buf := make([]byte, 1)
	_, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(0x03), buf[0])
}

func TestSkipBytes_NonSeeker(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	r := &readerWithoutSeeker{data: data}

	er := newEBMLReader(r)
	require.NoError(t, er.skipBytes(3))

	buf := make([]byte, 1)
	_, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(0x04), buf[0])
}

func TestSkipBytes_Zero(t *testing.T) {
	data := []byte{0x01}
	r := bytes.NewReader(data)
	er := newEBMLReader(r)
	require.NoError(t, er.skipBytes(0))
	require.NoError(t, er.skipBytes(-1))
}

type readerWithoutSeeker struct {
	data   []byte
	offset int
}

func (r *readerWithoutSeeker) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

func TestReadElement_OversizedNonMaster(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(0x83) // TrackType element ID (1-byte)
	buf.WriteByte(0x81) // VInt size = 1
	buf.WriteByte(0x01) // value

	er := newEBMLReader(&buf)
	elem, err := er.readElement()
	require.NoError(t, err)
	assert.Equal(t, uint32(0x83), elem.id)
	assert.Equal(t, int64(1), elem.size)
	require.NotNil(t, elem.data)
	assert.Equal(t, []byte{0x01}, elem.data)
}

func TestReadElement_MasterElement(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x1A, 0x45, 0xDF, 0xA3}) // EBML header ID (4-byte)
	buf.WriteByte(0x81)                       // VInt size = 1
	buf.WriteByte(0x00)                       // dummy data

	er := newEBMLReader(&buf)
	elem, err := er.readElement()
	require.NoError(t, err)
	assert.Equal(t, uint32(0x1A45DFA3), elem.id)
	assert.Equal(t, int64(1), elem.size)
	assert.Nil(t, elem.data)
}

func TestReadElement_SizeCap(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(0x83) // TrackType (non-master)
	buf.WriteByte(0x40)
	buf.WriteByte(0x01) // size = 1 (2-byte VInt: 0x4001 = 1)
	buf.WriteByte(0x01) // value

	er := newEBMLReader(&buf)
	elem, err := er.readElement()
	require.NoError(t, err)
	assert.Equal(t, uint32(0x83), elem.id)
	require.NotNil(t, elem.data)
	assert.Equal(t, []byte{0x01}, elem.data)
}

func TestIsKnownMasterElement(t *testing.T) {
	tests := []struct {
		id   uint32
		want bool
	}{
		{0x1A45DFA3, true}, // EBML
		{0x18538067, true}, // Segment
		{0x1549A966, true}, // Info
		{0x1654AE6B, true}, // Tracks
		{0xAE, true},       // TrackEntry
		{0xE0, true},       // Video
		{0xE1, true},       // Audio
		{0x1C53BB6B, true}, // Cues
		{0x1F43B675, true}, // Cluster
		{0x83, false},      // TrackType (non-master)
		{0x86, false},      // CodecID (non-master)
		{0x4489, false},    // Duration (non-master)
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.want, isKnownMasterElement(tt.id))
		})
	}
}

func TestReadElement_NegativeSize(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x44, 0x89}) // Duration element ID (2-byte)
	buf.Write([]byte{0x40, 0x80}) // VInt size = 128

	data := make([]byte, 128)
	buf.Write(data)

	er := newEBMLReader(&buf)
	elem, err := er.readElement()
	require.NoError(t, err)
	assert.Equal(t, uint32(0x4489), elem.id)
	assert.Equal(t, int64(128), elem.size)
}

func TestReadElement_NegativeSizeNonMaster(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(0x83) // TrackType (non-master)
	buf.WriteByte(0x81) // 1-byte VInt, size = 1
	buf.WriteByte(0x01) // value

	er := newEBMLReader(&buf)
	elem, err := er.readElement()
	require.NoError(t, err)
	assert.Equal(t, uint32(0x83), elem.id)
	require.NotNil(t, elem.data)
	assert.Equal(t, []byte{0x01}, elem.data)
}

func TestSkipBytes_LargeSkipWithNonSeeker(t *testing.T) {
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	r := &readerWithoutSeeker{data: data}
	er := newEBMLReader(r)
	require.NoError(t, er.skipBytes(50))

	buf := make([]byte, 1)
	_, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(50), buf[0])
}

func TestReadElement_UnknownSizeNonMaster(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(0x83)                                               // TrackType (non-master)
	buf.Write([]byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}) // 8-byte VInt unknown size

	er := newEBMLReader(&buf)
	_, err := er.readElement()
	assert.Error(t, err)
}

func TestReadVintSize_LargeValue(t *testing.T) {
	input := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00}
	val, n, err := readVintSize(bytes.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, 8, n)
	assert.Equal(t, int64(0x1000), val)
}

func TestParseString_SpecialChars(t *testing.T) {
	input := []byte("V_MPEG4/ISO/AVC")
	result := parseString(input)
	assert.True(t, strings.HasPrefix(result, "V_MPEG4"))
}

func TestParseUint_SingleByte(t *testing.T) {
	assert.Equal(t, uint64(1), parseUint([]byte{0x01}))
	assert.Equal(t, uint64(0xFF), parseUint([]byte{0xFF}))
}

func TestParseFloatEBML_NegativeFloat32(t *testing.T) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], math.Float32bits(-3.14))
	result := parseFloatEBML(buf[:])
	assert.InDelta(t, -3.14, result, 0.001)
}

func TestParseFloatEBML_NegativeFloat64(t *testing.T) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(-2.718))
	result := parseFloatEBML(buf[:])
	assert.InDelta(t, -2.718, result, 0.001)
}
