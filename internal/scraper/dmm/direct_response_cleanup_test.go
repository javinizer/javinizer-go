package dmm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type dmmDirectProbeBody struct {
	io.ReadCloser
	reads, closes atomic.Int64
	closeErr      error
}

func (b *dmmDirectProbeBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return b.ReadCloser.Read(p)
}

func (b *dmmDirectProbeBody) Close() error {
	b.closes.Add(1)
	err := b.ReadCloser.Close()
	if b.closeErr != nil {
		return b.closeErr
	}
	return err
}

func dmmDirectProbeURLs() []string {
	return []string{
		"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ssis00200/",
		"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ssis00200/",
		"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ssis00200/",
		"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ssis00200/",
		"https://video.dmm.co.jp/av/content/?id=ssis00200",
		"https://video.dmm.co.jp/amateur/content/?id=ssis00200",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=1ssis00200r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=2ssis00200r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=4ssis00200r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=5ssis00200r/",
	}
}

func TestGetURLClosesDirectProbeResponses(t *testing.T) {
	probes := dmmDirectProbeURLs()
	queries := []string{searchURLFor("ssis200"), searchURLFor("ssis-200"), searchURLFor("ssis00200")}
	for _, tc := range []struct {
		name           string
		status         int
		location       string
		redirectErr    error
		transportError bool
		contextError   bool
		closeError     bool
		cancel         bool
		wantURL        string
	}{
		{name: "nonempty_not_found", status: http.StatusNotFound},
		{name: "nonempty_server_error", status: http.StatusServiceUnavailable},
		{name: "success", status: http.StatusOK, wantURL: probes[0]},
		{name: "redirect_without_location", status: http.StatusFound, wantURL: probes[0]},
		{name: "redirect_return_last", status: http.StatusFound, location: "/probe-destination", redirectErr: http.ErrUseLastResponse, wantURL: probes[0]},
		{name: "redirect_followed", status: http.StatusFound, location: "/probe-destination", wantURL: probes[0]},
		{name: "redirect_rejected", status: http.StatusFound, location: "/probe-destination", redirectErr: errors.New("fixture redirect rejected")},
		{name: "malformed_redirect", status: http.StatusFound, location: "%invalid"},
		{name: "transport_error_then_success", status: http.StatusOK, transportError: true, wantURL: probes[0]},
		{name: "canceled_request_without_response", contextError: true, cancel: true},
		{name: "close_error_does_not_discard_candidate", status: http.StatusOK, closeError: true, wantURL: probes[0]},
		{name: "canceled_after_success_headers", status: http.StatusOK, cancel: true, wantURL: probes[0]},
		{name: "canceled_after_failure_headers", status: http.StatusNotFound, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, assertCacheUnchanged := newDMMCachedSearchContainmentScraper(t)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.redirectErr != nil {
				s.client.GetClient().CheckRedirect = func(*http.Request, []*http.Request) error { return tc.redirectErr }
			}
			type observedResponse struct {
				search bool
				body   *dmmDirectProbeBody
				resp   *http.Response
			}
			var observed []observedResponse
			var requested, directRequested []string
			// Cleanup is a failure-path safety net only. All closure assertions run
			// before cancellation or cleanup, including before the next request.
			t.Cleanup(func() {
				for _, entry := range observed {
					if entry.body.closes.Load() == 0 {
						_ = entry.resp.Body.Close()
					}
				}
			})
			s.client.SetTransport(dmmSearchContainmentTransport(func(req *http.Request) (*http.Response, error) {
				for _, entry := range observed {
					require.EqualValues(t, 1, entry.body.closes.Load(), "previous response must close before the next request")
				}
				requested = append(requested, req.URL.String())
				search := strings.HasPrefix(req.URL.Path, "/search/")
				status, content := tc.status, strings.Repeat("x", 4096)
				header := make(http.Header)
				if search {
					require.Contains(t, queries, req.URL.String())
					status, content = http.StatusOK, "<html><body>No candidates</body></html>"
				} else if req.URL.Path == "/probe-destination" {
					status = http.StatusOK
				} else {
					require.Contains(t, probes, req.URL.String(), "only known offline probes are allowed")
					directRequested = append(directRequested, req.URL.String())
					if tc.contextError {
						cancel()
						return nil, req.Context().Err()
					}
					if tc.transportError && len(directRequested) == 1 {
						return nil, errors.New("fixture transport failure")
					}
					if tc.location != "" {
						header.Set("Location", tc.location)
					}
				}
				body := &dmmDirectProbeBody{ReadCloser: io.NopCloser(strings.NewReader(content))}
				if !search && tc.closeError {
					body.closeErr = errors.New("fixture close failure")
				}
				// A known length larger than net/http's redirect slurp budget keeps
				// these checks about ownership, not incidental redirect draining.
				resp := &http.Response{StatusCode: status, Header: header, Request: req, Body: body, ContentLength: int64(len(content))}
				observed = append(observed, observedResponse{search: search, body: body, resp: resp})
				if !search && tc.cancel {
					// Exercise the ordinary headers/cancellation race without replacing
					// the caller's context or relying on a sleep.
					cancel()
				}
				return resp, nil
			}))

			got, err := s.GetURL(ctx, "SSIS-200")
			if tc.wantURL == "" {
				require.EqualError(t, err, "no scrapable URL found for movie on DMM")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantURL, got, "preserve original-probe selection and error behavior")
			if tc.cancel {
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				require.Equal(t, probes[:1], directRequested)
			} else {
				require.NoError(t, ctx.Err(), "closure must not depend on caller cancellation")
				require.Equal(t, probes, directRequested)
			}
			require.Equal(t, queries, requested[:3])
			wantResponses := 3 + len(directRequested)
			if tc.transportError || tc.contextError {
				wantResponses--
			}
			if tc.name == "redirect_followed" {
				wantResponses += len(directRequested)
			}
			require.Len(t, observed, wantResponses)
			for _, entry := range observed {
				require.EqualValues(t, 1, entry.body.closes.Load(), "response must close exactly once before GetURL returns")
				if entry.search {
					require.Positive(t, entry.body.reads.Load(), "ordinary search parsing is unchanged")
				} else {
					require.Zero(t, entry.body.reads.Load(), "direct probes must not download response bodies")
				}
			}
			assertCacheUnchanged()
		})
	}
}

