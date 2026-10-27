package jav321

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestParseHTMLActressInputBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		section string
		want    []models.ActressInfo
	}{
		{
			name:    "delimiter-only names do not create empty actresses",
			section: ` , / | ・ 、 `,
			want:    []models.ActressInfo{},
		},
		{
			name:    "overlong name does not discard a valid neighbor",
			section: `<a href="/actor/long">` + strings.Repeat("x", 81) + `</a> <a href="/actor/alice">Alice Smith</a>`,
			want:    []models.ActressInfo{{FirstName: "Alice", LastName: "Smith"}},
		},
		{
			name:    "blank and duplicate anchors preserve distinct names",
			section: `<a href="/actor/empty"> </a><a href="/actor/alice">Alice Smith</a><a href="/actor/alice-again">Alice Smith</a><a href="/actor/bob">Bob Jones</a>`,
			want:    []models.ActressInfo{{FirstName: "Alice", LastName: "Smith"}, {FirstName: "Bob", LastName: "Jones"}},
		},
		{
			name:    "duplicate plaintext names preserve distinct names",
			section: `Alice / Alice / Bob`,
			want:    []models.ActressInfo{{FirstName: "Alice"}, {FirstName: "Bob"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const sourceURL = "https://jp.jav321.com/video/abc00123"
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head><title>Fixture title</title></head><body><b>品番</b>: ABC-123<br><b>出演者</b>: ` + tc.section + `<br></body></html>`))
			require.NoError(t, err)
			result := ParseHTML(doc, sourceURL)
			require.Equal(t, "ABC-123", result.ID)
			require.Equal(t, "Fixture title", result.Title)
			require.Equal(t, sourceURL, result.SourceURL)
			require.Equal(t, tc.want, result.Actresses)
		})
	}
}
