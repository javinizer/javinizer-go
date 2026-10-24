package javlibrary

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

func TestParseHTMLAdapterIdentityContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		url    string
		pageID string
		wantID string
	}{
		{"page overrides slug", "https://www.javlibrary.com/en/URL-999/?v=opaque", "ABC-123", "ABC-123"},
		{"page survives malformed URL", "https://www.javlibrary.com/%zz", "ABC-123", "ABC-123"},
		{"last nonempty path segment", "https://www.javlibrary.com/en/ABC-123///?v=ignored#fragment", "", "ABC-123"},
		{"relative path", "/en/ABC-123/", "", "ABC-123"},
		{"root has no fallback ID", "https://www.javlibrary.com////?v=opaque", "", ""},
		{"query is not a product ID", "https://www.javlibrary.com?v=ABC-123", "", ""},
		{"malformed URL has no fallback ID", "https://www.javlibrary.com/%zz", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `<html><head><title>Adapter title - JAVLibrary</title></head><body>`
			if tc.pageID != "" {
				body += `<div id="video_id"><table><tr><td class="text">` + tc.pageID + `</td></tr></table></div>`
			}
			body += `</body></html>`
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
			require.NoError(t, err)
			s := &scraper{language: "ja"}
			result, err := s.ParseHTML(doc, tc.url)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.wantID, result.ID)
			require.Equal(t, tc.wantID, result.ContentID)
			require.Equal(t, tc.url, result.SourceURL)
			require.Equal(t, "ja", result.Language)
			require.Equal(t, "javlibrary", result.Source)
			require.Equal(t, "Adapter title", result.Title)
			require.Empty(t, result.CoverURL)
		})
	}
}
