package r18devdump

import (
	"errors"
	"io"
)

// MaxDecompressedDumpBytes bounds acceptable uncompressed dump data. The real
// r18.dev dump decompresses to a few GiB; this ceiling leaves ample headroom
// while refusing gzip bombs that would otherwise stream-write tens of GB into
// the temporary SQLite file (CPU + disk exhaustion).
const MaxDecompressedDumpBytes int64 = 20 << 30

// ErrDumpTooLarge is returned by a bounded dump reader once the decompressed
// stream crosses the limit.
var ErrDumpTooLarge = errors.New("dump exceeds the maximum supported decompressed size")

// EnforceDumpSizeLimit bounds a decompressed dump stream: reads past the byte
// ceiling fail with ErrDumpTooLarge instead of silently truncating (a bare
// LimitReader would also produce ErrTruncatedDump from the eventual EOF with
// no size signal for classification).
func EnforceDumpSizeLimit(r io.Reader, max int64) io.Reader {
	return &sizeLimitReader{r: r, left: max}
}

type sizeLimitReader struct {
	r    io.Reader
	left int64
}

func (s *sizeLimitReader) Read(p []byte) (int, error) {
	if s.left <= 0 {
		// Codex: content landing exactly on the cap must read its EOF cleanly;
		// only data beyond the cap is a real overflow.
		one := make([]byte, 1)
		n, err := s.r.Read(one)
		if n == 0 || err != nil {
			if err == nil {
				err = io.EOF
			}
			return 0, err
		}
		return 0, ErrDumpTooLarge
	}
	if int64(len(p)) > s.left {
		p = p[:s.left]
	}
	n, err := s.r.Read(p)
	s.left -= int64(n)
	if err != nil {
		return n, err
	}
	return n, nil
}
