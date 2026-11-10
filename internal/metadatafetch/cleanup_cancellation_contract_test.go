package metadatafetch

import (
	"bufio"
	"context"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/javinizer/javinizer-go/internal/imagevalidate"
	"github.com/stretchr/testify/require"
)

// An empty response has no body-read work. The transport and connection owner
// each close the raw connection. Rendezvous those calls to cancel the caller
// during cleanup, regardless of which closer arrives first. Unlike cancellation
// in Read, this guarantees that the successful response has already been read.
type cleanupCancellationConn struct {
	net.Conn
	calls       atomic.Int32
	bothClosing chan struct{}
	cancel      context.CancelFunc
	timedOut    atomic.Bool
}

func (c *cleanupCancellationConn) Close() error {
	if c.calls.Add(1) == 2 {
		c.cancel()
		close(c.bothClosing)
	}
	select {
	case <-c.bothClosing:
	case <-time.After(time.Second):
		c.timedOut.Store(true)
	}
	return c.Conn.Close()
}

func TestFetchCancellationDuringSuccessfulResponseCleanup(t *testing.T) {
	for _, imageFetch := range []bool{false, true} {
		name := "metadata"
		if imageFetch {
			name = "image"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				options := networkOptions()
				options.Limits.MaxConcurrent = 1
				client := networkClient(t, options)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var connection *cleanupCancellationConn
				var dials atomic.Int32
				client.dial = func(context.Context, string, string) (net.Conn, error) {
					raw := pipeResponse(t, func(peer net.Conn) {
						req, err := http.ReadRequest(bufio.NewReader(peer))
						if err != nil {
							t.Error(err)
							return
						}
						if !req.Close {
							t.Error("request permits connection reuse")
						}
						_, err = io.WriteString(peer, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
						if err != nil {
							t.Error(err)
						}
					})
					if dials.Add(1) == 1 {
						connection = &cleanupCancellationConn{Conn: raw, bothClosing: make(chan struct{}), cancel: cancel}
						return connection, nil
					}
					return raw, nil
				}
				started := time.Now()
				var result Result
				var err error
				if imageFetch {
					result, err = client.FetchImage(ctx, "http://localhost/image", imagevalidate.Limits{MaxEncodedBytes: 128, MaxDimension: 16, MaxPixels: 256})
				} else {
					result, err = client.Fetch(ctx, "http://localhost/metadata")
				}
				require.ErrorIs(t, err, context.Canceled, "cancellation must beat both success and invalid-image errors")
				boundaryZeroResult(t, result)
				require.NotNil(t, connection)
				require.False(t, connection.timedOut.Load(), "both cleanup owners must reach the close rendezvous")
				require.GreaterOrEqual(t, connection.calls.Load(), int32(2))
				require.Equal(t, started, time.Now(), "cleanup must not wait for any deadline")
				require.Empty(t, client.slots)
				require.Equal(t, int32(1), dials.Load())

				// The canceled fetch must release its only slot and leave the client usable.
				recovered, err := client.Fetch(t.Context(), "http://localhost/recovered")
				require.NoError(t, err)
				require.Empty(t, recovered.Bytes)
				require.Equal(t, "http://localhost/recovered", recovered.FinalURL)
				require.Equal(t, sha256.Sum256(nil), recovered.SHA256)
				require.Equal(t, int32(2), dials.Load())
				require.Empty(t, client.slots)
			})
		})
	}
}
