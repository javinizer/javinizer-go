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

// --- extractTableValue: sibling th/td pattern ---

func TestMiss2_ExtractTableValue_SiblingPattern(t *testing.T) {
	html := `<html><body><table class="detail_data">
		<tr><th>品番：</th><td>SIRO-5615</td></tr>
		<tr><th>配信開始日：</th><td>2024/01/15</td></tr>
	</table></body></html>`
	doc := docFromHTMLMGS(t, html)
	assert.Equal(t, "SIRO-5615", extractTableValue(doc, "品番："))
	assert.Equal(t, "2024/01/15", extractTableValue(doc, "配信開始日："))
}

// --- extractTableValue: standard tr pattern ---

func TestMiss2_ExtractTableValue_TRPattern(t *testing.T) {
	html := `<html><body><table>
		<tr><th>品番：</th><td>200GANA-2850</td></tr>
		<tr><th>メーカー：</th><td>Maker Name</td></tr>
	</table></body></html>`
	doc := docFromHTMLMGS(t, html)
	assert.Equal(t, "200GANA-2850", extractTableValue(doc, "品番："))
	assert.Equal(t, "Maker Name", extractTableValue(doc, "メーカー："))
}

// --- extractTableLinkValue: sibling th/td pattern ---

func TestMiss2_ExtractTableLinkValue_SiblingPattern(t *testing.T) {
	html := `<html><body><table class="detail_data">
		<tr><th>メーカー：</th><td><a href="/maker/1">Maker A</a></td></tr>
		<tr><th>レーベル：</th><td><a href="/label/1">Label A</a></td></tr>
		<tr><th>シリーズ：</th><td><a href="/series/1">Series A</a></td></tr>
	</table></body></html>`
	doc := docFromHTMLMGS(t, html)
	assert.Equal(t, "Maker A", extractTableLinkValue(doc, "メーカー："))
	assert.Equal(t, "Label A", extractTableLinkValue(doc, "レーベル："))
	assert.Equal(t, "Series A", extractTableLinkValue(doc, "シリーズ："))
}

// --- extractTableLinkValue: standard tr pattern ---

func TestMiss2_ExtractTableLinkValue_TRPattern(t *testing.T) {
	html := `<html><body><table>
		<tr><th>メーカー：</th><td><a href="/maker/1">Maker TR</a></td></tr>
		<tr><th>レーベル：</th><td><a href="/label/1">Label TR</a></td></tr>
	</table></body></html>`
	doc := docFromHTMLMGS(t, html)
	assert.Equal(t, "Maker TR", extractTableLinkValue(doc, "メーカー："))
	assert.Equal(t, "Label TR", extractTableLinkValue(doc, "レーベル："))
}

// --- extractTableLinkValue: not found returns empty ---

func TestMiss2_ExtractTableLinkValue_NotFound(t *testing.T) {
	html := `<html><body><table><tr><th>Other：</th><td>val</td></tr></table></body></html>`
	doc := docFromHTMLMGS(t, html)
	assert.Equal(t, "", extractTableLinkValue(doc, "メーカー："))
}

// --- extractGenres: sibling pattern with plain text fallback ---

func TestMiss2_ExtractGenres_SiblingPlainText(t *testing.T) {
	// Standard TR pattern with links
	html := `<html><body><table>
		<tr><th>ジャンル：</th><td><a href="/genre/1">Documentary</a></td></tr>
	</table></body></html>`
	doc := docFromHTMLMGS(t, html)
	genres := extractGenres(doc)
	assert.Contains(t, genres, "Documentary")
}

// --- extractGenres: standard tr pattern ---

func TestMiss2_ExtractGenres_TRPattern(t *testing.T) {
	html := `<html><body><table>
		<tr><th>ジャンル：</th><td><a href="/genre/1">Genre A</a><a href="/genre/2">Genre B</a></td></tr>
	</table></body></html>`
	doc := docFromHTMLMGS(t, html)
	genres := extractGenres(doc)
	assert.Contains(t, genres, "Genre A")
	assert.Contains(t, genres, "Genre B")
}

