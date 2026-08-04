package actresscache

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetcherDeclinesPinningCustomTLSDialerTransport(t *testing.T) {
	transport := &http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("must never be called")
	}}
	fetcher := NewFetcher(&http.Client{Transport: transport}, 0, "test")
	assert.False(t, fetcher.resolveTargets, "unpinnable transport must not be wrapped")
	// Request-layer guard still blocks literal/internal targets.
	_, _, err := fetcher.Get(context.Background(), "http://127.0.0.1:9/x", "*/*", 64)
	var blockedErr *BlockedFetchError
	require.True(t, errors.As(err, &blockedErr))
}
