package libredmm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestNonpositivePollBudgetStillMakesOneBoundedAttempt(t *testing.T) {
	for _, budget := range []int{0, -1} {
		for _, method := range []string{"Search", "ScrapeURL"} {
			t.Run(fmt.Sprintf("%s/%d", method, budget), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					// Exercise the defensive budget normalization directly. The
					// normal constructor supplies a positive default budget.
					var requests atomic.Int32
					var requestURL string
					s := &scraper{
						enabled: true, baseURL: defaultBaseURL,
						maxPollAttempts: budget, pollInterval: time.Hour,
						rateLimiter: ratelimit.NewLimiter(0),
						client: resty.New().SetTransport(pollingFixtureTransport(func(req *http.Request) (*http.Response, error) {
							requests.Add(1)
							requestURL = req.URL.String()
							return &http.Response{
								StatusCode: http.StatusAccepted,
								Header:     http.Header{"Content-Type": {"application/json"}},
								Request:    req,
								Body:       io.NopCloser(strings.NewReader(`{"err":"queued"}`)),
							}, nil
						})),
					}
					var call func(context.Context, string) (*models.ScraperResult, error)
					input := "abc00123"
					wantURL := defaultBaseURL + "/search?q=abc00123&format=json"
					call = s.Search
					if method == "ScrapeURL" {
						input = defaultBaseURL + "/movies/abc00123"
						wantURL = input + ".json"
						call = s.ScrapeURL
					}
					started := time.Now()
					result, err := call(t.Context(), input)
					require.Nil(t, result)
					require.EqualError(t, err, "LibreDMM is still queued for abc00123")
					require.Equal(t, int32(1), requests.Load())
					require.Equal(t, wantURL, requestURL)
					require.Equal(t, started, time.Now(), "exhausted budget must not enter the hour-long poll wait")
					require.Equal(t, budget, s.maxPollAttempts, "normalization must not mutate scraper configuration")
					require.NoError(t, t.Context().Err())
				})
			})
		}
	}
}
