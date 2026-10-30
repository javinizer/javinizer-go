package dlgetchu

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanupCaptureExtractionPreservesContentContracts(t *testing.T) {
	// The extraction loops require capture 1, not a particular HTML input.
	// Fail clearly if a future pattern edit removes that capture.
	require.GreaterOrEqual(t, genreRegex.NumSubexp(), 1)
	require.GreaterOrEqual(t, screenshotRegex.NumSubexp(), 1)

	for _, html := range []string{"", `<a href="?genre_id=bad">ignored</a><img src="/data/item_img/no.gif" class="highslide">`} {
		genres := extractGenres(html)
		require.NotNil(t, genres)
		require.Empty(t, genres)
		screenshots := extractScreenshots(html, "https://dl.getchu.com/i/item1234")
		require.NotNil(t, screenshots)
		require.Empty(t, screenshots)
	}

	html := `<a href="?genre_id=1">  First &amp; Second  </a>
<a href="?genre_id=2">   </a>
<a href="?genre_id=3">First &amp; Second</a>
<a href="?genre_id=4">Last</a>
<img src="/data/item_img/1234/one.JPG" class="highslide">
<img src="/data/item_img/1234/two.webp" class="highslide">
<img src="/data/item_img/1234/one.JPG" class="highslide">`
	require.Equal(t, []string{"First &amp; Second", "Last"}, extractGenres(html))
	require.Equal(t, []string{
		"https://dl.getchu.com/data/item_img/1234/one.JPG",
		"https://dl.getchu.com/data/item_img/1234/two.webp",
	}, extractScreenshots(html, "https://dl.getchu.com/i/item1234"))
}
