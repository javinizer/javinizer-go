package mediainfo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// RIFFChunk represents a single RIFF chunk read from a container stream.
// Data contains the chunk payload (excluding the 8-byte header and any
// word-alignment padding). For LIST chunks, Data includes the 4-byte
// list-type identifier followed by the sub-chunks.
type RIFFChunk struct {
	FourCC string
	Size   uint32
	Data   []byte
}

// ChunkReader yields RIFF chunks as []byte from an io.ReadSeeker.
// Stream-info extraction receives []byte instead of *os.File + offsets,
// making it testable without filesystem dependencies.
type ChunkReader struct {
	r io.ReadSeeker
}

// NewChunkReader creates a ChunkReader from an io.ReadSeeker.
func NewChunkReader(r io.ReadSeeker) *ChunkReader {
	return &ChunkReader{r: r}
}

// ReadChunks reads all top-level RIFF chunks from the stream.
// It skips the initial RIFF header and returns the form type (e.g., "AVI ")
// along with the chunks. Each chunk's Data field contains the full payload
// so that sub-chunk parsing can operate on []byte.
func (cr *ChunkReader) ReadChunks() (formType string, chunks []RIFFChunk, err error) {
	// Read and validate RIFF header
	var riffHeader riffChunk
	if err := binary.Read(cr.r, binary.LittleEndian, &riffHeader); err != nil {
		return "", nil, fmt.Errorf("failed to read RIFF header: %w", err)
	}
	if string(riffHeader.FourCC[:]) != "RIFF" {
		return "", nil, fmt.Errorf("invalid RIFF signature")
	}

	// Read form type (e.g., "AVI ")
	var ft [4]byte
	if err := binary.Read(cr.r, binary.LittleEndian, &ft); err != nil {
		return "", nil, fmt.Errorf("failed to read form type: %w", err)
	}

	for {
		chunk, err := cr.readChunk()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", nil, err
		}
		chunks = append(chunks, *chunk)
	}

	return string(ft[:]), chunks, nil
}

// readChunk reads a single RIFF chunk from the current position.
func (cr *ChunkReader) readChunk() (*RIFFChunk, error) {
	var header riffChunk
	if err := binary.Read(cr.r, binary.LittleEndian, &header); err != nil {
		return nil, err
	}

	fourCC := string(header.FourCC[:])
	dataSize := header.Size

	data := make([]byte, dataSize)
	if dataSize > 0 {
		n, err := io.ReadFull(cr.r, data)
		if err != nil && err != io.ErrUnexpectedEOF {
			if err == io.EOF {
				// Truncated chunk — return partial data rather than failing
				return &RIFFChunk{
					FourCC: fourCC,
					Size:   uint32(n),
					Data:   data[:n],
				}, nil
			}
			return nil, fmt.Errorf("failed to read %s chunk data: %w", fourCC, err)
		}
		if err == io.ErrUnexpectedEOF {
			// Truncated chunk — return partial data rather than failing
			return &RIFFChunk{
				FourCC: fourCC,
				Size:   uint32(n),
				Data:   data[:n],
			}, nil
		}
	}

	// Word alignment: RIFF chunks are word-aligned
	if dataSize%2 != 0 {
		var pad [1]byte
		if _, err := cr.r.Read(pad[:]); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("failed to skip word-alignment byte: %w", err)
		}
	}

	return &RIFFChunk{
		FourCC: fourCC,
		Size:   header.Size,
		Data:   data,
	}, nil
}
