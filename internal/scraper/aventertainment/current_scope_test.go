package aventertainment

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

type currentScopeAVETransport func(*http.Request) (*http.Response, error)

func (f currentScopeAVETransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCurrentScopeAVEFetchOutcomes(t *testing.T) {
	for _, status := range []int{http.StatusUnavailableForLegalReasons, http.StatusBadGateway, http.StatusOK} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := &scraper{enabled: true, language: "en", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(currentScopeAVETransport(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "www.aventertainments.com", req.URL.Host)
				return &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`<title>Just a moment...</title><div id="cf-challenge-running"></div>`))}, nil
			}))}
			result, err := s.ScrapeURL(context.Background(), "https://www.aventertainments.com/ppv/detail/12345/")
			require.Nil(t, result)
			require.Error(t, err)
			classified, ok := models.AsScraperError(err)
			require.True(t, ok)
			if status == http.StatusOK {
				require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
			} else {
				require.Equal(t, status, classified.StatusCode)
			}
		})
	}
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour), client: resty.New().SetTransport(currentScopeAVETransport(func(*http.Request) (*http.Response, error) {
		t.Error("cancelled limiter must not dispatch HTTP")
		return nil, context.Canceled
	}))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body, status, err := s.fetchPageCtx(ctx, "https://www.aventertainments.com/")
	require.Empty(t, body)
	require.Zero(t, status)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCurrentScopeAVEParseAndLanguage(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<title>Example - AV Entertainment</title><div class="product-description">Meaningful description<script>bad()</script><style>bad</style><a data-target="#more">Read more</a></div>`))
	require.NoError(t, err)
	s := &scraper{language: "ja"}
	result, err := s.ParseHTML(doc, "https://www.aventertainments.com/ppv/detail/ABP-123/")
	require.NoError(t, err)
	require.Equal(t, "Meaningful description", result.Description)
	require.Equal(t, "ja", result.Language)
	require.Equal(t, "http://%", s.applyLanguage("http://%"))
	require.Contains(t, s.applyLanguage("https://www.aventertainments.com/ppv/detail/12345/?languageID=1"), "languageID=2")
	s.language = "en"
	require.Contains(t, s.applyLanguage("https://www.aventertainments.com/ppv/detail/12345/?languageID=2"), "languageID=1")
	doc, err = goquery.NewDocumentFromReader(strings.NewReader(`<meta name="description" content=""><title>No description</title>`))
	require.NoError(t, err)
	require.Empty(t, extractDescription(doc))
	links := extractDetailLinks(`<a href=" ">empty</a><a href="/ppv/detail/12345/">one</a><a href="/ppv/detail/12345/">duplicate</a>`, "https://www.aventertainments.com")
	require.Equal(t, []string{"https://www.aventertainments.com/ppv/detail/12345/"}, links)
}

func TestCurrentScopeAVERawMediaFallbacks(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html></html>`))
	require.NoError(t, err)
	base := "https://www.aventertainments.com"
	require.Equal(t, base+"/vodimages/gallery/large/fixture.jpg", extractCoverURL(doc, `script has vodimages/gallery/large/fixture.jpg`, base))
	shots := extractScreenshotURLs(doc, `href='https://www.aventertainments.com/vodimages/screenshot/large/fixture.jpg' href='https://www.aventertainments.com/vodimages/screenshot/large/fixture.jpg' src='https://www.aventertainments.com/vodimages/gallery/large/fixture/001.webp'`, base, true)
	require.Equal(t, []string{base + "/vodimages/screenshot/large/fixture.jpg", base + "/vodimages/gallery/large/fixture/001.webp"}, shots)
}

func TestCurrentScopeAVECandidateInspectionBound(t *testing.T) {
	for _, candidateCount := range []int{4, 13} {
		t.Run(fmt.Sprint(candidateCount), func(t *testing.T) {
			searches, details := 0, 0
			s := &scraper{enabled: true, baseURL: "https://www.aventertainments.com", language: "en", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(currentScopeAVETransport(func(req *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, ""
				if req.URL.Path == "/ppv/search" {
					searches++
					for i := 0; i < candidateCount; i++ {
						body += fmt.Sprintf(`<a href="/ppv/detail/%d/">candidate</a>`, 10000+i)
					}
				} else {
					details++
					status = http.StatusNotFound
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
			}))}
			found, err := s.GetURL(context.Background(), "ABC-123")
			require.NoError(t, err)
			require.Equal(t, s.applyLanguage(s.baseURL+"/ppv/detail/10000/"), found)
			require.Equal(t, 2, searches)
			require.Equal(t, min(candidateCount, 12), details, "candidate inspection deduplicates the two search result sets and caps work")
		})
	}
}

func TestCurrentScopeAVESearchDetailFailure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		transportErr bool
	}{
		{name: "non-success status", status: http.StatusBadGateway},
		{name: "read failure", transportErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := &scraper{enabled: true, language: "en", rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(currentScopeAVETransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if tc.transportErr {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(""))}, nil
			}))}
			result, err := s.Search(context.Background(), "https://www.aventertainments.com/ppv/detail/12345/")
			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, 1, calls)
			if tc.transportErr {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				classified, ok := models.AsScraperError(err)
				require.True(t, ok)
				require.Equal(t, tc.status, classified.StatusCode)
			}
		})
	}
}
