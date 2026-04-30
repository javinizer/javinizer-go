package mediainfo

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

type ebmlElement struct {
	id   uint32
	size int64
	data []byte
}

func readElementID(r io.Reader) (uint32, int, error) {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, 0, err
	}
	b := buf[0]

	var length int
	switch {
	case b&0x80 != 0:
		length = 1
	case b&0x40 != 0:
		length = 2
	case b&0x20 != 0:
		length = 3
	case b&0x10 != 0:
		length = 4
	default:
		return 0, 0, fmt.Errorf("unsupported element ID length (byte: 0x%02x)", b)
	}

	value := uint32(b)
	if length > 1 {
		rest := make([]byte, length-1)
		if _, err := io.ReadFull(r, rest); err != nil {
			return 0, 0, err
		}
		for _, rb := range rest {
			value = (value << 8) | uint32(rb)
		}
	}

	return value, length, nil
}

func readVintSize(r io.Reader) (int64, int, error) {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, 0, err
	}
	b := buf[0]

	var length int
	var value int64
	switch {
	case b&0x80 != 0:
		length = 1
		value = int64(b & 0x7F)
	case b&0x40 != 0:
		length = 2
		value = int64(b & 0x3F)
	case b&0x20 != 0:
		length = 3
		value = int64(b & 0x1F)
	case b&0x10 != 0:
		length = 4
		value = int64(b & 0x0F)
	case b&0x08 != 0:
		length = 5
		value = int64(b & 0x07)
	case b&0x04 != 0:
		length = 6
		value = int64(b & 0x03)
	case b&0x02 != 0:
		length = 7
		value = int64(b & 0x01)
	case b&0x01 != 0:
		length = 8
		value = 0
	default:
		return 0, 0, fmt.Errorf("invalid vint size leading byte: 0x%02x", b)
	}

	if length > 1 {
		rest := make([]byte, length-1)
		if _, err := io.ReadFull(r, rest); err != nil {
			return 0, 0, err
		}
		for _, rb := range rest {
			value = (value << 8) | int64(rb)
		}
	}

	if value == 0x7FFFFFFFFFFFFFFF>>(8-length)&0x7FFFFFFFFFFFFFFF {
		return -1, length, nil
	}

	return value, length, nil
}

type ebmlReader struct {
	r io.Reader
}

func newEBMLReader(r io.Reader) *ebmlReader {
	return &ebmlReader{r: r}
}

func (er *ebmlReader) readElement() (*ebmlElement, error) {
	id, idLen, err := readElementID(er.r)
	if err != nil {
		return nil, err
	}

	size, sizeLen, err := readVintSize(er.r)
	if err != nil {
		return nil, err
	}

	_ = idLen
	_ = sizeLen

	elem := &ebmlElement{
		id:   id,
		size: size,
	}

	knownMaster := isKnownMasterElement(id)
	if knownMaster && size != 0 {
		elem.data = nil
		return elem, nil
	}

	if size < 0 {
		return nil, fmt.Errorf("unknown size for non-master element 0x%X", id)
	}

	const maxElementSize = 16 * 1024 * 1024
	if size > maxElementSize {
		if err := er.skipBytes(size); err != nil {
			return nil, fmt.Errorf("failed to skip oversized element 0x%X (%d bytes): %w", id, size, err)
		}
		elem.size = size
		elem.data = nil
		return elem, nil
	}

	if size > 0 {
		data := make([]byte, size)
		if _, err := io.ReadFull(er.r, data); err != nil {
			return nil, err
		}
		elem.data = data
	}

	return elem, nil
}

func isKnownMasterElement(id uint32) bool {
	switch id {
	case 0x1A45DFA3, 0x18538067, 0x1549A966, 0x1654AE6B,
		0xAE, 0xE0, 0xE1, 0x1C53BB6B, 0x1F43B675:
		return true
	}
	return false
}

func (er *ebmlReader) skipBytes(n int64) error {
	if n <= 0 {
		return nil
	}
	if seeker, ok := er.r.(io.Seeker); ok {
		_, err := seeker.Seek(n, io.SeekCurrent)
		return err
	}
	_, err := io.CopyN(io.Discard, er.r, n)
	return err
}

func parseUint(data []byte) uint64 {
	if len(data) == 0 {
		return 0
	}
	val := uint64(0)
	for _, b := range data {
		val = (val << 8) | uint64(b)
	}
	return val
}

func parseFloatEBML(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}
	switch len(data) {
	case 4:
		bits := binary.BigEndian.Uint32(data)
		return float64(math.Float32frombits(bits))
	case 8:
		bits := binary.BigEndian.Uint64(data)
		return math.Float64frombits(bits)
	default:
		return 0
	}
}

func parseString(data []byte) string {
	return string(data)
}
