package placeholder

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
)

// The reader may return bytes and a terminal error together. Those bytes must
// contribute to both the size and digest before the terminal status is handled.
type terminalHashReader struct {
	data []byte
	err  error
}

func (r *terminalHashReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.err
	}
	return n, nil
}

func TestStreamAndHashRealSHA256Contract(t *testing.T) {
	failure := errors.New("source interrupted")
	for _, tc := range []struct {
		name     string
		data     []byte
		terminal error
	}{
		{"empty EOF", nil, io.EOF},
		{"bytes with EOF", []byte("placeholder payload"), io.EOF},
		{"multiple buffers", bytes.Repeat([]byte("image"), 20000), io.EOF},
		{"bytes with failure", []byte("partial image"), failure},
		{"failure without bytes", nil, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := sha256.New()
			size, err := streamAndHash(&terminalHashReader{data: tc.data, err: tc.terminal}, h)
			wantErr := tc.terminal
			if wantErr == io.EOF {
				wantErr = nil
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			if size != int64(len(tc.data)) {
				t.Fatalf("size = %d, want %d", size, len(tc.data))
			}
			expected := sha256.Sum256(tc.data)
			if !bytes.Equal(h.Sum(nil), expected[:]) {
				t.Fatal("digest does not include exactly the bytes read")
			}
		})
	}
}
