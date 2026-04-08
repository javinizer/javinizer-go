package javdb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"golang.org/x/net/html"
)

type errorRoundTripper struct {
	err error
}

func (rt *errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, rt.err
}

type staticRoundTripper struct {
	responses map[string]string
}

func (s *staticRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if body, ok := s.responses[req.URL.String()]; ok {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("not found")),
		Request:    req,
	}, nil
}

func docFromHTML(t *testing.T, raw string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("goquery.NewDocumentFromReader() error = %v", err)
	}
	return doc
}

func TestResolveDownloadProxyForHost(t *testing.T) {
	downloadProxy := &config.ProxyConfig{Enabled: true, Profile: "download", Profiles: map[string]config.ProxyProfile{"download": {URL: "http://download.example:8080"}}}
	overrideProxy := &config.ProxyConfig{Enabled: true, Profile: "override", Profiles: map[string]config.ProxyProfile{"override": {URL: "http://override.example:8080"}}}
	scraper := &Scraper{
		downloadProxy: downloadProxy,
		proxyOverride: overrideProxy,
	}

	dp, op, ok := scraper.ResolveDownloadProxyForHost("img.jdbstatic.com")
	if !ok || dp != downloadProxy || op != overrideProxy {
		t.Fatalf("ResolveDownloadProxyForHost(jdbstatic) = (%v, %v, %v)", dp, op, ok)
	}

	dp, op, ok = scraper.ResolveDownloadProxyForHost("javdb.com")
	if !ok || dp != downloadProxy || op != overrideProxy {
		t.Fatalf("ResolveDownloadProxyForHost(javdb) = (%v, %v, %v)", dp, op, ok)
	}

	dp, op, ok = scraper.ResolveDownloadProxyForHost("example.com")
	if ok || dp != nil || op != nil {
		t.Fatalf("ResolveDownloadProxyForHost(example) = (%v, %v, %v)", dp, op, ok)
	}
}

func TestGetURL_EmptyID(t *testing.T) {
	scraper := &Scraper{baseURL: "https://javdb.test"}
	if _, err := scraper.GetURL("   "); err == nil {
		t.Fatal("expected GetURL to reject empty IDs")
	}
}

func TestFindDetailURL_Fallbacks(t *testing.T) {
	t.Run("falls back to single detail link", func(t *testing.T) {
		client := resty.New()
		client.SetTransport(&staticRoundTripper{
			responses: map[string]string{
				"https://javdb.test/search?q=XYZ-999&f=all": `<html><body><div class="movie-list"><div class="item"><a href="/v/fallback"><div class="video-title">Other Title</div></a></div></div></body></html>`,
			},
		})

		scraper := &Scraper{
			client:      client,
			enabled:     true,
			baseURL:     "https://javdb.test",
			rateLimiter: ratelimit.NewLimiter(0),
			settings:    config.ScraperSettings{Enabled: true},
		}

		got, err := scraper.findDetailURL("XYZ-999")
		if err != nil {
			t.Fatalf("findDetailURL() error = %v", err)
		}
		if got != "https://javdb.test/v/fallback" {
			t.Fatalf("findDetailURL() = %q", got)
		}
	})

	t.Run("returns not found when nothing matches", func(t *testing.T) {
		client := resty.New()
		client.SetTransport(&staticRoundTripper{
			responses: map[string]string{
				"https://javdb.test/search?q=XYZ-999&f=all": `<html><body><div class="movie-list"></div></body></html>`,
			},
		})

		scraper := &Scraper{
			client:      client,
			enabled:     true,
			baseURL:     "https://javdb.test",
			rateLimiter: ratelimit.NewLimiter(0),
			settings:    config.ScraperSettings{Enabled: true},
		}

		_, err := scraper.findDetailURL("XYZ-999")
		if err == nil {
			t.Fatal("expected findDetailURL to fail")
		}
		scraperErr, ok := models.AsScraperError(err)
		if !ok || scraperErr.Kind != models.ScraperErrorKindNotFound {
			t.Fatalf("expected scraper not found error, got %T: %v", err, err)
		}
	})
}

