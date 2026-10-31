package metadatafetch

import (
	"context"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These are low-level transport contracts. Fetch validates URLs and context
// earlier; these tests do not claim additional public Fetch-path coverage.
// Keep the constructor's real resolver/dialer: no callbacks are replaced.
func TestRoundTripRejectsMalformedTargetBeforeNetwork(t *testing.T) {
	options := networkOptions()
	client, err := New(options)
	require.NoError(t, err)
	for _, raw := range []string{"https:///missing-host", "https://example.invalid:", "https://example.invalid:65536"} {
		t.Run(raw, func(t *testing.T) {
			target, err := url.Parse(raw)
			require.NoError(t, err)
			before := *target
			response, cleanup, err := client.roundTrip(t.Context(), target)
			require.ErrorIs(t, err, ErrURL)
			require.Equal(t, ErrURL, err)
			require.Nil(t, response)
			require.Nil(t, cleanup, "failed setup retains cleanup ownership; caller receives no live resource")
			require.Equal(t, before, *target)
			require.NoError(t, t.Context().Err(), "per-request cleanup must not cancel the caller")
		})
	}
	require.Equal(t, options, client.options)
}

func TestPublicIPsPreCancelledContextDoesNotResolve(t *testing.T) {
	options := networkOptions()
	client, err := New(options)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ips, err := client.publicIPs(ctx, "must-not-resolve.invalid")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, context.Canceled, err)
	require.Nil(t, ips)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, options, client.options)

	// A fresh context still accepts a public literal without DNS or a socket.
	ips, err = client.publicIPs(t.Context(), "8.8.8.8")
	require.NoError(t, err)
	require.Len(t, ips, 1)
	require.True(t, net.ParseIP("8.8.8.8").Equal(ips[0]))
}

func TestContextDialerRefusesLegacyContextFreeDial(t *testing.T) {
	realDialer := &net.Dialer{Timeout: time.Second}
	before := *realDialer
	adapter := contextDialer{dial: realDialer.DialContext}
	// The SOCKS adapter must never fall back to a context-free connection,
	// even when it has a valid concrete context-aware dialer available.
	conn, err := adapter.Dial("tcp", "must-not-dial.invalid:443")
	require.ErrorIs(t, err, ErrConnect)
	require.Equal(t, ErrConnect, err)
	require.Nil(t, conn)
	require.Equal(t, before, *realDialer)
}