func TestGetURLClosesDirectProbeLoopbackConnections(t *testing.T) {
	s, assertCacheUnchanged := newDMMCachedSearchContainmentScraper(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	released := make(chan struct{}, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/") {
			_, _ = io.WriteString(w, "<html><body>No candidates</body></html>")
			return
		}
		// Never finish this large body. Closing a probe must release its socket
		// without waiting for EOF or downloading the advertised response.
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "nonempty unfinished direct response")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		released <- struct{}{}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)
	localURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	transport := &http.Transport{Proxy: nil, MaxConnsPerHost: 1}
	t.Cleanup(transport.CloseIdleConnections)
	type observedResponse struct {
		body *dmmDirectProbeBody
		resp *http.Response
	}
	var observed []observedResponse
	var requested []string
	t.Cleanup(func() {
		for _, entry := range observed {
			if entry.body.closes.Load() == 0 {
				_ = entry.resp.Body.Close()
			}
		}
	})
	s.client.SetTransport(dmmSearchContainmentTransport(func(req *http.Request) (*http.Response, error) {
		for _, entry := range observed {
			require.EqualValues(t, 1, entry.body.closes.Load(), "previous probe must close before another connection is requested")
		}
		search := strings.HasPrefix(req.URL.Path, "/search/")
		if !search {
			require.Contains(t, dmmDirectProbeURLs(), req.URL.String())
			requested = append(requested, req.URL.String())
		}
		// Rewrite only this owned transport's request clone to an explicit
		// loopback address. No provider DNS, proxy, global transport or browser.
		local := req.Clone(req.Context())
		local.URL.Scheme, local.URL.Host = localURL.Scheme, localURL.Host
		local.Host = localURL.Host
		resp, err := transport.RoundTrip(local)
		if err != nil {
			return resp, err
		}
		if !search {
			body := &dmmDirectProbeBody{ReadCloser: resp.Body}
			resp.Body = body
			observed = append(observed, observedResponse{body: body, resp: resp})
		}
		return resp, nil
	}))

	got, err := s.GetURL(ctx, "SSIS-200")
	require.Empty(t, got)
	require.EqualError(t, err, "no scrapable URL found for movie on DMM")
	require.NoError(t, ctx.Err(), "the client deadline must not be what releases the connections")
	require.Equal(t, dmmDirectProbeURLs(), requested)
	require.Len(t, observed, 10)
	for _, entry := range observed {
		require.EqualValues(t, 1, entry.body.closes.Load())
		require.Zero(t, entry.body.reads.Load())
	}
	// Observe every server-side disconnect before any fixture cleanup. The
	// timeout only bounds a broken test; no latency or connection-reuse claim.
	for range observed {
		select {
		case <-released:
		case <-ctx.Done():
			t.Fatal("direct-probe connection was not released before cleanup")
		}
	}
	require.NoError(t, ctx.Err())
	assertCacheUnchanged()
}
