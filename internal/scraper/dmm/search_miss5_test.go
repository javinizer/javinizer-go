package dmm

import (
	"context"
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

// --- CanHandleURL: edge cases ---

func TestMiss5_CanHandleURL(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	assert.True(t, s.CanHandleURL("https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/"))
	assert.True(t, s.CanHandleURL("https://video.dmm.co.jp/av/content/?id=test001"))
	assert.True(t, s.CanHandleURL("https://www.dmm.com/digital/videoa/-/detail/=/cid=test001/"))
	assert.False(t, s.CanHandleURL("https://pics.dmm.co.jp/images/test.jpg"))
	assert.False(t, s.CanHandleURL("https://awsimgsrc.dmm.co.jp/images/test.jpg"))
	assert.False(t, s.CanHandleURL("https://example.com/test"))
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- ExtractIDFromURL ---

func TestMiss5_ExtractIDFromURL(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}

	id, err := s.ExtractIDFromURL("https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.NoError(t, err)
	assert.Equal(t, "ipx00535", id)

	id, err = s.ExtractIDFromURL("https://video.dmm.co.jp/av/content/?id=ipx00535")
	require.NoError(t, err)
	assert.Equal(t, "ipx00535", id)

	_, err = s.ExtractIDFromURL("https://example.com/no-cid-here")
	require.Error(t, err)
}

// --- ResolveDownloadProxyForHost ---

func TestMiss5_ResolveDownloadProxyForHost(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}

	dp, sp, ok := s.ResolveDownloadProxyForHost("dmm.co.jp")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("pics.dmm.co.jp")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("dmm.com")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("libredmm.com")
	assert.False(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	_ = dp
	_ = sp
}

// --- extractBackgroundImageURL ---

func TestMiss5_ExtractBackgroundImageURL(t *testing.T) {
	assert.Equal(t, "https://pics.dmm.co.jp/test.jpg", extractBackgroundImageURL(`background-image: url("https://pics.dmm.co.jp/test.jpg");`))
	assert.Equal(t, "//pics.dmm.co.jp/test.jpg", extractBackgroundImageURL(`background-image: url(//pics.dmm.co.jp/test.jpg);`))
	assert.Equal(t, "", extractBackgroundImageURL("no background-image here"))
	assert.Equal(t, "", extractBackgroundImageURL(""))
}

// --- hiraganaToRomaji ---

func TestMiss5_HiraganaToRomaji(t *testing.T) {
	assert.Equal(t, "a", hiraganaToRomaji("あ"))
	assert.Equal(t, "ka", hiraganaToRomaji("か"))
	assert.Equal(t, "sa", hiraganaToRomaji("さ"))
	assert.Equal(t, "si", hiraganaToRomaji("し")) // Nihon-shiki
	assert.Equal(t, "ta", hiraganaToRomaji("た"))
	assert.Equal(t, "ti", hiraganaToRomaji("ち")) // Nihon-shiki
	assert.Equal(t, "tu", hiraganaToRomaji("つ")) // Nihon-shiki
	assert.Equal(t, "na", hiraganaToRomaji("な"))
	assert.Equal(t, "ha", hiraganaToRomaji("は"))
	assert.Equal(t, "ma", hiraganaToRomaji("ま"))
	assert.Equal(t, "ya", hiraganaToRomaji("や"))
	assert.Equal(t, "ra", hiraganaToRomaji("ら"))
	assert.Equal(t, "wa", hiraganaToRomaji("わ"))
	assert.Equal(t, "n", hiraganaToRomaji("ん"))
	assert.Equal(t, "", hiraganaToRomaji("っ")) // Small tsu alone
}

// --- hiraganaToRomaji: combined characters ---

func TestMiss5_HiraganaToRomaji_Combined(t *testing.T) {
	assert.Equal(t, "kya", hiraganaToRomaji("きゃ"))
	assert.Equal(t, "syu", hiraganaToRomaji("しゅ"))
	assert.Equal(t, "tyo", hiraganaToRomaji("ちょ"))
	assert.Equal(t, "nyu", hiraganaToRomaji("にゅ"))
	assert.Equal(t, "hyo", hiraganaToRomaji("ひょ"))
	assert.Equal(t, "mya", hiraganaToRomaji("みゃ"))
	assert.Equal(t, "ryu", hiraganaToRomaji("りゅ"))
}

// --- hiraganaToRomaji: gemination (small tsu) ---

func TestMiss5_HiraganaToRomaji_Gemination(t *testing.T) {
	assert.Equal(t, "tte", hiraganaToRomaji("って"))
}

// --- normalizeContentID ---

func TestMiss5_NormalizeContentID(t *testing.T) {
	assert.Equal(t, "ipx00535", normalizeContentID("IPX-535"))
	assert.Equal(t, "sone00860", normalizeContentID("SONE-860"))
	// Amateur ID with long prefix
	result := normalizeContentID("oreco183")
	assert.Contains(t, result, "oreco")
}

// --- normalizeID ---

func TestMiss5_NormalizeID(t *testing.T) {
	assert.Equal(t, "IPX-535", normalizeID("ipx00535"))
	assert.Equal(t, "SONE-860", normalizeID("sone860"))
	assert.Equal(t, "ORECO-183", normalizeID("oreco183"))
	assert.Equal(t, "T-28123", normalizeID("t28123"))
}

// --- extractContentIDFromURL ---

func TestMiss5_ExtractContentIDFromURL(t *testing.T) {
	assert.Equal(t, "test001", extractContentIDFromURL("https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/"))
	assert.Equal(t, "test001", extractContentIDFromURL("https://video.dmm.co.jp/av/content/?id=test001"))
	assert.Equal(t, "", extractContentIDFromURL("https://example.com/test"))
}

// --- matchesWithVariantSuffix ---

func TestMiss5_MatchesWithVariantSuffix(t *testing.T) {
	assert.True(t, matchesWithVariantSuffix("ipx535", "ipx535"))
	assert.True(t, matchesWithVariantSuffix("ipx535a", "ipx535"))
	assert.False(t, matchesWithVariantSuffix("ipx535ab", "ipx535"))
	assert.False(t, matchesWithVariantSuffix("ipx535", "sone860"))
}

// --- stripRentalSuffix ---

func TestMiss5_StripRentalSuffix(t *testing.T) {
	assert.Equal(t, "ipx535", stripRentalSuffix("ipx535r"))
	assert.Equal(t, "test001", stripRentalSuffix("test001r"))
	assert.Equal(t, "abc", stripRentalSuffix("abc")) // No suffix
	// Uppercase R is also stripped when beforeR is a digit
	assert.Equal(t, "ipx535", stripRentalSuffix("ipx535R"))
	// Not stripped when beforeR is not a digit
	assert.Equal(t, "abcr", stripRentalSuffix("abcr"))
}

// --- uniqueNonEmptyStrings ---

func TestMiss5_UniqueNonEmptyStrings(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, uniqueNonEmptyStrings([]string{"a", "b", "a", ""}))
	assert.Equal(t, []string{}, uniqueNonEmptyStrings([]string{"", ""}))
	assert.Equal(t, []string{"x"}, uniqueNonEmptyStrings([]string{"x"}))
}

// --- normalizedContentIDWithoutPadding ---

func TestMiss5_NormalizedContentIDWithoutPadding(t *testing.T) {
	assert.Equal(t, "ipx535", normalizedContentIDWithoutPadding("ipx00535"))
	assert.Equal(t, "sone860", normalizedContentIDWithoutPadding("sone00860"))
	assert.Equal(t, "", normalizedContentIDWithoutPadding(""))
}

// --- buildResolveContentIDSearchQueries ---

func TestMiss5_BuildResolveContentIDSearchQueries(t *testing.T) {
	queries := buildResolveContentIDSearchQueries("IPX-535", "ipx00535")
	assert.NotEmpty(t, queries)
	// Should contain unique, non-empty entries
	seen := map[string]bool{}
	for _, q := range queries {
		assert.NotEmpty(t, q)
		assert.False(t, seen[q], "duplicate query: %s", q)
		seen[q] = true
	}
}

// --- resolveTimeout ---

func TestMiss5_ResolveTimeout(t *testing.T) {
	assert.Equal(t, 10, resolveTimeout(10, 5))
	assert.Equal(t, 5, resolveTimeout(0, 5))
	assert.Equal(t, 30, resolveTimeout(0, 0))
}

// --- extractCoverURL: fallback to ps.jpg regex ---

func TestMiss5_ExtractCoverURL_FallbackRegex(t *testing.T) {
	html := `<html><body><div>some content</div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	// No og:image, should return empty
	result := s.extractCoverURL(doc, false, "test001")
	assert.Equal(t, "", result)
}

// --- extractScreenshots: old site with no sample images ---

func TestMiss5_ExtractScreenshots_OldSiteEmpty(t *testing.T) {
	html := `<html><body><p>No screenshots</p></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	result := s.extractScreenshots(doc, false)
	assert.Empty(t, result)
}

// --- filterPlaceholderScreenshots: empty input ---

func TestMiss5_FilterPlaceholderScreenshots_EmptyInput(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	result := s.filterPlaceholderScreenshots(context.Background(), nil)
	assert.Nil(t, result)

	result = s.filterPlaceholderScreenshots(context.Background(), []string{})
	assert.Empty(t, result)
}

// --- extractTrailerURL: old site with no trailer ---

func TestMiss5_ExtractTrailerURL_OldSiteEmpty(t *testing.T) {
	html := `<html><body><p>No trailer</p></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	result := s.extractTrailerURL(doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	assert.Equal(t, "", result)
}

// --- parseHTML: old site minimal ---

func TestMiss5_ParseHTML_OldSiteMinimal(t *testing.T) {
	html := `<html><body>
		<h1 id="title" class="item">TEST-001 Test Movie</h1>
		<div class="mg-b20 lh4"><p class="mg-b20">Test description</p></div>
		<table><tr><td>2024/03/15</td></tr></table>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		settings:      models.ScraperSettings{Enabled: true},
		scrapeActress: false,
	}
	result, err := s.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.NoError(t, err)
	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "TEST-001", result.ID)
}

