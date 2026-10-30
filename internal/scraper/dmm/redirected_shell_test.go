package dmm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

// A synthetic JS application shell, deliberately carrying no product metadata.
const redirectedShellHTML = `<!doctype html><html><head><title>FANZA</title><script type="module" src="/assets/app.js"></script></head><body><div id="root"></div><script>window.app={}</script></body></html>`
const redirectedProductHTML = `<html><body><script type="application/ld+json">{"@type":"Product","name":"Verified product"}</script><section data-e2eid="actress-information"><a href="/av/list/?actress=123"><img src="https://pics.dmm.co.jp/mono/actjpgs/fixture.jpg">Fixture Actress</a></section><video><source src="https://media.example/fixture.mp4"></video></body></html>`

func TestRedirectedDMMShell(t *testing.T) {
	for _, method := range []string{"Search", "ScrapeURL"} {
		for _, tc := range []struct {
			name                  string
			browser               bool
			httpBody, browserBody string
			direct                bool
			browserErr            error
			wantTitle             string
			wantCalls             int
		}{
			{name: "shell without browser", httpBody: redirectedShellHTML},
			{name: "direct probe shell", direct: true, httpBody: redirectedShellHTML},
			{name: "shell after rendering", browser: true, httpBody: redirectedShellHTML, browserBody: redirectedShellHTML, wantCalls: 1},
			{name: "rendered product", browser: true, httpBody: redirectedShellHTML, browserBody: redirectedProductHTML, wantTitle: "Verified product", wantCalls: 1},
			{name: "server rendered product", httpBody: redirectedProductHTML, wantTitle: "Verified product"},
			{name: "browser cancellation", browser: true, httpBody: redirectedShellHTML, browserErr: context.Canceled, wantCalls: 1},
			{name: "browser failure", browser: true, httpBody: redirectedShellHTML, browserErr: errors.New("render failed"), wantCalls: 1},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				original := fmt.Sprintf(digitalURL, "vrxs00084")
				finalURL := fmt.Sprintf(newDigitalURL, "vrxs00084")
				repo := newDMMTestRepo(t)
				require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{SearchID: "VRXS-084", ContentID: "vrxs00084", Source: "dmm"}))
				requests := []string{}
				s := &scraper{enabled: true, scrapeActress: true, useBrowser: tc.browser, contentIDRepo: repo, rateLimiter: ratelimit.NewLimiter(0)}
				s.client = resty.New().SetTransport(dmmFixtureTransport(func(req *http.Request) (*http.Response, error) {
					requests = append(requests, req.URL.String())
					status, body, header := http.StatusOK, "", make(http.Header)
					switch {
					case strings.Contains(req.URL.Path, "/search/"):
						body = `<a href="` + original + `">result</a>`
						if tc.direct {
							body = "<html><body>No candidates</body></html>"
						}
					case req.URL.String() == original:
						status = http.StatusMovedPermanently
						header.Set("Location", finalURL)
					case req.URL.String() == finalURL:
						body = tc.httpBody
					default:
						if tc.direct {
							status = http.StatusNotFound
						} else {
							t.Fatalf("unexpected request: %s", req.URL)
						}
					}
					return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				}))
				calls := 0
				s.browserFetch = func(ctx context.Context, got string) (string, error) {
					calls++
					require.Equal(t, finalURL, got)
					return tc.browserBody, tc.browserErr
				}
				var result *models.ScraperResult
				var err error
				if method == "Search" {
					result, err = s.Search(context.Background(), "VRXS-084")
				} else {
					result, err = s.ScrapeURL(context.Background(), original)
				}
				require.Contains(t, requests, finalURL, "must follow a real HTTP redirect")
				require.Equal(t, tc.wantCalls, calls)
				if tc.wantTitle == "" {
					require.Error(t, err)
					require.Nil(t, result)
					if tc.browserErr != nil {
						require.ErrorIs(t, err, tc.browserErr)
					}
					if tc.browserErr == nil {
						scraperErr, ok := models.AsScraperError(err)
						require.True(t, ok)
						require.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
					}
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.wantTitle, result.Title)
					require.Len(t, result.Actresses, 1)
					require.Equal(t, 123, result.Actresses[0].DMMID)
					require.Equal(t, "https://media.example/fixture.mp4", result.TrailerURL)
					require.Equal(t, original, result.SourceURL)
				}
			})
		}
	}
}

