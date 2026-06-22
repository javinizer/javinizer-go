package mgstage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// missRoundTripper routes all requests to the test server regardless of URL host.
type missRoundTripper struct {
	server *httptest.Server
}

func (rt *missRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyReq := req.Clone(req.Context())
	proxyReq.URL.Scheme = "http"
	proxyReq.URL.Host = rt.server.Listener.Addr().String()
	proxyReq.Host = rt.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(proxyReq)
}

// newMGStageHTTPTScraper creates a scraper wired to a httptest.Server via round-tripper.
func newMGStageHTTPTScraper(server *httptest.Server, enabled bool) *scraper {
	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	return &scraper{
		client:      client,
		enabled:     enabled,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: enabled},
	}
}

// --- Helper: build mock search result page ---
func buildMGStageSearchHTML(productIDs ...string) string {
	links := ""
	for _, id := range productIDs {
		links += fmt.Sprintf(`<a href="/product/product_detail/%s/">%s</a>`, id, id)
	}
	return fmt.Sprintf(`<html><body>%s</body></html>`, links)
}

// --- Helper: build mock detail page ---
func buildMGStageMissDetailHTML(id, title, date, runtime, maker, label, series, description string, genres, actresses []string) string {
	genreLinks := ""
	for _, g := range genres {
		genreLinks += fmt.Sprintf(`<a href="/genre/1">%s</a>`, g)
	}

	actressLinks := ""
	for _, a := range actresses {
		actressLinks += fmt.Sprintf(`<a href="/actress/1">%s</a>`, a)
	}

	return fmt.Sprintf(`<html>
<head>
<title>「%s」：エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞</title>
</head>
<body>
<div class="detail_data">
<table>
<tr><th>品番：</th><td>%s</td></tr>
<tr><th>配信開始日：</th><td>%s</td></tr>
<tr><th>収録時間：</th><td>%s 分</td></tr>
<tr><th>メーカー：</th><td><a href="/maker/1">%s</a></td></tr>
<tr><th>レーベル：</th><td><a href="/label/1">%s</a></td></tr>
<tr><th>シリーズ：</th><td><a href="/series/1">%s</a></td></tr>
<tr><th>ジャンル：</th><td>%s</td></tr>
<tr><th>出演：</th><td>%s</td></tr>
</table>
</div>
<p class="txt introduction">%s</p>
<a class="link_magnify" href="https://www.mgstage.com/images/jacket/%s.jpg">Enlarge</a>
<a class="sample_image" href="https://www.mgstage.com/sample/sample01.jpg">Sample 1</a>
<a class="sample_image" href="https://www.mgstage.com/sample/sample02.jpg">Sample 2</a>
</body>
</html>`, title, id, date, runtime, maker, label, series, genreLinks, actressLinks, description, id)
}

// ============================================================
// Search tests
// ============================================================

func TestMiss_Search_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}
	_, err := s.Search(context.Background(), "GANA-2850")
	require.Error(t, err)
}

func TestMiss_Search_Success(t *testing.T) {
	detailHTML := buildMGStageMissDetailHTML("GANA-2850", "Test GANA Movie", "2024/01/15", "120", "TestMaker", "TestLabel", "TestSeries", "A great movie", []string{"Amateur", "Nampa"}, []string{"田中ゆき"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "/search/") {
			fmt.Fprint(w, buildMGStageSearchHTML("GANA-2850"))
			return
		}
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	result, err := s.Search(context.Background(), "GANA-2850")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "mgstage", result.Source)
	assert.Contains(t, result.Title, "Test GANA Movie")
}

func TestMiss_Search_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "/search/") {
			fmt.Fprint(w, buildMGStageSearchHTML())
			return
		}
		fmt.Fprint(w, `<html><body>not found</body></html>`)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "NOMATCH-999")
	require.Error(t, err)
}

func TestMiss_Search_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "GANA-2850")
	require.Error(t, err)
}

// ============================================================
// ScrapeURL tests
// ============================================================

func TestMiss_ScrapeURL_WrongHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request for non-MGStage URL")
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://example.com/something")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestMiss_ScrapeURL_Success(t *testing.T) {
	detailHTML := buildMGStageMissDetailHTML("SIRO-5615", "ScrapeURL Test Movie", "2024/02/20", "90", "StudioA", "LabelA", "SeriesA", "Desc", []string{"Creampie"}, []string{"Sato Hanako"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	result, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "mgstage", result.Source)
	assert.Contains(t, result.Title, "ScrapeURL Test Movie")
}

func TestMiss_ScrapeURL_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/NOMATCH-999/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestMiss_ScrapeURL_429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/GANA-2850/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindRateLimited, scraperErr.Kind)
}

