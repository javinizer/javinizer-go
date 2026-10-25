package dmm

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScrapeURLStreamingMetadataContract(t *testing.T) {
	const sourceURL = "https://video.dmm.co.jp/av/content/?id=abc00123"
	for _, tc := range []struct {
		name, metadata, title, description, maker string
		structured                                bool
	}{
		{"structured metadata wins", `<script type="application/ld+json">{"@type":"Product","name":"Structured title","description":"Structured description","brand":{"name":"Structured maker"},"image":"https://fixtures.invalid/structured.jpg","subjectOf":{"uploadDate":"2025-02-03","genre":["Drama","Documentary"],"contentUrl":"https://fixtures.invalid/trailer.mp4"},"aggregateRating":{"ratingValue":4.25,"ratingCount":17}}</script>`, "Structured title", "Structured description", "Structured maker", true},
		{"HTML fallback without product metadata", ``, "HTML title", "HTML description", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestDMMScraper(t)
			body := `<html><head><meta property="og:title" content="Meta title"><meta property="og:description" content="HTML description"><meta property="og:image" content="https://fixtures.invalid/html.jpg">` + tc.metadata + `</head><body><h1>HTML title</h1></body></html>`
			var requests []string
			s.client.SetTransport(dmmFixtureTransport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.Method+" "+r.URL.String())
				if r.Method != http.MethodGet || r.URL.String() != sourceURL {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					return nil, fmt.Errorf("unexpected request")
				}
				return dmmFixtureResponse(r, http.StatusOK, body), nil
			}))
			result, err := s.ScrapeURL(context.Background(), sourceURL)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, []string{"GET " + sourceURL}, requests)
			require.Equal(t, "ABC-123", result.ID)
			require.Equal(t, "abc00123", result.ContentID)
			require.Equal(t, sourceURL, result.SourceURL)
			require.Equal(t, "dmm", result.Source)
			require.Equal(t, "ja", result.Language)
			require.Equal(t, tc.title, result.Title)
			require.Equal(t, tc.title, result.OriginalTitle)
			require.Equal(t, tc.description, result.Description)
			require.Equal(t, tc.maker, result.Maker)
			require.Empty(t, result.Actresses)
			require.Empty(t, result.ScreenshotURL)
			if tc.structured {
				require.Equal(t, "https://fixtures.invalid/structured.jpg", result.CoverURL)
				require.Equal(t, "https://fixtures.invalid/trailer.mp4", result.TrailerURL)
				require.NotNil(t, result.ReleaseDate)
				require.True(t, result.ReleaseDate.Equal(time.Date(2025, 2, 3, 0, 0, 0, 0, time.UTC)))
				require.Equal(t, []string{"Drama", "Documentary"}, result.Genres)
				require.NotNil(t, result.Rating)
				require.Equal(t, 8.5, result.Rating.Score)
				require.Equal(t, 17, result.Rating.Votes)
			} else {
				require.Equal(t, "https://fixtures.invalid/html.jpg", result.CoverURL)
				require.Empty(t, result.TrailerURL)
				require.Nil(t, result.ReleaseDate)
				require.Empty(t, result.Genres)
				require.Nil(t, result.Rating)
			}
			require.Equal(t, result.CoverURL, result.PosterURL)
			require.True(t, result.ShouldCropPoster)
		})
	}
}
