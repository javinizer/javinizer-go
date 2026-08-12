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

func TestFetcherRejectsCustomTLSDialerTransport(t *testing.T) {
	transport := &http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("must never be called")
	}}
	// A custom TLS dialer bypasses DialContext for HTTPS and cannot be
	// pinned; with egress pinning mandatory the fetcher must refuse it
	// rather than silently downgrading the guard to lexical-only checks.
	_, err := NewFetcher(&http.Client{Transport: transport}, 0, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DialTLS")

	// Trusted local mirrors may opt in: the custom transport is kept
	// verbatim and no resolution is promised.
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, true)
	require.NoError(t, err)
	assert.False(t, fetcher.resolveTargets, "allowed-private custom TLS dialer stays unpinned")
}