func TestFetchPageDirectResponse(t *testing.T) {
	scraper := &Scraper{}

	t.Run("propagates request error", func(t *testing.T) {
		wantErr := errors.New("network down")
		_, err := scraper.fetchPageDirectResponse(nil, wantErr)
		if !errors.Is(err, wantErr) {
			t.Fatalf("fetchPageDirectResponse() error = %v", err)
		}
	})

	t.Run("returns status error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		}))
		defer server.Close()

		client := resty.New()
		resp, err := client.R().Get(server.URL)
		if err != nil {
			t.Fatalf("client.Get() error = %v", err)
		}
		_, err = scraper.fetchPageDirectResponse(resp, nil)
		scraperErr, ok := models.AsScraperError(err)
		if err == nil || !ok || scraperErr.StatusCode != http.StatusForbidden {
			t.Fatalf("expected scraper status error, got %v", err)
		}
	})

	t.Run("returns challenge error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`<html><title>Just a moment...</title><body>Checking your browser before accessing javdb.com</body></html>`))
		}))
		defer server.Close()

		client := resty.New()
		resp, err := client.R().Get(server.URL)
		if err != nil {
			t.Fatalf("client.Get() error = %v", err)
		}
		_, err = scraper.fetchPageDirectResponse(resp, nil)
		scraperErr, ok := models.AsScraperError(err)
		if err == nil || !ok || scraperErr.Kind != models.ScraperErrorKindBlocked {
			t.Fatalf("expected challenge error, got %v", err)
		}
	})

	t.Run("returns html on success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
		}))
		defer server.Close()

		client := resty.New()
		resp, err := client.R().Get(server.URL)
		if err != nil {
			t.Fatalf("client.Get() error = %v", err)
		}
		html, err := scraper.fetchPageDirectResponse(resp, nil)
		if err != nil {
			t.Fatalf("fetchPageDirectResponse() error = %v", err)
		}
		if !strings.Contains(html, "ok") {
			t.Fatalf("fetchPageDirectResponse() = %q", html)
		}
	})
}

func TestFetchPage(t *testing.T) {
	t.Run("direct success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
		}))
		defer server.Close()

		client := resty.New()
		scraper := &Scraper{
			client:      client,
			enabled:     true,
			baseURL:     server.URL,
			rateLimiter: ratelimit.NewLimiter(0),
			settings:    config.ScraperSettings{Enabled: true},
		}

		html, err := scraper.fetchPage(server.URL)
		if err != nil {
			t.Fatalf("fetchPage() error = %v", err)
		}
		if !strings.Contains(html, "ok") {
			t.Fatalf("fetchPage() = %q", html)
		}
	})

	t.Run("request error falls back to direct response handling", func(t *testing.T) {
		client := resty.New()
		client.SetTransport(&errorRoundTripper{err: errors.New("boom")})

		scraper := &Scraper{
			client:      client,
			enabled:     true,
			baseURL:     "https://javdb.test",
			rateLimiter: ratelimit.NewLimiter(0),
			settings:    config.ScraperSettings{Enabled: true},
		}

		_, err := scraper.fetchPage("https://javdb.test/page")
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("expected propagated request error, got %v", err)
		}
	})
}

func TestWaitForRateLimitAndHelpers(t *testing.T) {
	scraper := &Scraper{rateLimiter: ratelimit.NewLimiter(20 * time.Millisecond)}

	start := time.Now()
	if err := scraper.rateLimiter.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	if err := scraper.rateLimiter.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if time.Since(start) < 15*time.Millisecond {
		t.Fatal("rate limiter did not delay second call")
	}

	scraper.rateLimiter = ratelimit.NewLimiter(0)
	start = time.Now()
	if err := scraper.rateLimiter.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if time.Since(start) > 10*time.Millisecond {
		t.Fatal("zero-delay limiter delayed unexpectedly")
	}
}

func TestNodeAndListHelpers(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<strong class="symbol">prefix<span>♀</span></strong>`))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}
	var strong *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil || strong != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "strong" {
			strong = n
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)

	if nodeAttr(strong, "class") != "symbol" {
		t.Fatalf("nodeAttr(class) = %q", nodeAttr(strong, "class"))
	}
	if nodeText(strong) != "prefix♀" {
		t.Fatalf("nodeText() = %q", nodeText(strong))
	}
	if nodeText(nil) != "" {
		t.Fatalf("nodeText(nil) = %q", nodeText(nil))
	}

	sel := docFromHTML(t, `<div>N/A</div>`).Find("div").First()
	if got := extractStringList(sel); got != nil {
		t.Fatalf("extractStringList(N/A) = %#v", got)
	}

	sel = docFromHTML(t, `<div>Drama / Schoolgirl、Romance</div>`).Find("div").First()
	got := extractStringList(sel)
	if len(got) != 3 || got[0] != "Drama" || got[1] != "Schoolgirl" || got[2] != "Romance" {
		t.Fatalf("extractStringList(text) = %#v", got)
	}
}