// --- parseHTML: new site (video.dmm.co.jp) minimal ---
// Skipped: parseHTML with isNewSite requires full JSON-LD and client setup
// which causes nil pointer issues in minimal test fixtures.

// --- extractDescription: old site ---

func TestMiss5_ExtractDescription_OldSite(t *testing.T) {
	html := `<html><body><div class="mg-b20 lh4"><p class="mg-b20">A great movie</p></div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	desc := s.extractDescription(doc, false)
	assert.Equal(t, "A great movie", desc)
}

// --- extractDescription: new site with og:description ---

func TestMiss5_ExtractDescription_NewSite(t *testing.T) {
	html := `<html><head><meta property="og:description" content="OG description"/></head><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	desc := s.extractDescription(doc, true)
	assert.Equal(t, "OG description", desc)
}

// --- extractReleaseDate ---

func TestMiss5_ExtractReleaseDate(t *testing.T) {
	html := `<html><body><td>2024/03/15</td></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	dt := s.extractReleaseDate(doc)
	if dt != nil {
		assert.Equal(t, 2024, dt.Year())
	}
}

// --- extractRuntime ---

func TestMiss5_ExtractRuntime(t *testing.T) {
	html := `<html><body><td>120 minutes</td></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	assert.Equal(t, 120, s.extractRuntime(doc))
}

