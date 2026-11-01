package placeholder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
)

func TestFilterURLsPreservesDisabledAndEmptySlices(t *testing.T) {
	for _, tc := range []struct {
		name    string
		urls    []string
		enabled bool
	}{
		{name: "enabled nil", enabled: true},
		{name: "enabled empty", urls: []string{}, enabled: true},
		{name: "disabled nil"},
		{name: "disabled empty", urls: []string{}},
		{name: "disabled duplicates and invalid", urls: []string{"bad\x00url", "same", "same"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A nil client is valid here: disabled/empty input must perform no I/O.
			kept, count := FilterURLs(t.Context(), nil, tc.urls, Config{Enabled: tc.enabled})
			require.Zero(t, count)
			require.Equal(t, tc.urls, kept)
			require.Equal(t, tc.urls == nil, kept == nil)
			if len(tc.urls) > 0 {
				require.Same(t, &tc.urls[0], &kept[0], "disabled filtering returns the original slice")
			}
		})
	}
}

func TestFilterURLsMixedResultsPreserveDuplicatesOrderAndCallbacks(t *testing.T) {
	const base = "https://placeholder.invalid"
	const placeholder = "owned placeholder"
	digest := sha256.Sum256([]byte(placeholder))
	cfg := Config{Enabled: true, Threshold: 1024, Hashes: []string{hex.EncodeToString(digest[:])}}
	urls := []string{base + "/ordinary", base + "/unavailable", base + "/placeholder", base + "/ordinary", base + "/placeholder"}
	before := append([]string(nil), urls...)
	beforeHashes := append([]string(nil), cfg.Hashes...)
	var requests, beforeHooks, afterHooks []string
	var bodies []*transportContractBody
	client := resty.New().SetRetryCount(0).SetHeader("X-Fixture", "preserved")
	client.OnBeforeRequest(func(_ *resty.Client, req *resty.Request) error {
		beforeHooks = append(beforeHooks, req.Method+" "+req.URL)
		return nil
	})
	client.OnAfterResponse(func(_ *resty.Client, response *resty.Response) error {
		afterHooks = append(afterHooks, response.Request.Method+" "+response.Request.URL)
		return nil
	})
	client.SetTransport(placeholderRejectTransport(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Method+" "+req.URL.String())
		require.Equal(t, "placeholder.invalid", req.URL.Host)
		require.Equal(t, "preserved", req.Header.Get("X-Fixture"))
		require.NoError(t, req.Context().Err())
		response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}
		if req.Method == http.MethodHead {
			if req.URL.Path == "/unavailable" {
				response.StatusCode = http.StatusServiceUnavailable
			}
			return response, nil
		}
		require.Equal(t, http.MethodGet, req.Method)
		content := "owned ordinary image"
		if req.URL.Path == "/placeholder" {
			content = placeholder
		} else {
			require.Equal(t, "/ordinary", req.URL.Path)
		}
		body := &transportContractBody{Reader: strings.NewReader(content)}
		bodies = append(bodies, body)
		response.Body = body
		return response, nil
	}))

	kept, count := FilterURLs(t.Context(), client, urls, cfg)
	require.Equal(t, []string{urls[0], urls[1], urls[3]}, kept)
	require.Equal(t, 2, count, "each confirmed placeholder occurrence is counted")
	require.Equal(t, before, urls)
	require.Equal(t, beforeHashes, cfg.Hashes)
	var wantRequests, wantHooks []string
	for _, url := range urls {
		wantHooks = append(wantHooks, "HEAD "+url)
		wantRequests = append(wantRequests, "HEAD "+url)
		if url != base+"/unavailable" {
			wantRequests = append(wantRequests, "GET "+url)
		}
	}
	require.Equal(t, wantRequests, requests, "fail-open continues; duplicates are not deduplicated")
	require.Equal(t, wantHooks, beforeHooks)
	require.Equal(t, wantHooks, afterHooks, "HEAD hooks remain intact; existing raw GET does not invoke Resty hooks")
	require.Len(t, bodies, 4)
	for _, body := range bodies {
		require.Equal(t, 1, body.closes)
	}
}

func TestFilterURLsCancellationRemainsPerItemFailOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	urls := []string{"https://placeholder.invalid/one", "https://placeholder.invalid/one", ""}
	before := append([]string(nil), urls...)
	client := resty.New().SetRetryCount(0).SetTransport(placeholderRejectTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodHead, req.Method, "canceled checks must not download bodies")
		require.ErrorIs(t, req.Context().Err(), context.Canceled)
		return nil, req.Context().Err()
	}))
	kept, count := FilterURLs(ctx, client, urls, Config{Enabled: true, Threshold: 1024})
	require.Zero(t, count)
	require.Equal(t, before, kept)
	require.Equal(t, before, urls)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}
