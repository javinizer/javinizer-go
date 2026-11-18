package javdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/challengedetect"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func javdbHealthResponse(status int, body string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err == nil {
		doc.Find("script, style").Remove()
		text := strings.ToLower(strings.Join(strings.Fields(doc.Text()), " "))
		if strings.Contains(text, "access to this site is prohibited in the country where your internet is located") || strings.Contains(text, "本站禁止了你的網路所在國家的訪問") {
			return "country_blocked"
		}
	}
	if challengedetect.IsCloudflareChallengePage(body) {
		return "challenge"
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "access_denied"
	}
	if status == http.StatusTooManyRequests {
		return "rate_limited"
	}
	if status < 200 || status >= 300 {
		return "http_failure"
	}
	return ""
}

func runJavDBHealth(ctx context.Context, s *scraper, id string) string {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	outcome := ""
	s.client.SetRetryCount(0)
	s.client.OnError(func(_ *resty.Request, _ error) {
		if outcome == "" {
			outcome = "transport_failure"
		}
	})
	s.client.OnAfterResponse(func(_ *resty.Client, resp *resty.Response) error {
		if category := javdbHealthResponse(resp.StatusCode(), resp.String()); category != "" {
			outcome = category
			cancel()
			return errors.New(category)
		}
		return nil
	})
	result, err := s.Search(ctx, id)
	if outcome != "" {
		return outcome
	}
	if err != nil {
		var networkError net.Error
		if errors.As(err, &networkError) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "transport_failure"
		}
		return "scrape_failure"
	}
	if result == nil || normalizeIDForCompare(result.ID) != normalizeIDForCompare(id) || strings.TrimSpace(result.Title) == "" || result.Title == result.ID || (result.ReleaseDate == nil && result.Runtime == 0 && len(result.Actresses) == 0) {
		return "incomplete_metadata"
	}
	return "healthy"
}

func TestJavDBLiveHealth(t *testing.T) {
	if testing.Short() || os.Getenv("JAVINIZER_RUN_JAVDB_HEALTH") != "1" {
		t.Skip("live network disabled; run JAVINIZER_RUN_JAVDB_HEALTH=1 go test ./internal/scraper/javdb -run '^TestJavDBLiveHealth$' -count=1")
	}
	s := newScraper(&models.ScraperSettings{Enabled: true, Timeout: 20}, &models.ProxyConfig{}, models.FlareSolverrConfig{})
	defer s.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	s.client.SetTransport(transport)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if outcome := runJavDBHealth(ctx, s, "EYAN-172"); outcome != "healthy" {
		t.Fatalf("JavDB live health: %s (not a passing scrape; no solver or proxy is used)", outcome)
	}
}

type javdbHealthTransport func(*http.Request) (*http.Response, error)

func (f javdbHealthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJavDBHealthResponseClassification(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"country", `<p>Due to copyright restrictions, access to this site is prohibited in the country where your internet is located.</p>`, "country_blocked", 200},
		{"formatted country", `<p>access to this site is <strong>prohibited</strong> in the country where your internet is located</p>`, "country_blocked", 200},
		{"Chinese", `由於版權限制，本站禁止了你的網路所在國家的訪問。`, "country_blocked", 200},
		{"country before challenge", `本站禁止了你的網路所在國家的訪問 cf_chl_`, "country_blocked", 403},
		{"challenge", `<p>Enable JavaScript and cookies to continue</p>`, "challenge", 200},
		{"forbidden", "denied", "access_denied", 403},
		{"unauthorized", "login", "access_denied", 401},
		{"throttled", "slow down", "rate_limited", 429},
		{"server failure", "unavailable", "http_failure", 503},
		{"not found", "not found", "http_failure", 404},
		{"ordinary", `<p>A normal page</p>`, "", 200},
		{"script is not a country notice", `<script>"本站禁止了你的網路所在國家的訪問"</script><p>Normal page</p>`, "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, javdbHealthResponse(tc.status, tc.body)) })
	}
}

func TestJavDBHealthNoFalseGreen(t *testing.T) {
	search := `<div class="movie-list"><div class="item"><a href="/v/abc123"><div class="video-title"><strong>EYAN-172</strong> Fixture</div><div class="uid">EYAN-172</div></a></div></div>`
	detail := `<h2 class="title is-4"><strong>EYAN-172</strong> Fixture Title</h2><div class="movie-panel-info"><div class="panel-block"><strong>番號:</strong><span class="value">EYAN-172</span></div><div class="panel-block"><strong>時長:</strong><span class="value">120分鐘</span></div></div>`
	for _, tc := range []struct {
		name, search, detail, want string
		status                     int
		transportError             bool
		truncated                  bool
	}{
		{name: "metadata success", search: search, detail: detail, want: "healthy", status: 200},
		{name: "different release", search: search, detail: strings.ReplaceAll(detail, "EYAN-172", "EYAN-173"), want: "incomplete_metadata", status: 200},
		{name: "different variant", search: search, detail: strings.ReplaceAll(detail, "EYAN-172", "EYAN-172Z"), want: "incomplete_metadata", status: 200},
		{name: "truncated response", want: "transport_failure", status: 200, truncated: true},
		{name: "empty search", search: `<html></html>`, want: "scrape_failure", status: 200},
		{name: "country search", search: `本站禁止了你的網路所在國家的訪問`, want: "country_blocked", status: 200},
		{name: "country detail", search: search, detail: `本站禁止了你的網路所在國家的訪問`, want: "country_blocked", status: 200},
		{name: "challenge detail", search: search, detail: `cf_chl_`, want: "challenge", status: 200},
		{name: "HTTP failure", search: `unavailable`, want: "http_failure", status: 503},
		{name: "title only is insufficient", search: search, detail: `<h2 class="title is-4"><strong>EYAN-172</strong> Fixture Title</h2>`, want: "incomplete_metadata", status: 200},
		{name: "empty detail", search: search, detail: `<html></html>`, want: "scrape_failure", status: 200},
		{name: "transport failure", want: "transport_failure", transportError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true, Timeout: 1}, &models.ProxyConfig{}, models.FlareSolverrConfig{})
			defer s.Close()
			calls := 0
			s.client.SetTransport(javdbHealthTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.transportError {
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("fixture unavailable")}
				}
				body := tc.search
				if strings.HasPrefix(r.URL.Path, "/v/") {
					body = tc.detail
				}
				var reader io.Reader = strings.NewReader(body)
				if tc.truncated {
					reader = javdbHealthBrokenBody{}
				}
				return &http.Response{StatusCode: tc.status, Status: fmt.Sprint(tc.status), Header: make(http.Header), Body: io.NopCloser(reader), Request: r}, nil
			}))
			require.Equal(t, tc.want, runJavDBHealth(context.Background(), s, "EYAN-172"))
			if tc.want == "country_blocked" || tc.want == "challenge" || tc.want == "http_failure" || tc.want == "transport_failure" {
				expected := 1
				if tc.search == search {
					expected = 2
				}
				require.Equal(t, expected, calls, "diagnostics must not retry a blocked response")
			}
		})
	}
}

type javdbHealthBrokenBody struct{}

func (javdbHealthBrokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
