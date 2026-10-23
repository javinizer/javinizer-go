package fc2

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRatingSkipsMalformedAndIncompleteStructuredData(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<script type="application/ld+json"> </script><script type="application/ld+json">broken</script><script type="application/ld+json">{}</script><script type="application/ld+json">{"aggregateRating":[]}</script><script type="application/ld+json">{"aggregateRating":{"ratingValue":0,"reviewCount":0}}</script><script type="application/ld+json">{"aggregateRating":{"ratingValue":4.5,"reviewCount":12}}</script><script type="application/ld+json">{"aggregateRating":{"ratingValue":1,"reviewCount":2}}</script>`))
	require.NoError(t, err)
	require.Equal(t, &models.Rating{Score: 4.5, Votes: 12}, extractRating(doc))
	require.Empty(t, normalizeURL("http://%", "https://fc2.com"))
	require.Empty(t, normalizeURL("/image.jpg", "http://%"))
}

func TestInfoAndScreenshotFiltering(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div class="items_article_softDevice"><p>unrelated</p><p>Runtime</p></div><div class="items_article_SampleImagesArea"><a href=""></a><a href="/one.jpg"></a><a href="/one.jpg"></a></div>`))
	require.NoError(t, err)
	require.Empty(t, extractInfoValue(doc, " "))
	require.Equal(t, "Runtime", extractInfoValue(doc, "Runtime"))
	require.Equal(t, []string{"https://fc2.com/one.jpg"}, extractScreenshotURLs(doc, "https://fc2.com"))
}
