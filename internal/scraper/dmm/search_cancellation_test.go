package dmm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dmmCancellationTransport func(*http.Request) (*http.Response, error)

func (f dmmCancellationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func dmmCancellationResponse(req *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}
}

func TestTryDirectURLs_AlreadyCancelledBeforeImmediateSlot(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Hour} {
		for _, expired := range []bool{false, true} {
			name := delay.String() + "/cancelled"
			if expired {
				name = delay.String() + "/expired"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				wantErr := context.Canceled
				if expired {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
					wantErr = context.DeadlineExceeded
				}
				defer cancel()
				cancel()
				require.ErrorIs(t, ctx.Err(), wantErr)

				requests := 0
				s := &scraper{
					rateLimiter: ratelimit.NewLimiter(delay), // First slot is immediately available.
					client: resty.New().SetRetryCount(0).SetTransport(dmmCancellationTransport(func(req *http.Request) (*http.Response, error) {
						requests++
						return dmmCancellationResponse(req, http.StatusNotFound), nil
					})),
				}

				assert.Nil(t, s.tryDirectURLs(ctx, "h_123ipx00535"))
				assert.Zero(t, requests, "an already-cancelled context must not start even the first probe")
			})
		}
	}
}

func TestTryDirectURLs_CancellationInterruptsHTTPAndPreservesCandidates(t *testing.T) {
	for _, firstStatus := range []int{0, http.StatusOK, http.StatusFound} {
		name := "no_completed_candidate"
		if firstStatus != 0 {
			name = http.StatusText(firstStatus)
		}
		t.Run(name, func(t *testing.T) {
			type contextKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "direct-probe"))
			started := make(chan context.Context, 1)
			release := make(chan struct{})
			finished := make(chan struct{})
			result := make(chan []urlCandidate, 1)
			var requests atomic.Int32
			wantRequests := int32(1)
			if firstStatus != 0 {
				wantRequests++
			}

			s := &scraper{
				rateLimiter: ratelimit.NewLimiter(0),
				client: resty.New().SetRetryCount(0).SetTimeout(time.Minute).SetTransport(dmmCancellationTransport(func(req *http.Request) (*http.Response, error) {
					requestNumber := requests.Add(1)
					if firstStatus != 0 && requestNumber == 1 {
						return dmmCancellationResponse(req, firstStatus), nil
					}
					if requestNumber == wantRequests {
						started <- req.Context()
					}
					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-release:
						return nil, errors.New("test transport released")
					}
				})),
			}
			go func() {
				defer close(finished)
				result <- s.tryDirectURLs(ctx, "h_123ipx00535")
			}()
			t.Cleanup(func() {
				cancel()
				close(release)
				<-finished
			})

			var requestContext context.Context
			select {
			case requestContext = <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("direct probe did not reach the in-memory transport")
			}
			assert.Equal(t, "direct-probe", requestContext.Value(contextKey{}), "the request must inherit the caller's context")
			cancel()

			select {
			case candidates := <-result:
				require.ErrorIs(t, requestContext.Err(), context.Canceled)
				assert.Equal(t, wantRequests, requests.Load(), "cancellation must prevent every later probe")
				if firstStatus == 0 {
					assert.Nil(t, candidates)
				} else {
					assert.Equal(t, []urlCandidate{{
						url:       "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ipx00535/",
						priority:  350,
						contentID: "ipx00535",
						idLength:  8,
					}}, candidates, "a previously successful candidate must survive cancellation")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancellation did not interrupt the request before the one-minute client timeout")
			}
		})
	}
}

func TestTryDirectURLs_ActiveContextContinuesAfterOrdinaryFailure(t *testing.T) {
	var requested []string
	s := &scraper{
		rateLimiter: ratelimit.NewLimiter(0),
		client: resty.New().SetRetryCount(0).SetTransport(dmmCancellationTransport(func(req *http.Request) (*http.Response, error) {
			requested = append(requested, req.URL.String())
			if len(requested) == 1 {
				return nil, errors.New("synthetic transient failure")
			}
			status := http.StatusNotFound
			if len(requested) == 3 {
				status = http.StatusOK
			}
			return dmmCancellationResponse(req, status), nil
		})),
	}

	candidates := s.tryDirectURLs(context.Background(), "h_123ipx00535")
	assert.Equal(t, []string{
		"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ipx00535/",
		"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
		"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=h_123ipx00535/",
		"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=h_123ipx00535/",
		"https://video.dmm.co.jp/av/content/?id=ipx00535",
		"https://video.dmm.co.jp/amateur/content/?id=ipx00535",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=1ipx00535r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=2ipx00535r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=4ipx00535r/",
		"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=5ipx00535r/",
	}, requested)
	assert.Equal(t, []urlCandidate{{
		url:       "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=h_123ipx00535/",
		priority:  350,
		contentID: "h_123ipx00535",
		idLength:  13,
	}}, candidates)
}