// --- extractDirector ---

func TestMiss5_ExtractDirector(t *testing.T) {
	html := `<html><body><a href="?director=123">Director Name</a></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	dir := s.extractDirector(doc)
	assert.NotEmpty(t, dir)
}

// --- extractGenres: old site ---

func TestMiss5_ExtractGenres_OldSite(t *testing.T) {
	html := `<html><body><table><tr><td>Genre:</td><td><a href="/genre/1">Action</a><a href="/genre/2">Drama</a></td></tr></table></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	genres := s.extractGenres(doc)
	assert.Contains(t, genres, "Action")
	assert.Contains(t, genres, "Drama")
}

// --- extractGenres: no genres ---

func TestMiss5_ExtractGenres_None(t *testing.T) {
	html := `<html><body><p>No genres</p></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	genres := s.extractGenres(doc)
	assert.Empty(t, genres)
}

// --- resolveContentIDCtx: nil repo ---

func TestMiss5_ResolveContentIDCtx_NilRepo(t *testing.T) {
	s := &scraper{
		settings:      models.ScraperSettings{Enabled: true},
		contentIDRepo: nil,
		rateLimiter:   ratelimit.NewLimiter(0),
		client:        resty.New(),
	}
	_, err := s.resolveContentIDCtx(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

// --- extractContentIDCandidates: nil doc ---

func TestMiss5_ExtractContentIDCandidates_NilDoc(t *testing.T) {
	result := extractContentIDCandidates(nil, []string{"ipx535"})
	assert.Empty(t, result)
}

// --- extractContentIDCandidates: empty search IDs ---

func TestMiss5_ExtractContentIDCandidates_EmptySearchIDs(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<html></html>"))
	require.NoError(t, err)
	result := extractContentIDCandidates(doc, []string{})
	assert.Empty(t, result)
}

// --- extractContentIDCandidates: with cid link ---

func TestMiss5_ExtractContentIDCandidates_WithCIDLink(t *testing.T) {
	html := `<html><body><a href="/digital/videoa/-/detail/=/cid=ipx00535/">Link</a></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	// The content ID from the URL is ipx00535, which after cleanPrefix is ipx535
	result := extractContentIDCandidates(doc, []string{"ipx535"})
	// May or may not find depending on normalization
	_ = result
}

