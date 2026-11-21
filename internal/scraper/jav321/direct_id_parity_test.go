package jav321

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestDirectVideoIDMatchesPublicExtraction(t *testing.T) {
	const root = "https://jp.jav321.com"
	for _, tc := range []struct {
		name, path, wantID string
		direct             bool
	}{
		{"path wins over query", "/video/ab-001/?sn=search-999", "AB-001", true},
		{"decoded path retained", "/video/ab%2F%E5%BD%B1///", "AB/影", true},
		{"case-sensitive path falls back to search ID", "/Video/ab-001?sn=search-999", "SEARCH-999", false},
		{"empty path falls back to search ID", "/video/%2F?sn=search-999", "SEARCH-999", false},
		{"search URL is not a direct page", "/search?sn=search-999", "SEARCH-999", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := root + tc.path
			var requests []string
			client := resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"text/html; charset=UTF-8"}},
					Body:       io.NopCloser(strings.NewReader("<html><title>Synthetic title</title></html>")),
					Request:    req,
				}, nil
			}))
			s := &scraper{client: client, rateLimiter: ratelimit.NewLimiter(0)}
			id, err := s.ExtractIDFromURL(raw)
			require.NoError(t, err)
			require.Equal(t, tc.wantID, id)
			got, err := s.ScrapeURL(t.Context(), raw)
			if !tc.direct {
				require.Nil(t, got)
				var notFound *models.ScraperError
				require.ErrorAs(t, err, &notFound)
				require.Equal(t, models.ScraperErrorKindNotFound, notFound.Kind)
				require.Equal(t, "URL is not a direct video page", notFound.Message)
				require.Empty(t, requests)
				return
			}
			require.NoError(t, err)
			require.Equal(t, id, got.ID)
			require.Equal(t, id, got.ContentID)
			require.Equal(t, raw, got.SourceURL)
			require.Equal(t, "Synthetic title", got.Title)
			require.Equal(t, []string{raw}, requests)
		})
	}
}
