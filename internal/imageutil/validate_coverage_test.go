package imageutil

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRemoteImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := ValidateRemoteImage(context.Background(), server.URL); err == nil {
		t.Fatal("expected SSRF error for localhost, got nil")
	}
}

// A globally customized default transport whose HTTPS dialing is unpinnable
// must be refused, same as an explicitly supplied unpinnable transport.
func TestValidateRemoteImageWithSafeClientRejectsUnpinnableDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = &http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	}}
	err := ValidateRemoteImageWithSafeClient(context.Background(), &http.Client{}, "http://93.184.216.34/img.jpg", "agent", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "DialTLS")
}

// A literal public origin needs no DNS; its 302 to the link-local metadata
// address must be stopped by the redirect guard without any network access.
func TestValidateRemoteImageWithSafeClientBlocksRedirectToLinkLocal(t *testing.T) {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return respondWith("HTTP/1.1 302 Found\r\nLocation: http://169.254.169.254/meta\r\nContent-Length: 0\r\n\r\n")(ctx, network, addr)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: dial}}
	err := ValidateRemoteImageWithSafeClient(context.Background(), client, "http://93.184.216.34/image.jpg", "test", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "SSRF blocked")
}

// A caller-provided CheckRedirect that always returns nil must NOT lift the
// hop cap; validation stops after 10 hops regardless.
func TestValidateRemoteImageWithSafeClientCapsRegardlessOfCallerPolicy(t *testing.T) {
	hops := 0
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		hops++
		return respondWith("HTTP/1.1 302 Found\r\nLocation: http://93.184.216.34/redirect\r\nContent-Length: 0\r\n\r\n")(ctx, network, addr)
	}
	allowAll := &http.Transport{DialContext: dial}
	client := &http.Client{Transport: allowAll, CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	err := ValidateRemoteImageWithSafeClient(context.Background(), client, "http://93.184.216.34/x", "", "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "stopped after 10 redirects")
	if hops < 10 {
		t.Errorf("caller policy must not bypass the redirect cap: only %d hops", hops)
	}
}
