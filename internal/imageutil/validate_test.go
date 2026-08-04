package imageutil

import (
	"bufio"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type validationTransport func(*http.Request) (*http.Response, error)

func (f validationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// respondWith returns a DialContext that answers every dial with a canned
// HTTP/1.1 response over an in-memory pipe. Lets redirect/validation tests
// drive a real *http.Transport (fail-closed contract) without network or TLS.
func respondWith(response string) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) {
		clientSide, serverSide := net.Pipe()
		go func() {
			defer func() { _ = serverSide.Close() }()
			br := bufio.NewReader(serverSide)
			if _, err := http.ReadRequest(br); err != nil {
				return
			}
			_, _ = io.WriteString(serverSide, response)
		}()
		return clientSide, nil
	}
}

func TestValidateRemoteImageWithSafeClientBlocksPrivateRedirect(t *testing.T) {
	calls := 0
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls++
		return respondWith("HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1/private\r\nContent-Length: 0\r\n\r\n")(ctx, network, addr)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: dial}}
	err := ValidateRemoteImageWithSafeClient(context.Background(), client, "http://1.1.1.1/image.jpg", "test-agent", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "SSRF blocked")
	require.Equal(t, 1, calls)
}

func TestValidateRemoteImageWithSafeClientRejectsCustomTransport(t *testing.T) {
	client := &http.Client{Transport: validationTransport(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("must never be called")
	})}
	err := ValidateRemoteImageWithSafeClient(context.Background(), client, "http://1.1.1.1/image.jpg", "agent", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "*http.Transport")
}

func TestValidateRemoteImageWithClient(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "valid image",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				require.NoError(t, png.Encode(w, image.NewRGBA(image.Rect(0, 0, 2, 2))))
			},
		},
		{
			name: "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
		{
			name: "html response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html>blocked</html>"))
			},
			wantErr: true,
		},
		{
			name: "corrupt image",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write([]byte("not an image"))
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			err := ValidateRemoteImageWithClient(context.Background(), server.Client(), server.URL, "test-agent", server.URL)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
