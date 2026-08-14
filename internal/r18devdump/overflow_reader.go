package r18devdump

import (
	"fmt"
	"io"
)

type overflowReader struct {
	r   io.Reader
	max int64
	n   int64
}

func (o *overflowReader) Read(p []byte) (int, error) {
	remaining := o.max - o.n
	if remaining < 0 {
		return 0, fmt.Errorf("r18dev dump: decompressed size exceeds %d bytes (possible corrupt or hostile dump)", o.max)
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := o.r.Read(p)
	o.n += int64(n)
	if o.n > o.max {
		return n, fmt.Errorf("r18dev dump: decompressed size exceeds %d bytes (possible corrupt or hostile dump)", o.max)
	}
	return n, err
}
