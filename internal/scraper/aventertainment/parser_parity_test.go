package aventertainment

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

func TestScreenshotCollectorDeduplicatesResolvedURLs(t *testing.T) {
	const base = "https://example.invalid/detail/"
	const content = `<a class="lightbox" href=" "></a>
<a class="lightbox" href="/vodimages/screenshot/large/first.webp"></a>
<a class="lightbox" href="https://example.invalid/vodimages/screenshot/large/first.webp"></a>
<a class="lightbox" href="/vodimages/screenshot/large/second.jpg"></a>
<img src="/vodimages/gallery/large/fixture/001.webp">
<img src="https://example.invalid/vodimages/gallery/large/fixture/001.webp">`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	require.NoError(t, err)
	standard := []string{
		"https://example.invalid/vodimages/screenshot/large/first.webp",
		"https://example.invalid/vodimages/screenshot/large/second.jpg",
	}
	require.Equal(t, standard, extractScreenshotURLs(doc, content, base, false))
	require.Equal(t, append(standard, "https://example.invalid/vodimages/gallery/large/fixture/001.webp"),
		extractScreenshotURLs(doc, content, base, true))
}