// --- extractActresses: sibling pattern ---

func TestMiss2_ExtractActresses_TRPattern(t *testing.T) {
	html := `<html><body><table>
		<tr><th>出演：</th><td><a href="/actress/1">田中麻美</a></td></tr>
	</table></body></html>`
	doc := docFromHTMLMGS(t, html)
	actresses := extractActresses(doc)
	require.GreaterOrEqual(t, len(actresses), 1)
	assert.Equal(t, "田中麻美", actresses[0].JapaneseName)
}

// --- createActressInfo: Japanese name ---

func TestMiss2_CreateActressInfo_Japanese(t *testing.T) {
	info := createActressInfo("田中麻美")
	assert.Equal(t, "田中麻美", info.JapaneseName)
	assert.Empty(t, info.FirstName)
	assert.Empty(t, info.LastName)
}

// --- createActressInfo: Western name ---

func TestMiss2_CreateActressInfo_Western(t *testing.T) {
	info := createActressInfo("Jane Doe")
	assert.Equal(t, "Jane", info.LastName)
	assert.Equal(t, "Doe", info.FirstName)
	assert.Empty(t, info.JapaneseName)
}

// --- createActressInfo: single name ---

func TestMiss2_CreateActressInfo_SingleName(t *testing.T) {
	info := createActressInfo("Actress1")
	assert.Equal(t, "Actress1", info.FirstName)
	assert.Empty(t, info.LastName)
	assert.Empty(t, info.JapaneseName)
}

// --- cleanTitle: Japanese bracket format ---

func TestMiss2_CleanTitle_JapaneseBrackets(t *testing.T) {
	title := "「My Movie Title」：エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞"
	assert.Equal(t, "My Movie Title", cleanTitle(title))
}

// --- cleanTitle: fallback colon split ---

func TestMiss2_CleanTitle_ColonSplit(t *testing.T) {
	title := "Some Title：サイトのサフィックス"
	cleaned := cleanTitle(title)
	assert.NotContains(t, cleaned, "サイト")
}

// --- cleanTitle: generic title returns empty ---

func TestMiss2_CleanTitle_GenericReturnsEmpty(t *testing.T) {
	title := "エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞"
	assert.Equal(t, "", cleanTitle(title))
}

// --- isGenericMGStageTitle ---

func TestMiss2_IsGenericMGStageTitle(t *testing.T) {
	assert.True(t, isGenericMGStageTitle("エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞"))
	assert.False(t, isGenericMGStageTitle("My Movie"))
	assert.False(t, isGenericMGStageTitle(""))
}

// --- isGenericMGStageDescription ---

func TestMiss2_IsGenericMGStageDescription(t *testing.T) {
	assert.True(t, isGenericMGStageDescription("MGS動画 エロ動画 test"))
	assert.True(t, isGenericMGStageDescription("MGS動画 アダルトビデオ test"))
	assert.False(t, isGenericMGStageDescription("A real description"))
	assert.False(t, isGenericMGStageDescription(""))
}

// --- normalizeIDForSearch ---

func TestMiss2_NormalizeIDForSearch(t *testing.T) {
	assert.Equal(t, "gana2850", normalizeIDForSearch("GANA-2850"))
	assert.Equal(t, "siro5615", normalizeIDForSearch("SIRO-5615"))
	assert.Equal(t, "200gana2850", normalizeIDForSearch("200GANA-2850"))
}

// --- splitMGStageID ---

func TestMiss2_SplitMGStageID(t *testing.T) {
	letter, number := splitMGStageID("GANA-2850")
	assert.Equal(t, "GANA", letter)
	assert.Equal(t, "2850", number)

	letter, number = splitMGStageID("SIRO-5615")
	assert.Equal(t, "SIRO", letter)
	assert.Equal(t, "5615", number)

	letter, number = splitMGStageID("invalid")
	assert.Equal(t, "", letter)
	assert.Equal(t, "", number)
}

