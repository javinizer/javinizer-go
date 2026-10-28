package aventertainment

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func TestMissingHTMLCandidateIdentityUsesMatchingURL(t *testing.T) {
	const base = "https://www.aventertainments.com"
	for _, tc := range []struct {
		name, requestedID, expectedID, language, culture, lang string
	}{
		{"Caribbean", "carib_020326_001", "CARIB-020326-001", "en", "en-US", "1"},
		{"compact", "IPX123", "IPX123", "ja", "ja-JP", "2"},
	} {
		for _, entrypoint := range []string{"GetURL", "Search"} {
			t.Run(tc.name+"/"+entrypoint, func(t *testing.T) {
				settings := models.ScraperSettings{Enabled: true, Language: tc.language, BaseURL: base}
				s := newScraper(&settings, nil, models.FlareSolverrConfig{})
				wrongURL := base + "/ppv/detail/ABC-999/"
				matchingURL := base + "/ppv/detail/" + tc.requestedID + "/"
				resolvedURL := matchingURL + "?culture=" + tc.culture + "&lang=" + tc.lang + "&v=1"
				searchURL := base + "/ppv/search?culture=" + tc.culture + "&keyword=" + tc.requestedID + "&lang=" + tc.lang
				searchWithMode := searchURL + "&searchby=keyword&v=1"
				searchWithoutMode := searchURL + "&v=1"
				// Both search endpoints repeat the same links. The first result
				// is unrelated, and neither detail body supplies a product ID.
				searchHTML := `<a href="` + wrongURL + `">Unrelated</a><a href="` + matchingURL + `">Matching</a>`
				responses := map[string]string{
					searchWithMode:    searchHTML,
					searchWithoutMode: searchHTML,
					wrongURL:          `<title>Unrelated movie - AV Entertainment</title>`,
					matchingURL:       `<title>Matching movie - AV Entertainment</title>`,
					resolvedURL:       `<title>Matching movie - AV Entertainment</title>`,
				}
				var requests []string
				s.client.SetTransport(currentScopeAVETransport(func(req *http.Request) (*http.Response, error) {
					requests = append(requests, req.Method+" "+req.URL.String())
					body, ok := responses[req.URL.String()]
					if !ok || req.Method != http.MethodGet {
						t.Errorf("unexpected request: %s %s", req.Method, req.URL)
						return nil, fmt.Errorf("unexpected fixture request")
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
						Body:       io.NopCloser(strings.NewReader(body)),
						Request:    req,
					}, nil
				}))

				expectedRequests := []string{
					"GET " + searchWithMode, "GET " + searchWithoutMode,
					"GET " + wrongURL, "GET " + matchingURL,
				}
				if entrypoint == "GetURL" {
					got, err := s.GetURL(context.Background(), tc.requestedID)
					require.NoError(t, err)
					require.Equal(t, resolvedURL, got, "missing labels must not fall back to the first unrelated result")
				} else {
					got, err := s.Search(context.Background(), tc.requestedID)
					require.NoError(t, err)
					require.NotNil(t, got)
					require.Equal(t, tc.expectedID, got.ID)
					require.Equal(t, tc.expectedID, got.ContentID)
					require.Equal(t, "aventertainment", got.Source)
					require.Equal(t, tc.language, got.Language)
					require.Equal(t, resolvedURL, got.SourceURL)
					require.Equal(t, "Matching movie", got.Title)
					require.Equal(t, "Matching movie", got.OriginalTitle)
					expectedRequests = append(expectedRequests, "GET "+resolvedURL)
				}
				// Exact requests also verify keyword/language query values, no
				// unexpected host/path, and only one inspection per candidate.
				require.Equal(t, expectedRequests, requests)
			})
		}
	}
}

func TestMissingHTMLAdapterPreservesScriptCover(t *testing.T) {
	const sourceURL = "https://www.aventertainments.com/ppv/detail/IPX123/"
	const coverURL = "https://www.aventertainments.com/vodimages/gallery/large/ipx123/cover.jpg"
	const rawHTML = `<html><head><title>Script cover movie - AV Entertainment</title></head><body>
<span class="tag-title">IPX123</span>
<div class="product-description">Description remains intact.</div>
<script>const coverMarkup = "<a class='lightbox' href='` + coverURL + `'>Cover</a>";</script>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	require.NoError(t, err)
	require.Zero(t, doc.Find("a.lightbox, #PlayerCover img, meta[property='og:image']").Length(), "cover exists only in script text, not the DOM")
	settings := models.ScraperSettings{Language: "ja", ScrapeBonusScreens: true}
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	s.client.SetTransport(currentScopeAVETransport(func(*http.Request) (*http.Response, error) {
		t.Error("HTML parsing must not fetch media or pages")
		return nil, fmt.Errorf("unexpected fixture request")
	}))
	var parser models.HTMLParser = s
	got, err := parser.ParseHTML(doc, sourceURL)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "IPX123", got.ID)
	require.Equal(t, "IPX123", got.ContentID)
	require.Equal(t, "aventertainment", got.Source)
	require.Equal(t, sourceURL, got.SourceURL)
	require.Equal(t, "ja", got.Language)
	require.Equal(t, "Script cover movie", got.Title)
	require.Equal(t, "Script cover movie", got.OriginalTitle)
	require.Equal(t, "Description remains intact.", got.Description)
	require.Equal(t, coverURL, got.CoverURL, "retain the original absolute URL rather than resolving a raw path relative to the detail page")
	require.Equal(t, coverURL, got.PosterURL)
	require.True(t, got.ShouldCropPoster)
	require.Empty(t, got.ScreenshotURL, "the original cover is not a bonus screenshot")
}

func TestMissingHTMLAdapterRejectsUnrenderableDocument(t *testing.T) {
	// This public adapter accepts caller-owned DOMs, which can contain nodes
	// that the HTTP HTML parser would never produce.
	root := &html.Node{Type: html.DocumentNode}
	invalid := &html.Node{Type: html.ErrorNode}
	root.AppendChild(invalid)
	doc := goquery.NewDocumentFromNode(root)
	settings := models.ScraperSettings{Language: "en"}
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	s.client.SetTransport(currentScopeAVETransport(func(*http.Request) (*http.Response, error) {
		t.Error("a rejected document must not trigger HTTP")
		return nil, fmt.Errorf("unexpected fixture request")
	}))
	var parser models.HTMLParser = s
	got, err := parser.ParseHTML(doc, "https://www.aventertainments.com/ppv/detail/IPX123/")
	require.Nil(t, got, "unrenderable input must not yield a partial result")
	require.ErrorContains(t, err, "aventertainment: failed to render document to HTML")
	require.Same(t, invalid, root.FirstChild, "the caller-owned DOM must remain unchanged")
	require.Same(t, root, invalid.Parent)
	require.Equal(t, html.ErrorNode, invalid.Type)
}
