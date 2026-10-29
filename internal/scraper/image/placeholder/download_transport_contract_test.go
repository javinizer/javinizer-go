package placeholder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
)

// The HEAD probe can succeed even when the subsequent GET cannot be transported.
// Such an inconclusive check must not remove an otherwise usable screenshot URL.
func TestPlaceholderDownloadTransportFailurePreservesCandidates(t *testing.T) {
	const base = "https://images.example"
	const placeholderBody = "owned placeholder fixture"
	const ordinaryBody = "owned ordinary image fixture"
	downloadErr := errors.New("fixture download transport unavailable")
	digest := sha256.Sum256([]byte(placeholderBody))
	cfg := Config{Enabled: true, Threshold: 1024, Hashes: []string{hex.EncodeToString(digest[:])}}
	urls := []string{base + "/unavailable.jpg", base + "/placeholder.jpg", base + "/ordinary.jpg"}
	var calls []string
	var bodies []*transportContractBody
	client := resty.New().SetRetryCount(0).SetHeader("X-Fixture", "preserved").SetTransport(placeholderRejectTransport(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.String())
		require.Equal(t, "images.example", r.URL.Host)
		require.Equal(t, "preserved", r.Header.Get("X-Fixture"))
		require.NoError(t, r.Context().Err())
		require.Nil(t, r.Body)
		switch r.Method {
		case http.MethodHead:
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: r}, nil
		case http.MethodGet:
			if r.URL.Path == "/unavailable.jpg" {
				return nil, downloadErr
			}
			content := ordinaryBody
			if r.URL.Path == "/placeholder.jpg" {
				content = placeholderBody
			} else {
				require.Equal(t, "/ordinary.jpg", r.URL.Path)
			}
			body := &transportContractBody{Reader: strings.NewReader(content)}
			bodies = append(bodies, body)
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: r}, nil
		default:
			t.Fatalf("unexpected method %s", r.Method)
			return nil, errors.New("unexpected fixture request")
		}
	}))

	found, err := isPlaceholder(context.Background(), client, urls[0], cfg)
	require.False(t, found)
	require.ErrorIs(t, err, downloadErr)
	require.ErrorContains(t, err, "download failed")
	require.Equal(t, []string{"HEAD " + urls[0], "GET " + urls[0]}, calls)

	calls = nil
	kept, count, err := FilterURLs(context.Background(), client, urls, cfg)
	require.NoError(t, err)
	require.Equal(t, []string{urls[0], urls[2]}, kept)
	require.Equal(t, 1, count)
	require.Equal(t, []string{
		"HEAD " + urls[0], "GET " + urls[0],
		"HEAD " + urls[1], "GET " + urls[1],
		"HEAD " + urls[2], "GET " + urls[2],
	}, calls)
	require.Equal(t, []string{base + "/unavailable.jpg", base + "/placeholder.jpg", base + "/ordinary.jpg"}, urls)
	require.Len(t, bodies, 2)
	for _, body := range bodies {
		require.Equal(t, 1, body.closes)
	}
}

type transportContractBody struct {
	io.Reader
	closes int
}

func (b *transportContractBody) Close() error { b.closes++; return nil }