// --- expandMGStagePrefixes ---

func TestMiss2_ExpandMGStagePrefixes(t *testing.T) {
	prefixes := expandMGStagePrefixes("GANA", "2850")
	require.NotEmpty(t, prefixes)
	assert.Equal(t, "200GANA-2850", prefixes[0])
	assert.Equal(t, "259GANA-2850", prefixes[1])
}

// --- normalizeMGStageIDToken ---

func TestMiss2_NormalizeMGStageIDToken(t *testing.T) {
	id, ok := normalizeMGStageIDToken("GANA-2850")
	assert.True(t, ok)
	assert.Equal(t, "GANA-2850", id)

	// Compact ID format: 259GANA2850 -> 259GANA-2850
	id, ok = normalizeMGStageIDToken("259GANA2850")
	assert.True(t, ok)
	assert.Equal(t, "259GANA-2850", id)

	id, ok = normalizeMGStageIDToken("")
	assert.False(t, ok)

	id, ok = normalizeMGStageIDToken("INVALID")
	assert.False(t, ok)
}

// --- extractIDFromURL ---

func TestMiss2_ExtractIDFromURL(t *testing.T) {
	assert.Equal(t, "200GANA-2850", extractIDFromURL("https://www.mgstage.com/product/product_detail/200GANA-2850/"))
	assert.Equal(t, "", extractIDFromURL("https://www.mgstage.com/other/path/"))
}

// --- hasProductSignals ---

func TestMiss2_HasProductSignals(t *testing.T) {
	// With tableID
	assert.True(t, hasProductSignals(&models.ScraperResult{}, "SIRO-5615"))

	// With runtime
	assert.True(t, hasProductSignals(&models.ScraperResult{Runtime: 120}, ""))

	// With maker
	assert.True(t, hasProductSignals(&models.ScraperResult{Maker: "Test"}, ""))

	// With genres
	assert.True(t, hasProductSignals(&models.ScraperResult{Genres: []string{"test"}}, ""))

	// With cover URL
	assert.True(t, hasProductSignals(&models.ScraperResult{CoverURL: "https://example.com/cover.jpg"}, ""))

	// Nil result
	assert.False(t, hasProductSignals(nil, ""))

	// Empty result
	assert.False(t, hasProductSignals(&models.ScraperResult{}, ""))
}

// --- mgstageIDsMatch ---

func TestMiss2_MGStageIDsMatch(t *testing.T) {
	assert.True(t, mgstageIDsMatch("GANA-2850", "200GANA-2850"))
	assert.True(t, mgstageIDsMatch("GANA-2850", "GANA-2850"))
	assert.False(t, mgstageIDsMatch("GANA-2850", "SIRO-5615"))
	assert.False(t, mgstageIDsMatch("", "GANA-2850"))
	assert.False(t, mgstageIDsMatch("GANA-2850", ""))
}

// --- ScrapeURL: not handled URL ---

func TestMiss2_ScrapeURL_UnhandledURL(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request")
	})), true)
	_, err := s.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- ScrapeURL: 404 status ---

func TestMiss2_ScrapeURL_Status404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// --- ScrapeURL: 429 status ---

func TestMiss2_ScrapeURL_Status429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusTooManyRequests, scraperErr.StatusCode)
}

// --- ScrapeURL: 403 status ---

func TestMiss2_ScrapeURL_Status403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "access blocked")
}

// --- ScrapeURL: 451 status ---

func TestMiss2_ScrapeURL_Status451(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(451)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "access blocked")
}

// --- ScrapeURL: non-200 status ---

func TestMiss2_ScrapeURL_Status500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "status code 500")
}

