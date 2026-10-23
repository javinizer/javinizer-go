package dmm

import (
	"context"
	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
	"html"
	"strings"
	"testing"
	"time"
)

func boundaryDocument(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	return doc
}

func TestOfflineLegacyTrailerBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, onclick, want string }{
		{"https", `video_url: 'https://media.example/sample.mp4'`, "https://media.example/sample.mp4"},
		{"http", `video_url: 'http://media.example/sample.mp4'`, "http://media.example/sample.mp4"},
		{"escaped quote", `video_url: &quot;https://media.example/sample.mp4\&quot; trailing`, "https://media.example/sample.mp4"},
		{"no terminator", `video_url: https://media.example/sample.mp4`, "https://media.example/sample.mp4"},
		{"missing scheme", `video_url: sample.mp4`, ""},
		{"unrelated", `play('https://media.example/sample.mp4')`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := boundaryDocument(t, `<a class="fn-sampleVideoBtn"></a><a class="fn-sampleVideoBtn" onclick="`+html.EscapeString(tc.onclick)+`"></a><a class="fn-sampleVideoBtn"></a>`)
			require.Equal(t, tc.want, (&scraper{}).extractTrailerURL(doc, "https://www.dmm.co.jp/mono/dvd/"))
		})
	}
}

func TestOfflineStructuredMetadataFallbacks(t *testing.T) {
	doc := boundaryDocument(t, `<meta property="og:title" content="Fallback title"><table><tr><th>メーカー</th><td><a>Fallback maker</a></td></tr><tr><td>2024/02/29 120分</td></tr></table><script type="application/ld+json">{"aggregateRating":{"ratingValue":4,"ratingCount":7}}</script>`)
	s := &scraper{}
	result := &models.ScraperResult{}
	s.extractTextualMetadata(result, doc, true, nil)
	require.Equal(t, "Fallback title", result.Title)
	require.Equal(t, result.Title, result.OriginalTitle)
	require.Equal(t, "Fallback maker", result.Maker)
	s.extractStructuredData(context.Background(), result, doc, "https://video.dmm.co.jp/av/content/?id=abc001", true, nil)
	require.NotNil(t, result.ReleaseDate)
	require.Equal(t, "2024-02-29", result.ReleaseDate.Format("2006-01-02"))
	require.Equal(t, 120, result.Runtime)
	require.Equal(t, &models.Rating{Score: 8, Votes: 7}, result.Rating)
	require.Empty(t, result.Actresses)
	legacy := &models.ScraperResult{}
	s.extractStructuredData(context.Background(), legacy, doc, "https://www.dmm.co.jp/mono/dvd/", false, nil)
	require.Equal(t, result.ReleaseDate, legacy.ReleaseDate)
}

func TestOfflineStreamingTrailerAndMalformedRatings(t *testing.T) {
	s := &scraper{}
	doc := boundaryDocument(t, `<video><source src="https://media.example/first.mp4"><source src="https://media.example/second.mp4"></video>`)
	require.Equal(t, "https://media.example/first.mp4", s.extractTrailerURLNewSite(doc))
	doc = boundaryDocument(t, `<video data-src="https://media.example/first.mp4"></video><video src="https://media.example/second.mp4"></video>`)
	require.Equal(t, "https://media.example/first.mp4", s.extractTrailerURLNewSite(doc))
	doc = boundaryDocument(t, `<script type="application/ld+json">invalid</script><script type="application/ld+json">{"aggregateRating":"invalid"}</script><script type="application/ld+json">{"aggregateRating":{"ratingValue":3,"ratingCount":2}}</script>`)
	rating, votes := s.extractRatingNewSite(doc)
	require.Equal(t, 6.0, rating)
	require.Equal(t, 2, votes)
}

func TestOfflineInvalidConfigurationAndEmptyCSS(t *testing.T) {
	require.Error(t, validateScraperSettings(&models.ScraperSettings{BaseURL: "https://untrusted.example/"}))
	for _, style := range []string{"background-image: url(", "background-image: url('')", "background-image: url(   )"} {
		require.Empty(t, extractBackgroundImageURL(style))
	}
	doc := boundaryDocument(t, `<a>no href</a><a href="cid=abc001">not an absolute or root-relative URL</a><a href="/mono/dvd/-/detail/=/cid=abc001/">valid</a>`)
	candidates := extractContentIDCandidates(doc, []string{"abc001"})
	require.NotEmpty(t, candidates)
	for _, candidate := range candidates {
		require.Equal(t, "abc001", candidate.contentID)
	}
	urls := (&scraper{}).extractCandidateURLs(doc, "abc001")
	require.Len(t, urls, 1)
	require.Equal(t, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abc001/", urls[0].url)
}

func TestOfflineActressLookupCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour)}
	require.Empty(t, s.extractRomajiVariantsFromActressPageCtx(ctx, 12345))
}
