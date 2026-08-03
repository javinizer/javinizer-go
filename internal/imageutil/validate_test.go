package imageutil

import (
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type validationTransport func(*http.Request) (*http.Response, error)

func (f validationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestValidateRemoteImageWithSafeClientBlocksPrivateRedirect(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: validationTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	err := ValidateRemoteImageWithSafeClient(context.Background(), client, "https://example.com/image.jpg", "test-agent", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "SSRF blocked")
	require.Equal(t, 1, calls)
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