func TestMiss_ScrapeURL_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/GANA-2850/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindBlocked, scraperErr.Kind)
}

func TestMiss_ScrapeURL_IDMismatch(t *testing.T) {
	// Page returns different ID than what's in the URL
	detailHTML := buildMGStageMissDetailHTML("OTHER-9999", "Wrong Movie", "2024/01/01", "60", "M", "L", "S", "D", nil, nil)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/GANA-2850/")
	require.Error(t, err)
}

// ============================================================
// GetURL tests
// ============================================================

func TestMiss_GetURL_SearchFound(t *testing.T) {
	detailHTML := buildMGStageMissDetailHTML("GANA-2850", "Test", "2024/01/01", "60", "M", "L", "S", "D", nil, nil)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "/search/") {
			fmt.Fprint(w, buildMGStageSearchHTML("GANA-2850"))
			return
		}
		// checkDirectURL - needs to return a page with 品番
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	url, err := s.GetURL(context.Background(), "GANA-2850")
	require.NoError(t, err)
	assert.Contains(t, url, "mgstage.com")
}

func TestMiss_GetURL_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		// Return empty search, and for direct URL check return page without 品番
		if strings.Contains(r.URL.Path, "/search/") {
			fmt.Fprint(w, buildMGStageSearchHTML())
			return
		}
		fmt.Fprint(w, `<html><body>no product</body></html>`)
	}))
	defer ts.Close()

	s := newMGStageHTTPTScraper(ts, true)
	_, err := s.GetURL(context.Background(), "NOMATCH-999")
	require.Error(t, err)
}

// ============================================================
// CanHandleURL tests
// ============================================================

func TestMiss_CanHandleURL(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.mgstage.com/product/product_detail/GANA-2850/", true},
		{"https://mgstage.com/anything", true},
		{"https://sub.mgstage.com/page", true},
		{"https://example.com/something", false},
		{"not-a-url", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// ============================================================
// ExtractIDFromURL tests
// ============================================================

func TestMiss_ExtractIDFromURL(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{"valid product URL", "https://www.mgstage.com/product/product_detail/GANA-2850/", "GANA-2850", false},
		{"no product path", "https://www.mgstage.com/other", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.ExtractIDFromURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, id)
			}
		})
	}
}

// ============================================================
// ResolveSearchQuery tests
// ============================================================

