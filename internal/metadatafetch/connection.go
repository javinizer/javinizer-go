package metadatafetch

import (
	"context"
	"net"
	"sync"
)

// connectionOwner keeps raw direct/SOCKS socket lifetime inside the fetch slot.
// Closing a transport's TLS connection can otherwise spend five seconds writing
// close_notify after Body.Close has returned. Close the raw socket first, and
// wait for context-aware dial work (including a late successful dial) to finish.
type connectionOwner struct {
	mu        sync.Mutex
	closed    bool
	conn      net.Conn
	dials     sync.WaitGroup
	closeOnce sync.Once
	cancel    context.CancelFunc
}

func (o *connectionOwner) connect(ctx context.Context, network, port string, ips []net.IP, dial dialer) (net.Conn, error) {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil, ErrConnect
	}
	o.dials.Add(1)
	o.mu.Unlock()
	defer o.dials.Done()
	conn, err := dialCandidates(ctx, network, port, ips, dial)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	if o.closed || o.conn != nil {
		o.mu.Unlock()
		_ = conn.Close()
		return nil, ErrConnect
	}
	o.conn = conn
	o.mu.Unlock()
	return conn, nil
}

func (o *connectionOwner) close() {
	o.closeOnce.Do(func() {
		o.mu.Lock()
		o.closed = true
		conn := o.conn
		o.mu.Unlock()
		o.cancel()
		if conn != nil {
			_ = conn.Close()
		}
		// closed is set under the same lock used by connect's Add, so no new
		// dial can be admitted after this Wait starts. Late sockets are closed
		// by connect before it calls Done.
		o.dials.Wait()
	})
}