func TestDMMBrowserHostSelection(t *testing.T) {
	for _, rawURL := range []string{"https://evil.example/?next=video.dmm.co.jp", "https://video.dmm.co.jp.evil.example/", "https://www.dmm.co.jp/?next=video.dmm.co.jp", "file://video.dmm.co.jp/"} {
		require.False(t, isVideoDMMURL(rawURL))
	}
	require.True(t, isVideoDMMURL("https://VIDEO.DMM.CO.JP/av/content/?id=test"))
}

func TestDMMRedirectDomainAndPolicyGuards(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		reject       bool
	}{
		{"foreign host", "https://video.dmm.co.jp.evil.example/av/content/?id=test", false},
		{"rejected redirect", "https://video.dmm.co.jp/av/content/?id=test", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := fmt.Sprintf(digitalURL, "test00001")
			s := &scraper{useBrowser: true, rateLimiter: ratelimit.NewLimiter(0)}
			s.client = resty.New().SetTransport(dmmFixtureTransport(func(req *http.Request) (*http.Response, error) {
				header := make(http.Header)
				status := http.StatusOK
				body := redirectedProductHTML
				if req.URL.String() == original {
					status = http.StatusFound
					header.Set("Location", tc.target)
				} else {
					require.False(t, tc.reject)
					require.Equal(t, tc.target, req.URL.String())
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			}))
			if tc.reject {
				s.client.GetClient().CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirect blocked") }
			}
			s.browserFetch = func(context.Context, string) (string, error) {
				t.Fatal("must not launch browser for rejected/foreign redirect")
				return "", nil
			}
			result, err := s.ScrapeURL(context.Background(), original)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}

func TestDMMLegacyProductAndTitlelessPage(t *testing.T) {
	for _, body := range []string{`<html><h1 id="title" class="item">Legacy product</h1></html>`, redirectedShellHTML} {
		s := &scraper{rateLimiter: ratelimit.NewLimiter(0)}
		s.client = resty.New().SetTransport(dmmFixtureTransport(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}))
		result, err := s.ScrapeURL(context.Background(), fmt.Sprintf(physicalURL, "test00001"))
		if body == redirectedShellHTML {
			require.Error(t, err)
			require.Nil(t, result)
		} else {
			require.NoError(t, err)
			require.Equal(t, "Legacy product", result.Title)
		}
	}
}

func TestDMMRedirectKeepsOriginalRemasterIdentity(t *testing.T) {
	for _, method := range []string{"Search", "ScrapeURL"} {
		for _, browser := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/browser=%t", method, browser), func(t *testing.T) {
				original := fmt.Sprintf(digitalURL, "1rct00156h")
				target := fmt.Sprintf(newDigitalURL, "1rct00157h")
				product := `<h1>Different release</h1><table><tr><td>品番：</td><td>RCT-157-HD</td></tr></table>`
				s := &scraper{useBrowser: browser, contentIDRepo: newDMMTestRepo(t), rateLimiter: ratelimit.NewLimiter(0)}
				s.client = resty.New().SetTransport(dmmFixtureTransport(func(req *http.Request) (*http.Response, error) {
					status, body, header := http.StatusOK, "", make(http.Header)
					switch {
					case strings.Contains(req.URL.Path, "/search/"):
						body = `<a href="` + original + `">candidate</a>`
					case req.URL.String() == original:
						status = http.StatusMovedPermanently
						header.Set("Location", target)
					case req.URL.String() == target:
						body = product
					default:
						t.Fatalf("unexpected request: %s", req.URL)
					}
					return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				}))
				calls := 0
				s.browserFetch = func(_ context.Context, url string) (string, error) {
					calls++
					require.Equal(t, target, url)
					return product, nil
				}
				var result *models.ScraperResult
				var err error
				if method == "Search" {
					result, err = s.Search(context.Background(), "1rct00156h")
				} else {
					result, err = s.ScrapeURL(context.Background(), original)
				}
				require.Error(t, err)
				require.Nil(t, result)
				require.Contains(t, err.Error(), "different release")
				scraperErr, ok := models.AsScraperError(err)
				require.True(t, ok)
				require.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
				if browser {
					require.Equal(t, 1, calls)
				} else {
					require.Zero(t, calls)
				}
			})
		}
	}
}