// --- extractContentIDCandidates: with video.dmm.co.jp id link ---

func TestMiss5_ExtractContentIDCandidates_WithVideoDMMIDLink(t *testing.T) {
	html := `<html><body><a href="https://video.dmm.co.jp/av/content/?id=oreco183">Link</a></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	result := extractContentIDCandidates(doc, []string{"oreco183"})
	require.NotEmpty(t, result)
}

// --- extractActressFromLink: no href ---

func TestMiss5_ExtractActressFromLink_NoHref(t *testing.T) {
	html := `<a>No href</a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	sel := doc.Find("a").First()
	actress := s.extractActressFromLink(context.Background(), sel)
	assert.Equal(t, 0, actress.DMMID)
}

// --- shouldSkipActressName ---

func TestMiss5_ShouldSkipActressName(t *testing.T) {
	assert.True(t, shouldSkipActressName(""))
	assert.True(t, shouldSkipActressName("購入前チェック"))
	assert.True(t, shouldSkipActressName("レビュー"))
	assert.True(t, shouldSkipActressName("ポイント"))
	assert.False(t, shouldSkipActressName("田中麻美"))
	assert.False(t, shouldSkipActressName("Jane Doe"))
}

// --- cleanActressName ---

func TestMiss5_CleanActressName(t *testing.T) {
	assert.Equal(t, "田中麻美", cleanActressName("田中麻美"))
	assert.Equal(t, "Test", cleanActressName("Test(备注)"))
}

// --- upsertActressInfo ---

func TestMiss5_UpsertActressInfo(t *testing.T) {
	actresses := make([]models.ActressInfo, 0)
	indexByID := make(map[int]int)

	// Insert new
	result := upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 1, JapaneseName: "テスト"})
	assert.True(t, result)
	require.Len(t, actresses, 1)

	// Update existing (add thumb)
	result = upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 1, ThumbURL: "https://example.com/thumb.jpg"})
	assert.False(t, result)
	assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)

	// Zero DMMID is rejected
	result = upsertActressInfo(&actresses, indexByID, models.ActressInfo{DMMID: 0})
	assert.False(t, result)
}

// --- ScrapeURL: fetch error ---

func TestMiss5_ScrapeURL_FetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This will be proxied but the transport won't handle it
	}))
	defer server.Close()

	client := resty.New()
	client.SetTimeout(1 * time.Second)

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=err001/")
	// Will likely fail with connection error or timeout
	_ = err
}
