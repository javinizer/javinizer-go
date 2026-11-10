package metadatafetch

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundTripRejectsInconsistentTargetWithoutDialing(t *testing.T) {
	// These are contracts of the lower-level transport boundary, not inputs
	// accepted by Fetch's string URL validation. A URL value can carry an
	// authority in Opaque that differs from the Host validated for DNS pinning.
	for _, tc := range []struct {
		name   string
		target url.URL
		want   error
	}{
		{
			name:   "opaque authority differs from pinned host",
			target: url.URL{Scheme: "http", Host: "203.0.113.7", Opaque: "//203.0.113.8/private?token=secret"},
			want:   ErrTarget,
		},
		{
			name:   "raw query prevents request construction",
			target: url.URL{Scheme: "http", Host: "203.0.113.7", RawQuery: "token=secret\r\n"},
			want:   ErrURL,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := New(networkOptions())
			require.NoError(t, err)
			var dials atomic.Int32
			client.dial = func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				return nil, errors.New("unexpected network connection")
			}
			before := tc.target
			response, cleanup, err := client.roundTrip(t.Context(), &tc.target)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, response)
			require.Nil(t, cleanup, "failed setup must retain cleanup ownership")
			require.Zero(t, dials.Load(), "reject before opening any connection")
			require.Equal(t, before, tc.target)
			require.NoError(t, t.Context().Err())
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
