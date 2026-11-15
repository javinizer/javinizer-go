package libredmm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

// Both public entry points must keep the same polling and error contract.
func TestSharedPollingEntryPoints(t *testing.T) {
	for _, method := range []string{"Search", "ScrapeURL"} {
		for _, tc := range []struct {
			name     string
			statuses []int
			body     string
			wantErr  error
		}{
			{"pending then success", []int{202, 200}, `{}`, nil},
			{"exhausted", []int{202, 202, 202}, `{}`, fmt.Errorf("LibreDMM is still processing for abc00123")},
			{"exhausted message", []int{202, 202, 202}, `{"err":" queued "}`, fmt.Errorf("LibreDMM is still queued for abc00123")},
			{"payload error", []int{200}, `{"err":" broken "}`, fmt.Errorf("LibreDMM returned error: broken")},
			{"not found", []int{404}, `{}`, models.NewScraperNotFoundError("LibreDMM", "movie abc00123 not found on LibreDMM")},
			{"bad gateway", []int{502}, `{}`, models.NewScraperStatusError("LibreDMM", 502, "LibreDMM is temporarily unavailable (HTTP 502 Bad Gateway; host may be down)")},
			{"bad gateway message", []int{502}, `{"err":" down "}`, models.NewScraperStatusError("LibreDMM", 502, "LibreDMM is temporarily unavailable (HTTP 502 Bad Gateway; host may be down): down")},
			{"other status", []int{418}, `{}`, models.NewScraperStatusError("LibreDMM", 418, "LibreDMM returned status code 418")},
			{"other status message", []int{418}, `{"err":" teapot "}`, models.NewScraperStatusError("LibreDMM", 418, "LibreDMM returned status code 418: teapot")},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					requests := 0
					s := &scraper{enabled: true, baseURL: defaultBaseURL, maxPollAttempts: 3, pollInterval: time.Hour, rateLimiter: ratelimit.NewLimiter(0)}
					s.client = resty.New().SetTransport(pollingFixtureTransport(func(req *http.Request) (*http.Response, error) {
						require.Less(t, requests, len(tc.statuses), "unexpected extra request")
						status := tc.statuses[requests]
						requests++
						return &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
					}))
					call, input := pollingEntryPoint(s, method)
					start := time.Now()
					result, err := call(t.Context(), input)
					require.Equal(t, len(tc.statuses), requests)
					require.Equal(t, time.Duration(requests-1)*time.Hour, time.Since(start), "no wait after the final request")
					if tc.wantErr == nil {
						require.NoError(t, err)
						require.NotNil(t, result)
					} else {
						require.Nil(t, result)
						require.EqualError(t, err, tc.wantErr.Error())
						require.Equal(t, reflect.TypeOf(tc.wantErr), reflect.TypeOf(err))
					}
				})
			})
		}
		t.Run(method+"/cancellation", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				s := &scraper{enabled: true, baseURL: defaultBaseURL, maxPollAttempts: 3, pollInterval: time.Hour, rateLimiter: ratelimit.NewLimiter(0)}
				s.client = resty.New().SetTransport(pollingFixtureTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 202, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				}))
				call, input := pollingEntryPoint(s, method)
				done := make(chan error, 1)
				go func() { _, err := call(ctx, input); done <- err }()
				synctest.Wait()
				require.Equal(t, 1, calls)
				cancel()
				require.Equal(t, context.Canceled, <-done)
				// Fetch cancellation retains its query wrapper, unlike poll-wait cancellation.
				s.rateLimiter = ratelimit.NewLimiter(time.Hour)
				_, err := call(ctx, input)
				require.ErrorIs(t, err, context.Canceled)
				require.EqualError(t, err, "failed to query LibreDMM: context canceled")
				require.Equal(t, 1, calls)
			})
		})
	}
}

func pollingEntryPoint(s *scraper, method string) (func(context.Context, string) (*models.ScraperResult, error), string) {
	if method == "ScrapeURL" {
		return s.ScrapeURL, defaultBaseURL + "/movies/abc00123"
	}
	return s.Search, "abc00123"
}

func TestSharedPollingKeepsRedirectTarget(t *testing.T) {
	for _, method := range []string{"Search", "ScrapeURL"} {
		t.Run(method, func(t *testing.T) {
			var paths []string
			s := &scraper{enabled: true, baseURL: defaultBaseURL, maxPollAttempts: 2, rateLimiter: ratelimit.NewLimiter(0)}
			s.client = resty.New().SetTransport(pollingFixtureTransport(func(req *http.Request) (*http.Response, error) {
				paths = append(paths, req.URL.Path)
				status, body, header := 200, `{"title":"Resolved movie"}`, make(http.Header)
				if len(paths) == 1 {
					status = 302
					body = ""
					header.Set("Location", defaultBaseURL+"/movies/resolved.json")
				} else if len(paths) == 2 {
					status = 202
					body = `{}`
				}
				return &http.Response{StatusCode: status, Header: header, Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
			}))
			call, input := pollingEntryPoint(s, method)
			result, err := call(t.Context(), input)
			require.NoError(t, err)
			require.NotNil(t, result)
			first := "/search"
			if method == "ScrapeURL" {
				first = "/movies/abc00123.json"
			}
			require.Equal(t, []string{first, "/movies/resolved.json", "/movies/resolved.json"}, paths)
			require.Equal(t, "Resolved movie", result.Title)
		})
	}
}
