package placeholder

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPlaceholderNetworkFailuresPreserveCandidates(t *testing.T) {
	for _, tc := range []struct {
		name       string
		head, get  int
		truncated  bool
		wantError  string
		wantStatus int
	}{
		{name: "HEAD blocked", head: 403, wantError: "HEAD request failed", wantStatus: 403},
		{name: "download unavailable", head: 200, get: 503, wantError: "download failed", wantStatus: 503},
		{name: "download disappeared", head: 200, get: 404},
		{name: "truncated download", head: 200, get: 200, truncated: true, wantError: "unexpected EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.Method)
				mu.Unlock()
				if r.Header.Get("X-Fixture") != "preserved" {
					http.Error(w, "missing configured header", http.StatusBadRequest)
					return
				}
				if r.Method == http.MethodHead {
					w.WriteHeader(tc.head)
					return
				}
				if tc.truncated {
					w.Header().Set("Content-Length", "100")
				}
				w.WriteHeader(tc.get)
				_, _ = fmt.Fprint(w, "short")
			}))
			defer server.Close()
			client := resty.New().SetTimeout(2*time.Second).SetHeader("X-Fixture", "preserved")
			cfg := Config{Enabled: true, Threshold: 1024}
			placeholder, err := isPlaceholder(context.Background(), client, server.URL, cfg)
			require.False(t, placeholder)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			if tc.wantStatus != 0 {
				var status *models.ScraperError
				require.True(t, errors.As(err, &status))
				require.Equal(t, tc.wantStatus, status.StatusCode)
				if tc.wantStatus == 403 {
					require.Equal(t, models.ScraperErrorKindBlocked, status.Kind)
				} else {
					require.Equal(t, models.ScraperErrorKindUnavailable, status.Kind)
				}
			}
			wantMethods := []string{http.MethodHead}
			if tc.head < 400 {
				wantMethods = append(wantMethods, http.MethodGet)
			}
			mu.Lock()
			methods := append([]string(nil), requests...)
			mu.Unlock()
			require.Equal(t, wantMethods, methods)
			kept, count := FilterURLs(context.Background(), client, []string{server.URL}, cfg)
			require.Equal(t, []string{server.URL}, kept, "an inconclusive check or missing image must not discard the candidate")
			require.Zero(t, count)
		})
	}
}

func TestPlaceholderDisabledAndInvalidInputDoNotRequest(t *testing.T) {
	calls := 0
	client := resty.New().SetTransport(placeholderRejectTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("unexpected transport invocation")
	}))
	found, err := isPlaceholder(context.Background(), client, "", Config{Enabled: true})
	require.ErrorContains(t, err, "empty URL")
	require.False(t, found)
	found, err = isPlaceholder(context.Background(), client, "http://unused.invalid", Config{Enabled: false})
	require.NoError(t, err)
	require.False(t, found)
	require.Zero(t, calls)
}

func TestPlaceholderCanceledHeadPreservesError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	client := resty.New().SetTransport(placeholderRejectTransport(func(r *http.Request) (*http.Response, error) { calls++; return nil, r.Context().Err() }))
	found, err := isPlaceholder(ctx, client, "http://unused.invalid", Config{Enabled: true, Threshold: 1024})
	require.False(t, found)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "HEAD request failed")
	require.LessOrEqual(t, calls, 1)
}

type placeholderRejectTransport func(*http.Request) (*http.Response, error)

func (f placeholderRejectTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
