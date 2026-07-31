package imageutil

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRemoteImageWithSafeClientGuards(t *testing.T) {
	require.Error(t, ValidateRemoteImageWithSafeClient(t.Context(), nil, "https://example.com/a.jpg", "", ""))
	require.Error(t, ValidateRemoteImageWithSafeClient(t.Context(), http.DefaultClient, "http://127.0.0.1/a.jpg", "", ""))
}

func TestValidateRemoteImageWithSafeClientHonorsRedirectPolicy(t *testing.T) {
	policyErr := errors.New("redirect denied")
	client := &http.Client{
		Transport: validationTransport(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://example.org/image.png"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error { return policyErr },
	}
	err := ValidateRemoteImageWithSafeClient(t.Context(), client, "https://example.com/start", "agent", "")
	require.ErrorIs(t, err, policyErr)
}

func TestValidateRemoteImageWithSafeClientWrapsHTTPTransportAndLimitsRedirects(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := ValidateRemoteImageWithSafeClient(ctx, &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}, "https://example.com/image", "", "")
	require.Error(t, err)

	redirects := 0
	client := &http.Client{Transport: validationTransport(func(req *http.Request) (*http.Response, error) {
		redirects++
		location := fmt.Sprintf("https://example.com/image/%d", redirects)
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	err = ValidateRemoteImageWithSafeClient(t.Context(), client, "https://example.com/start", "", "")
	require.ErrorContains(t, err, "stopped after 10 redirects")
	assert.Equal(t, 10, redirects)
}

func TestValidateRemoteImageWithClientRequestAndTransportErrors(t *testing.T) {
	require.Error(t, ValidateRemoteImageWithClient(t.Context(), nil, "https://example.com", "", ""))
	require.Error(t, ValidateRemoteImageWithClient(t.Context(), http.DefaultClient, "://bad", "", ""))

	transportErr := errors.New("offline")
	client := &http.Client{Transport: validationTransport(func(*http.Request) (*http.Response, error) { return nil, transportErr })}
	err := ValidateRemoteImageWithClient(context.Background(), client, "https://example.com/image", "agent", "https://example.com/")
	require.ErrorIs(t, err, transportErr)
}

func TestValidateRemoteImageWithClientRejectsZeroDimensions(t *testing.T) {
	const magic = "zero-dimension-image"
	image.RegisterFormat("zero-dimension-test", magic, func(io.Reader) (image.Image, error) {
		return image.NewRGBA(image.Rect(0, 0, 0, 0)), nil
	}, func(io.Reader) (image.Config, error) {
		return image.Config{Width: 0, Height: 1}, nil
	})
	client := &http.Client{Transport: validationTransport(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "agent", req.Header.Get("User-Agent"))
		assert.Equal(t, "https://example.com/ref", req.Header.Get("Referer"))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/x-zero"}}, Body: io.NopCloser(strings.NewReader(magic)), Request: req}, nil
	})}
	err := ValidateRemoteImageWithClient(t.Context(), client, "https://example.com/image", "agent", "https://example.com/ref")
	require.ErrorContains(t, err, "dimensions are invalid")
}