func TestMiss_ResolveSearchQuery(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"GANA-2850", "GANA-2850", true},
		{"SIRO-5615", "SIRO-5615", true},
		{"259LUXU-1806", "259LUXU-1806", true},
		{"", "", false},
		{"INVALID", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := s.ResolveSearchQuery(tt.input)
			assert.Equal(t, tt.ok, ok)
			if tt.expected != "" {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// ============================================================
// ResolveDownloadProxyForHost tests
// ============================================================

func TestMiss_ResolveDownloadProxyForHost(t *testing.T) {
	dlProxy := &models.ProxyConfig{Enabled: true, Profile: "dl"}
	scraperProxy := &models.ProxyConfig{Enabled: true, Profile: "sc"}
	s := &scraper{
		settings: models.ScraperSettings{
			DownloadProxy: dlProxy,
			Proxy:         scraperProxy,
		},
	}

	tests := []struct {
		host        string
		wantHandled bool
	}{
		{"mgstage.com", true},
		{"www.mgstage.com", true},
		{"example.com", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			dl, _, handled := s.ResolveDownloadProxyForHost(tt.host)
			assert.Equal(t, tt.wantHandled, handled)
			if tt.wantHandled {
				assert.Equal(t, dlProxy, dl)
			}
		})
	}
}

// ============================================================
// IsEnabled / Name / Config / Close tests
// ============================================================

func TestMiss_IsEnabled(t *testing.T) {
	s1 := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.True(t, s1.IsEnabled())

	s2 := newScraper(&models.ScraperSettings{Enabled: false}, nil, models.FlareSolverrConfig{})
	assert.False(t, s2.IsEnabled())
}

func TestMiss_Name(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.Equal(t, "mgstage", s.Name())
}

func TestMiss_Config(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	cfg := s.Config()
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	cfg.Enabled = false
	assert.True(t, s.Config().Enabled)
}

func TestMiss_Close(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.NoError(t, s.Close())
}

// ============================================================
// parseHTML unit tests
// ============================================================

func TestMiss_ParseHTML_FullDetail(t *testing.T) {
	html := buildMGStageMissDetailHTML("GANA-2850", "Full Movie Title", "2024/03/15", "90", "StudioA", "LabelA", "SeriesA", "Great movie description", []string{"Amateur", "Nampa"}, []string{"田中ゆき"})

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := newMGStageHTTPTScraper(httptest.NewServer(nil), true)
	result, err := s.parseHTML(doc, "https://www.mgstage.com/product/product_detail/GANA-2850/")
	require.NoError(t, err)
	assert.Equal(t, "GANA-2850", result.ID)
	assert.Equal(t, "Full Movie Title", result.Title)
	assert.Equal(t, "StudioA", result.Maker)
	assert.Equal(t, "LabelA", result.Label)
	assert.Equal(t, "SeriesA", result.Series)
	assert.Equal(t, "Great movie description", result.Description)
	assert.Equal(t, 90, result.Runtime)
	require.NotNil(t, result.ReleaseDate)
	assert.Equal(t, 2024, result.ReleaseDate.Year())
}

func TestMiss_ParseHTML_NoProductSignals(t *testing.T) {
	// Generic page with no product info
	html := `<html><head><title>エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞</title></head><body></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := newMGStageHTTPTScraper(httptest.NewServer(nil), true)
	_, err = s.parseHTML(doc, "https://www.mgstage.com/product/product_detail/NOMATCH/")
	require.Error(t, err)
}

// ============================================================
// Helper function unit tests
// ============================================================

func TestMiss_CleanTitle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"brackets", "「My Movie」：エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞", "My Movie"},
		{"colon", "My Movie：suffix", "My Movie"},
		{"pipe", "My Movie|suffix", "My Movie"},
		{"generic title", "エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞", ""},
		{"already clean", "Clean Title", "Clean Title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanTitle(tt.input))
		})
	}
}

func TestMiss_ExtractTableValue(t *testing.T) {
	html := `<html><body><table>
<tr><th>品番：</th><td>GANA-2850</td></tr>
<tr><th>配信開始日：</th><td>2024/01/15</td></tr>
</table></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	assert.Equal(t, "GANA-2850", extractTableValue(doc, "品番："))
	assert.Equal(t, "2024/01/15", extractTableValue(doc, "配信開始日："))
	assert.Equal(t, "", extractTableValue(doc, "nonexistent："))
}

func TestMiss_ExtractTableLinkValue(t *testing.T) {
	html := `<html><body><table>
<tr><th>メーカー：</th><td><a href="/maker/1">TestMaker</a></td></tr>
</table></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	assert.Equal(t, "TestMaker", extractTableLinkValue(doc, "メーカー："))
	assert.Equal(t, "", extractTableLinkValue(doc, "nonexistent："))
}

func TestMiss_ExtractGenres(t *testing.T) {
	html := `<html><body><table>
<tr><th>ジャンル：</th><td><a href="/genre/1">Action</a><a href="/genre/2">Drama</a></td></tr>
</table></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	genres := extractGenres(doc)
	assert.Equal(t, []string{"Action", "Drama"}, genres)
}

func TestMiss_ExtractActresses(t *testing.T) {
	html := `<html><body><table>
<tr><th>出演：</th><td><a href="/actress/1">田中ゆき</a><a href="/actress/2">Jane Smith</a></td></tr>
</table></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	actresses := extractActresses(doc)
	require.Len(t, actresses, 2)
	assert.Equal(t, "田中ゆき", actresses[0].JapaneseName)
	assert.Equal(t, "Smith", actresses[1].FirstName)
}

func TestMiss_CreateActressInfo(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		japanese  string
		firstName string
		lastName  string
	}{
		{"Japanese name", "田中ゆき", "田中ゆき", "", ""},
		{"Western name", "Jane Smith", "", "Smith", "Jane"},
		{"Single name", "Madonna", "", "Madonna", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := createActressInfo(tt.input)
			assert.Equal(t, tt.japanese, info.JapaneseName)
			assert.Equal(t, tt.firstName, info.FirstName)
			assert.Equal(t, tt.lastName, info.LastName)
		})
	}
}

func TestMiss_ExtractRating(t *testing.T) {
	html := `<html><body>
<div class="star_40">Rating</div>
<div class="review_cnt">(15)</div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	rating := extractRating(doc)
	require.NotNil(t, rating)
	assert.Equal(t, 8.0, rating.Score) // 40/5 = 8.0
}

func TestMiss_ExtractRating_Nil(t *testing.T) {
	html := `<html><body>no rating</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	rating := extractRating(doc)
	assert.Nil(t, rating)
}

func TestMiss_ExtractCoverURL(t *testing.T) {
	html := `<html><body>
<a class="link_magnify" href="https://www.mgstage.com/images/jacket/test.jpg">Enlarge</a>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractCoverURL(doc)
	assert.Equal(t, "https://www.mgstage.com/images/jacket/test.jpg", result)
}

func TestMiss_ExtractCoverURL_ImgSrc(t *testing.T) {
	html := `<html><body>
<img src="https://www.mgstage.com/images/jacket/ps.test.jpg" alt="cover"/>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractCoverURL(doc)
	assert.Contains(t, result, "pl.test.jpg") // ps -> pl upgrade
}

func TestMiss_ExtractScreenshots(t *testing.T) {
	html := `<html><body>
<a class="sample_image" href="https://www.mgstage.com/sample/sample01.jpg">1</a>
<a class="sample_image" href="https://www.mgstage.com/sample/sample02.jpg">2</a>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	screenshots := extractScreenshots(doc)
	assert.Len(t, screenshots, 2)
}

func TestMiss_ExtractDescription(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{"p.txt.introduction", `<html><body><p class="txt introduction">A great story</p></body></html>`, "A great story"},
		{"#introduction dd", `<html><body><div id="introduction"><dd>A great story</dd></div></body></html>`, "A great story"},
		{"meta description", `<html><head><meta name="Description" content="Meta desc"/></head><body></body></html>`, "Meta desc"},
		{"no description", `<html><body></body></html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractDescription(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMiss_NormalizeIDForSearch(t *testing.T) {
	assert.Equal(t, "gana2850", normalizeIDForSearch("GANA-2850"))
	assert.Equal(t, "siro5615", normalizeIDForSearch("SIRO-5615"))
}

func TestMiss_SplitMGStageID(t *testing.T) {
	letter, number := splitMGStageID("GANA-2850")
	assert.Equal(t, "GANA", letter)
	assert.Equal(t, "2850", number)

	letter, number = splitMGStageID("INVALID")
	assert.Empty(t, letter)
	assert.Empty(t, number)
}

func TestMiss_ExpandMGStagePrefixes(t *testing.T) {
	candidates := expandMGStagePrefixes("GANA", "2850")
	assert.NotEmpty(t, candidates)
	assert.Contains(t, candidates[0], "200GANA-2850")
}

func TestMiss_NormalizeMGStageIDToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"GANA-2850", "GANA-2850", true},
		{"GANA2850", "", false},
		{"gana_2850", "GANA-2850", true},
		{"", "", false},
		{"123", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := normalizeMGStageIDToken(tt.input)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestMiss_MGStageIDsMatch(t *testing.T) {
	tests := []struct {
		a, b      string
		wantMatch bool
	}{
		{"GANA-2850", "GANA-2850", true},
		{"GANA-2850", "200GANA-2850", true},
		{"GANA-2850", "OTHER-9999", false},
		{"", "GANA-2850", false},
	}

	for _, tt := range tests {
		name := fmt.Sprintf("%s_vs_%s", tt.a, tt.b)
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.wantMatch, mgstageIDsMatch(tt.a, tt.b))
		})
	}
}

func TestMiss_HasProductSignals(t *testing.T) {
	tests := []struct {
		name     string
		result   *models.ScraperResult
		tableID  string
		expected bool
	}{
		{"nil result", nil, "", false},
		{"with tableID", &models.ScraperResult{}, "GANA-2850", true},
		{"with runtime", &models.ScraperResult{Runtime: 90}, "", true},
		{"with genres", &models.ScraperResult{Genres: []string{"Action"}}, "", true},
		{"empty result", &models.ScraperResult{}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasProductSignals(tt.result, tt.tableID))
		})
	}
}

func TestMiss_IsGenericMGStageTitle(t *testing.T) {
	assert.True(t, isGenericMGStageTitle("エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞"))
	assert.False(t, isGenericMGStageTitle("My Movie Title"))
}

func TestMiss_IsGenericMGStageDescription(t *testing.T) {
	assert.True(t, isGenericMGStageDescription("MGS動画 エロ動画 test"))
	assert.False(t, isGenericMGStageDescription("A great movie"))
}

func TestMiss_HTTPStatusError(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}

	err := s.httpStatusError("detail", 500)
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, 500, scraperErr.StatusCode)
}

func TestMiss_HTTPStatusError_403_WithProxy(t *testing.T) {
	s := &scraper{usingProxy: true, settings: models.ScraperSettings{Enabled: true}}

	err := s.httpStatusError("detail", 403)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proxy")
}

func TestMiss_ExtractIDFromURL_Helper(t *testing.T) {
	assert.Equal(t, "GANA-2850", extractIDFromURL("/product/product_detail/GANA-2850/"))
	assert.Equal(t, "", extractIDFromURL("/other/path"))
}
