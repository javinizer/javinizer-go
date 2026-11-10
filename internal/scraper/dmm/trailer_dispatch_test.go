package dmm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractTrailerURLUsesSourceSiteMarkup(t *testing.T) {
	// Both page generations can contain promotional video markup. Select the
	// relevant extractor using the detail page's source, not DOM order.
	doc := boundaryDocument(t, `<a class="fn-sampleVideoBtn" onclick="video_url: 'https://media.example/legacy.mp4'"></a><video><source src="https://media.example/streaming.mp4"></video>`)
	for _, tc := range []struct {
		name, sourceURL, want string
	}{
		{"legacy", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abc001/", "https://media.example/legacy.mp4"},
		{"streaming", "https://video.dmm.co.jp/av/content/?id=abc001", "https://media.example/streaming.mp4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, (&scraper{}).extractTrailerURL(doc, tc.sourceURL))
		})
	}

	require.Empty(t, (&scraper{}).extractTrailerURL(boundaryDocument(t, `<p>No trailer</p>`), "https://video.dmm.co.jp/av/content/?id=abc001"))
}
