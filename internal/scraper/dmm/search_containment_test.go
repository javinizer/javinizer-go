package dmm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type dmmSearchContainmentTransport func(*http.Request) (*http.Response, error)

func (f dmmSearchContainmentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type dmmSearchContainmentBody struct {
	*strings.Reader
	onClose func()
}

func (b *dmmSearchContainmentBody) Close() error {
	b.onClose()
	return nil
}

func newDMMSearchContainmentScraper(t *testing.T, repo models.ContentIDMappingRepositoryInterface) *scraper {
	t.Helper()
	var settings models.ScraperSettings
	// Decode ordinary settings so an explicit zero retains its presence and
	// disables retries without changing the constructed client or its limiter.
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true,"rate_limit":1,"timeout":5,"retry_count":0,"use_browser":false,"scrape_actress":false}`), &settings))
	s := newScraper(&settings, nil, models.FlareSolverrConfig{}, dmmOptions{ContentIDRepo: repo})
	require.Equal(t, 1, s.Config().RateLimit)
	require.Zero(t, s.client.RetryCount)
	require.False(t, s.useBrowser)
	require.False(t, s.scrapeActress)
	return s
}

func newDMMCachedSearchContainmentScraper(t *testing.T) (*scraper, func()) {
	t.Helper()
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{
		SearchID: "SSIS-200", ContentID: "ssis00200", Source: "dmm",
	}))
	before, err := repo.FindBySearchID(context.Background(), "SSIS-200")
	require.NoError(t, err)
	return newDMMSearchContainmentScraper(t, repo), func() {
		t.Helper()
		// The operation's context may be canceled; use a fresh live context to
		// check the complete persisted mapping, including its identity/timestamp.
		after, err := repo.FindBySearchID(context.Background(), "SSIS-200")
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
}

func TestGetURLRejectsOverDepthSearchDocuments(t *testing.T) {
	physical := "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ssis00200/"
	digital := digitalURLFor("ssis00200")
	anchor := `<a href="` + physical + `">SSIS-200 physical edition</a>`
	malformed := anchor + strings.Repeat("<dl>", 511)
	// A real x/net parser failure, not a mocked reader error. The tempting
	// higher-priority anchor must be discarded with the rejected document.
	_, err := goquery.NewDocumentFromReader(strings.NewReader(anchor + strings.Repeat("<dl>", 510)))
	require.NoError(t, err)
	_, err = goquery.NewDocumentFromReader(strings.NewReader(malformed))
	require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")

	queries := []string{searchURLFor("ssis200"), searchURLFor("ssis-200"), searchURLFor("ssis00200")}
	directProbes := []string{
		physical, digital, physical, digital,
		"https://video.dmm.co.jp/av/content/?id=ssis00200",
		"https://video.dmm.co.jp/amateur/content/?id=ssis00200",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=1ssis00200r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=2ssis00200r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=4ssis00200r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=5ssis00200r/",
	}
	for _, tc := range []struct {
		name        string
		validSearch bool
		directWorks bool
		wantURL     string
	}{
		{name: "later valid search", validSearch: true, wantURL: digital},
		{name: "all search and direct attempts fail"},
		{name: "direct fallback recovers", directWorks: true, wantURL: digital},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, assertCacheUnchanged := newDMMCachedSearchContainmentScraper(t)
			var requested []string
			s.client.SetTransport(dmmSearchContainmentTransport(func(req *http.Request) (*http.Response, error) {
				u := req.URL.String()
				requested = append(requested, u)
				status, body := http.StatusOK, malformed
				switch u {
				case queries[0]:
				case queries[1]:
					if tc.validSearch {
						// The accepted link is beyond the 500-byte debug snippet:
						// snippet truncation must not truncate parser input.
						body = "<html><body>" + strings.Repeat("x", 600) + `<a href="` + digital + `">SSIS-200 digital edition</a></body></html>`
					}
				case queries[2]:
					if tc.validSearch {
						body = "<html><body>No additional results</body></html>"
					}
				default:
					require.Contains(t, directProbes, u, "unexpected offline request")
					status, body = http.StatusNotFound, "not found"
					if tc.directWorks && u == digital {
						status, body = http.StatusOK, "available"
					}
				}
				response := &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(body))}
				// Direct probes leave their raw responses unread. Close the
				// current Body at fixture cleanup, after all outcome assertions,
				// to release net/http's client-timeout wrapper too. This cleanup
				// does not assert that production closes direct-probe bodies.
				t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
				return response, nil
			}))

			got, err := s.GetURL(context.Background(), "SSIS-200")
			if tc.wantURL == "" {
				require.EqualError(t, err, "no scrapable URL found for movie on DMM")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantURL, got)
			wantRequests := append([]string(nil), queries...)
			if !tc.validSearch {
				wantRequests = append(wantRequests, directProbes...)
			}
			require.Equal(t, wantRequests, requested, "rejected anchors must not bypass fallback or displace a later valid search result")
			assertCacheUnchanged()
		})
	}
}

func TestSearchCancellationAfterSearchStopsDetailAdmission(t *testing.T) {
	queries := []string{searchURLFor("ssis200"), searchURLFor("ssis-200"), searchURLFor("ssis00200")}
	detail := digitalURLFor("ssis00200")
	for _, cancelAfterSearch := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelAfterSearch), func(t *testing.T) {
			s, assertCacheUnchanged := newDMMCachedSearchContainmentScraper(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requested, closed []string
			s.client.SetTransport(dmmSearchContainmentTransport(func(req *http.Request) (*http.Response, error) {
				u := req.URL.String()
				requested = append(requested, u)
				body := `<a href="` + detail + `">SSIS-200</a>`
				if u == detail {
					// No images/actresses: the noncanceling control has no other I/O.
					body = `<html><body><h1 id="title" class="item">SSIS-200 search control</h1></body></html>`
				} else {
					require.Contains(t, queries, u, "unexpected offline request")
				}
				reader := strings.NewReader(body)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req,
					Body: &dmmSearchContainmentBody{Reader: reader, onClose: func() {
						require.Zero(t, reader.Len(), "response must be fully read before cancellation")
						closed = append(closed, u)
						// Resty closes the final completed search response before
						// returning to Search. Ordinary caller cancellation here
						// deterministically precedes the detail-admission boundary.
						if cancelAfterSearch && u == queries[2] {
							cancel()
						}
					}},
				}, nil
			}))

			result, err := s.Search(ctx, "SSIS-200")
			wantRequests := append([]string(nil), queries...)
			if cancelAfterSearch {
				require.Nil(t, result)
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorContains(t, err, "DMM: rate limit wait failed")
				require.ErrorIs(t, ctx.Err(), context.Canceled)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "SSIS-200", result.ID)
				require.Equal(t, "ssis00200", result.ContentID)
				require.Equal(t, "SSIS-200 search control", result.Title)
				require.Equal(t, detail, result.SourceURL)
				wantRequests = append(wantRequests, detail)
			}
			require.Equal(t, wantRequests, requested)
			require.Equal(t, requested, closed, "every completed response must be closed exactly once")
			assertCacheUnchanged()
		})
	}
}

func TestScrapeURLCanceledCallerNeverReachesHTTP(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{name: "canceled", ctx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}, want: context.Canceled},
		{name: "deadline exceeded", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Unix(1, 0))
		}, want: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newDMMSearchContainmentScraper(t, nil)
			calls := 0
			s.client.SetTransport(dmmSearchContainmentTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				return nil, fmt.Errorf("unexpected request after cancellation: %s", req.URL)
			}))
			ctx, cancel := tc.ctx()
			defer cancel()
			require.ErrorIs(t, ctx.Err(), tc.want)

			result, err := s.ScrapeURL(ctx, digitalURLFor("ssis00200"))
			require.Nil(t, result)
			var scraperErr *models.ScraperError
			require.ErrorAs(t, err, &scraperErr)
			require.Equal(t, "DMM", scraperErr.Scraper)
			require.Zero(t, scraperErr.StatusCode)
			require.Equal(t, "rate limit wait failed: "+tc.want.Error(), scraperErr.Message)
			require.Zero(t, calls, "canceled calls must stop before HTTP admission")
		})
	}
}
