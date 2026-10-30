package javdb

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

// ParseHTML remains a two-result public parsing seam even though its concrete
// detail parser cannot return an error. Sparse markup is data, not a parse error.
func TestCleanupParseHTMLPreservesSparseAndPublishedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, html, id, title, cover string
	}{
		{name: "empty", html: `<html><body></body></html>`},
		{name: "published ID only", html: `<h2 class="title is-4"><strong>IPX-123</strong></h2>`, id: "IPX-123", title: "IPX-123"},
		{name: "complete", html: `<h2 class="title is-4"><strong>IPX-123</strong> Detail title</h2><div class="column-video-cover"><img class="video-cover" src="/cover.jpg"></div>`, id: "IPX-123", title: "Detail title", cover: "https://javdb.example/cover.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			require.NoError(t, err)
			s := &scraper{baseURL: "https://javdb.example"}
			got, err := s.ParseHTML(doc, "https://javdb.example/v/abc123")
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, "javdb", got.Source)
			require.Equal(t, "https://javdb.example/v/abc123", got.SourceURL)
			require.Equal(t, "ja", got.Language)
			require.Equal(t, tc.id, got.ID)
			require.Equal(t, tc.id, got.ContentID)
			require.Equal(t, tc.title, got.Title)
			require.Equal(t, tc.title, got.OriginalTitle)
			require.Equal(t, tc.cover, got.CoverURL)
			require.Equal(t, tc.cover, got.PosterURL)
			require.True(t, got.ShouldCropPoster)
		})
	}
}
