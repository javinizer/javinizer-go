package actresscache

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type validateTransport func(*http.Request) (*http.Response, error)

func (f validateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func makeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 40, B: 60, A: 255})
		}
	}
	var body bytes.Buffer
	require.NoError(t, jpeg.Encode(&body, img, nil))
	return body.Bytes()
}

func testFetcher(status int, contentType string, body []byte, requestErr error) *Fetcher {
	client := &http.Client{Transport: validateTransport(func(req *http.Request) (*http.Response, error) {
		if requestErr != nil {
			return nil, requestErr
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})}
	// Fixture transport: opt in so the canned RoundTripper is used verbatim.
	f, err := NewFetcherWithOptions(client, 0, "test", nil, true)
	if err != nil {
		panic(err)
	}
	return f
}

func TestValidateThumbnailAcceptsDecodedImage(t *testing.T) {
	body := makeJPEG(t, 80, 90)
	validation, err := ValidateThumbnail(context.Background(), testFetcher(http.StatusOK, "image/jpeg", body, nil), "https://example.test/photo.jpg", 64, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, 80, validation.Width)
	assert.Equal(t, 90, validation.Height)
	assert.Equal(t, "jpeg", validation.Format)
	assert.NotEmpty(t, validation.SHA256)
}

func TestValidateThumbnailRejectsInvalidInputs(t *testing.T) {
	cases := []string{
		"",
		"not a URL",
		"https://pics.dmm.co.jp/mono/noimage/now_printing.jpg",
		"https://pics.dmm.co.jp/mono/actjpgs/no-extension",
		"https://www.minnano-av.com/p_actress_125_125/000/np.gif",
	}
	for _, rawURL := range cases {
		t.Run(rawURL, func(t *testing.T) {
			_, err := ValidateThumbnail(context.Background(), testFetcher(http.StatusOK, "image/jpeg", makeJPEG(t, 80, 80), nil), rawURL, 64, 1<<20)
			var rejected *ThumbnailRejectedError
			assert.ErrorAs(t, err, &rejected)
		})
	}
}

func TestValidateThumbnailKeepsTransientHTTPFailuresRetryable(t *testing.T) {
	client := &http.Client{Transport: validateTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
	})}
	_, err := ValidateThumbnail(context.Background(), mustFetcher(NewFetcherWithOptions(client, 0, "test", nil, true)), "https://example.test/photo.jpg", 64, 1<<20)
	require.Error(t, err)
	var rejected *ThumbnailRejectedError
	assert.False(t, errors.As(err, &rejected))
	var statusErr *HTTPError
	require.ErrorAs(t, err, &statusErr)
	assert.True(t, statusErr.IsTransient())
}

func TestValidateThumbnailRejectsBadResponses(t *testing.T) {
	body := makeJPEG(t, 80, 80)
	cases := []struct {
		name        string
		status      int
		contentType string
		body        []byte
		wantReason  string
	}{
		{"status", http.StatusNotFound, "text/html", []byte("missing"), "HTTP 404"},
		{"content type", http.StatusOK, "text/html", body, "content type"},
		{"decode", http.StatusOK, "image/jpeg", []byte("not an image"), "decode"},
		{"dimensions", http.StatusOK, "image/jpeg", makeJPEG(t, 8, 8), "dimensions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateThumbnail(context.Background(), testFetcher(tc.status, tc.contentType, tc.body, nil), "https://example.test/photo.jpg", 64, 1<<20)
			var rejected *ThumbnailRejectedError
			require.ErrorAs(t, err, &rejected)
			assert.Contains(t, rejected.Error(), tc.wantReason)
		})
	}
}

func TestValidateThumbnailReturnsTransportErrorsForRetry(t *testing.T) {
	_, err := ValidateThumbnail(context.Background(), testFetcher(http.StatusOK, "", nil, errors.New("connection reset")), "https://example.test/photo.jpg", 64, 1<<20)
	assert.Error(t, err)
	var rejected *ThumbnailRejectedError
	assert.False(t, errors.As(err, &rejected))
}

func TestValidateThumbnailRejectsOversizedResponse(t *testing.T) {
	body := makeJPEG(t, 80, 80)
	_, err := ValidateThumbnail(context.Background(), testFetcher(http.StatusOK, "image/jpeg", body, nil), "https://example.test/photo.jpg", 64, int64(len(body)-1))
	var rejected *ThumbnailRejectedError
	assert.False(t, errors.As(err, &rejected))
	assert.Contains(t, err.Error(), "exceeds")
}

// TestValidateThumbnailTreatsUnverifiableHostAsTransient pins SEC-1: a hostname
// that cannot be proven public (fail-closed DNS under a proxy) must surface as
// a transient error so the builder records "failed", never "rejected".
func TestValidateThumbnailTreatsUnverifiableHostAsTransient(t *testing.T) {
	proxied := &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
		return url.Parse("http://proxy.corp.example:3128")
	}}
	prev := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("dns down")
	}
	defer func() { lookupIP = prev }()
	fetcher := mustFetcher(NewFetcher(&http.Client{Transport: proxied}, 0, "test"))
	_, err := ValidateThumbnail(context.Background(), fetcher, "https://img.example-invalid.test/x.jpg", 0, 1024)
	require.Error(t, err)
	var rejected *ThumbnailRejectedError
	require.False(t, errors.As(err, &rejected), "DNS-unverifiable must not become a permanent rejection")
	var unverifiable *ssrf.UnverifiableHostError
	require.True(t, errors.As(err, &unverifiable))
}