// --- ScrapeURL: rate limiter error ---

func TestMiss2_ScrapeURL_RateLimiterError(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ScrapeURL(ctx, "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.Error(t, err)
}

// --- Search: rate limiter error on getURLCtx ---

func TestMiss2_Search_GetURLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	_, err := s.Search(context.Background(), "NOTFOUND-99999")
	require.Error(t, err)
}

// --- Search: cancelled context ---

func TestMiss2_Search_CancelledContext(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Search(ctx, "SIRO-5615")
	require.Error(t, err)
}

// --- CanHandleURL ---

func TestMiss2_CanHandleURL(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	assert.True(t, s.CanHandleURL("https://www.mgstage.com/product/product_detail/SIRO-5615/"))
	assert.False(t, s.CanHandleURL("https://example.com/test"))
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- ResolveSearchQuery ---

func TestMiss2_ResolveSearchQuery(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)

	id, ok := s.ResolveSearchQuery("GANA-2850")
	assert.True(t, ok)
	assert.Equal(t, "GANA-2850", id)

	id, ok = s.ResolveSearchQuery("")
	assert.False(t, ok)

	id, ok = s.ResolveSearchQuery("https://www.mgstage.com/product/product_detail/SIRO-5615/")
	assert.True(t, ok)
	assert.Equal(t, "SIRO-5615", id)

	_, ok = s.ResolveSearchQuery("not-a-valid-id")
	assert.False(t, ok)
}

// --- extractCoverURL ---

func TestMiss2_ExtractCoverURL(t *testing.T) {
	html := `<html><body>
		<a class="link_magnify" href="https://www.mgstage.com/jacket/siro5615.jpg">Enlarge</a>
	</body></html>`
	doc := docFromHTMLMGS(t, html)
	cover := extractCoverURL(doc)
	assert.Contains(t, cover, "siro5615.jpg")
}

// --- extractCoverURL: relative URL ---

func TestMiss2_ExtractCoverURL_RelativeURL(t *testing.T) {
	html := `<html><body>
		<a class="link_magnify" href="/jacket/siro5615.jpg">Enlarge</a>
	</body></html>`
	doc := docFromHTMLMGS(t, html)
	cover := extractCoverURL(doc)
	assert.Contains(t, cover, "mgstage.com")
	assert.Contains(t, cover, "siro5615.jpg")
}

// --- extractCoverURL: img fallback ---

func TestMiss2_ExtractCoverURL_ImgFallback(t *testing.T) {
	html := `<html><body>
		<img src="https://www.mgstage.com/jacket/siro5615ps.jpg" alt="cover"/>
	</body></html>`
	doc := docFromHTMLMGS(t, html)
	cover := extractCoverURL(doc)
	assert.Contains(t, cover, "pl.jpg")
}

// --- extractScreenshots ---

func TestMiss2_ExtractScreenshots(t *testing.T) {
	html := `<html><body>
		<a class="sample_image" href="https://www.mgstage.com/sample/siro5615-1.jpg">S1</a>
		<a class="sample_image" href="https://www.mgstage.com/sample/siro5615-2.jpg">S2</a>
	</body></html>`
	doc := docFromHTMLMGS(t, html)
	screenshots := extractScreenshots(doc)
	require.Len(t, screenshots, 2)
}

// --- extractScreenshots: relative URLs ---

func TestMiss2_ExtractScreenshots_RelativeURLs(t *testing.T) {
	html := `<html><body>
		<a class="sample_image" href="/sample/siro5615-1.jpg">S1</a>
	</body></html>`
	doc := docFromHTMLMGS(t, html)
	screenshots := extractScreenshots(doc)
	require.Len(t, screenshots, 1)
	assert.Contains(t, screenshots[0], "mgstage.com")
}

// --- extractDescription ---

