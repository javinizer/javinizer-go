package imageutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRemoteImageWithSafeClientRejectsTLSDialerTransport(t *testing.T) {
	client := &http.Client{Transport: &http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("must never be called")
	}}}
	err := ValidateRemoteImageWithSafeClient(context.Background(), client, "http://1.1.1.1/x.jpg", "agent", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "DialTLSContext")
}
