package mediainfo

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// RFC 8794 sections 6.2 and 6.3 reserve all-one VINT_DATA for unknown
// size, at every encoded width: https://www.rfc-editor.org/rfc/rfc8794.html#section-6.2
func TestEBMLUnknownSizeWidths(t *testing.T) {
	for width := 1; width <= 8; width++ {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			unknown := bytes.Repeat([]byte{0xff}, width)
			unknown[0] = byte(0xff >> uint(width-1))
			size, n, err := readVintSize(bytes.NewReader(unknown))
			require.NoError(t, err)
			require.Equal(t, width, n)
			require.Equal(t, int64(-1), size)
			master, err := newEBMLReader(bytes.NewReader(append([]byte{0x18, 0x53, 0x80, 0x67}, unknown...))).readElement()
			require.NoError(t, err)
			require.Equal(t, elemSegment, master.id)
			require.Equal(t, int64(-1), master.size)
			require.Nil(t, master.data)
			scalar, err := newEBMLReader(bytes.NewReader(append([]byte{0x83}, unknown...))).readElement()
			require.Nil(t, scalar)
			require.EqualError(t, err, "unknown size for non-master element 0x83")
			// The adjacent largest finite value and zero retain their original meaning.
			finite := bytes.Clone(unknown)
			finite[len(finite)-1]--
			size, n, err = readVintSize(bytes.NewReader(finite))
			require.NoError(t, err)
			require.Equal(t, width, n)
			require.Equal(t, int64(uint64(1)<<uint(7*width)-2), size)
			zero := make([]byte, width)
			zero[0] = byte(1 << uint(8-width))
			size, n, err = readVintSize(bytes.NewReader(zero))
			require.NoError(t, err)
			require.Equal(t, width, n)
			require.Zero(t, size)
			for length := 1; length < width; length++ {
				size, n, err = readVintSize(bytes.NewReader(unknown[:length]))
				require.Zero(t, size)
				require.Zero(t, n)
				if length == 1 {
					require.ErrorIs(t, err, io.EOF)
				} else {
					require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				}
			}
		})
	}
	size, n, err := readVintSize(bytes.NewReader([]byte{0}))
	require.Zero(t, size)
	require.Zero(t, n)
	require.EqualError(t, err, "invalid vint size leading byte: 0x00")
	size, n, err = readVintSize(bytes.NewReader(nil))
	require.Zero(t, size)
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
}

func TestMKVUnknownSegmentDoesNotTruncateAt127Bytes(t *testing.T) {
	// A streaming Segment has a one-byte unknown-size marker. The valid Void
	// payload puts Info after byte 127, where the old finite-size interpretation
	// incorrectly stopped parsing the Segment.
	payload := mkvTestEBMLElement(0xec, make([]byte, 128))
	info := append(mkvTestEBMLElement(elemTimecodeScale, uintData(1000000)), mkvTestEBMLElement(elemDuration, floatData(2000))...)
	payload = append(payload, mkvTestEBMLElement(elemInfo, info)...)
	data := append([]byte{0x18, 0x53, 0x80, 0x67, 0xff}, payload...)
	result, err := analyzeMKV(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, &VideoInfo{Container: "mkv", Duration: 2}, result)
}

func TestEBMLRejectsUnknownScalarBeforeConsumingPayload(t *testing.T) {
	// The old parser accepted this invalid scalar as a finite 127-byte value.
	r := bytes.NewReader(append([]byte{0x83, 0xff}, make([]byte, 127)...))
	elem, err := newEBMLReader(r).readElement()
	require.Nil(t, elem)
	require.EqualError(t, err, "unknown size for non-master element 0x83")
	require.Equal(t, 127, r.Len())
}