func TestMiss2_ExtractDescription_Paragraph(t *testing.T) {
	html := `<html><body><p class="txt introduction">A great movie description</p></body></html>`
	doc := docFromHTMLMGS(t, html)
	desc := extractDescription(doc)
	assert.Equal(t, "A great movie description", desc)
}

// --- extractDescription: nil doc ---

func TestMiss2_ExtractDescription_NilDoc(t *testing.T) {
	assert.Equal(t, "", extractDescription(nil))
}

// --- extractDescription: meta og:description fallback ---

func TestMiss2_ExtractDescription_MetaFallback(t *testing.T) {
	html := `<html><head><meta property="og:description" content="OG Description"/></head><body></body></html>`
	doc := docFromHTMLMGS(t, html)
	desc := extractDescription(doc)
	assert.Equal(t, "OG Description", desc)
}

// --- extractDescription: meta Description fallback ---

func TestMiss2_ExtractDescription_MetaDescriptionFallback(t *testing.T) {
	html := `<html><head><meta name="Description" content="Meta Desc"/></head><body></body></html>`
	doc := docFromHTMLMGS(t, html)
	desc := extractDescription(doc)
	assert.Equal(t, "Meta Desc", desc)
}

// --- extractRating ---

func TestMiss2_ExtractRating_StarClass(t *testing.T) {
	html := `<html><body><div class="star_40">4.0</div></body></html>`
	doc := docFromHTMLMGS(t, html)
	rating := extractRating(doc)
	require.NotNil(t, rating)
	assert.InDelta(t, 8.0, rating.Score, 0.01)
}

// --- extractRating: no rating ---

func TestMiss2_ExtractRating_NoRating(t *testing.T) {
	html := `<html><body><p>No rating here</p></body></html>`
	doc := docFromHTMLMGS(t, html)
	assert.Nil(t, extractRating(doc))
}

// --- extractRating: with votes ---

func TestMiss2_ExtractRating_WithVotes(t *testing.T) {
	html := `<html><body>
		<div class="star_45">4.5</div>
		<span class="review_cnt">(15)</span>
	</body></html>`
	doc := docFromHTMLMGS(t, html)
	rating := extractRating(doc)
	require.NotNil(t, rating)
	assert.Equal(t, 15, rating.Votes)
}

// --- extractTrailerURL: returns empty (current implementation) ---

func TestMiss2_ExtractTrailerURL(t *testing.T) {
	html := `<html><body><iframe src="/sample/abc123/"></iframe></body></html>`
	doc := docFromHTMLMGS(t, html)
	client := resty.New()
	assert.Equal(t, "", extractTrailerURL(doc, client))
}

// --- httpStatusError: with proxy ---

func TestMiss2_HttpStatusError_WithProxy(t *testing.T) {
	s := &scraper{usingProxy: true}
	err := s.httpStatusError("detail", 403)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "proxy")
}

// --- httpStatusError: without proxy ---

func TestMiss2_HttpStatusError_NoProxy(t *testing.T) {
	s := &scraper{usingProxy: false}
	err := s.httpStatusError("detail", 403)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "access blocked")
}

// --- checkDirectURL: cancelled context ---

func TestMiss2_CheckDirectURL_CancelledContext(t *testing.T) {
	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, s.checkDirectURL(ctx, "https://www.mgstage.com/product/product_detail/SIRO-5615/"))
}

// --- findProductInSearchResults ---

func TestMiss2_FindProductInSearchResults(t *testing.T) {
	html := `<html><body>
		<a href="/product/product_detail/SIRO-5615/">SIRO-5615</a>
	</body></html>`
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	found := s.findProductInSearchResults(html, "SIRO-5615")
	assert.Contains(t, found, "SIRO-5615")
}

// --- findProductInSearchResults: no match ---

func TestMiss2_FindProductInSearchResults_NoMatch(t *testing.T) {
	html := `<html><body><a href="/product/product_detail/OTHER-001/">OTHER</a></body></html>`
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	found := s.findProductInSearchResults(html, "SIRO-5615")
	assert.Equal(t, "", found)
}

