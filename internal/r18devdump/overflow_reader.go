package r18devdump

import (
	"fmt"
	"io"
)

type overflowReader struct {
	r       io.Reader
	max     int64
	n       int64
	atLimit bool
}

func (o *overflowReader) Read(p []byte) (int, error) {
	if o.atLimit {
		probe := make([]byte, 1)
		n, err := o.r.Read(probe)
		if n > 0 {
			return 0, fmt.Errorf("r18dev dump: decompressed size exceeds %d bytes (possible corrupt or hostile dump)", o.max)
		}
		return 0, err
	}
	remaining := o.max - o.n
	if remaining <= 0 {
		o.atLimit = true
		probe := make([]byte, 1)
		n, err := o.r.Read(probe)
		if n > 0 {
			return 0, fmt.Errorf("r18dev dump: decompressed size exceeds %d bytes (possible corrupt or hostile dump)", o.max)
		}
		return 0, err
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := o.r.Read(p)
	o.n += int64(n)
	if o.n >= o.max {
		o.atLimit = true
	}
	return n, err
}