// --- searchByID: cancelled context ---

func TestMiss2_SearchByID_CancelledContext(t *testing.T) {
	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.searchByID(ctx, "SIRO-5615")
	require.Error(t, err)
}

// --- getURLCtx: search returns nothing, expands prefixes ---

func TestMiss2_GetURLCtx_SearchFailsThenExpands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	_, err := s.getURLCtx(context.Background(), "GANA-2850")
	require.Error(t, err)
}

// --- extractDescription: generic description filtered ---

func TestMiss2_ExtractDescription_GenericFiltered(t *testing.T) {
	html := `<html><head><meta property="og:description" content="MGS動画 エロ動画 content"/></head><body></body></html>`
	doc := docFromHTMLMGS(t, html)
	desc := extractDescription(doc)
	assert.Equal(t, "", desc)
}

// --- Search: successful parse with matching ID ---

func TestMiss2_Search_Success(t *testing.T) {
	detailHTML := buildMGStageMissDetailHTML("SIRO-5615", "Test Title", "2024/01/15", "60", "TestMaker", "TestLabel", "TestSeries", "Description", []string{"Genre1"}, []string{"Actress1"})
	searchHTML := buildMGStageSearchHTML("SIRO-5615")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "cSearch.php") {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, searchHTML)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, detailHTML)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	result, err := s.Search(context.Background(), "SIRO-5615")
	require.NoError(t, err)
	assert.Equal(t, "mgstage", result.Source)
	assert.Equal(t, "SIRO-5615", result.ID)
}

// --- Search: ID mismatch after parse ---

func TestMiss2_Search_IDMismatch(t *testing.T) {
	detailHTML := buildMGStageMissDetailHTML("OTHER-001", "Test Title", "2024/01/15", "60", "TestMaker", "TestLabel", "TestSeries", "Description", []string{"Genre1"}, []string{"Actress1"})
	searchHTML := buildMGStageSearchHTML("SIRO-5615")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "cSearch.php") {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, searchHTML)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, detailHTML)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	_, err := s.Search(context.Background(), "SIRO-5615")
	require.Error(t, err)
}

// --- Helper: doc from HTML ---

func docFromHTMLMGS(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	return doc
}

// --- ExtractIDFromURL_Method ---

func TestMiss2_ExtractIDFromURL_Method(t *testing.T) {
	s := newMGStageHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	id, err := s.ExtractIDFromURL("https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.NoError(t, err)
	assert.Equal(t, "SIRO-5615", id)

	_, err = s.ExtractIDFromURL("https://example.com/no-match")
	require.Error(t, err)
}

// --- ResolveDownloadProxyForHost ---

func TestMiss2_ResolveDownloadProxyForHost(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}

	dp, sp, ok := s.ResolveDownloadProxyForHost("mgstage.com")
	assert.True(t, ok)
	assert.Nil(t, dp)
	assert.Nil(t, sp)

	dp, sp, ok = s.ResolveDownloadProxyForHost("sub.mgstage.com")
	assert.True(t, ok)

	dp, sp, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)

	dp, sp, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
}

// --- ScrapeURL: parse error (not a valid MGStage URL) ---

func TestMiss2_ScrapeURL_ExtractIDFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := newMGStageHTTPTScraper(server, true)
	// This URL passes CanHandleURL but fails ExtractIDFromURL
	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/other/path/")
	require.Error(t, err)
}

// --- ScrapeURL: fetch error ---

func TestMiss2_ScrapeURL_FetchError(t *testing.T) {
	client := resty.New()
	client.SetTimeout(1 * time.Millisecond)
	client.SetTransport(&errorRoundTripperMGS{})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.Error(t, err)
}

type errorRoundTripperMGS struct{}

func (rt *errorRoundTripperMGS) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("forced error")
}
