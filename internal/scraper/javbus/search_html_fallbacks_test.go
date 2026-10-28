package javbus

import (
	"context"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestSearch_OpaqueResultAndCoverFallbacks(t *testing.T) {
	const (
		baseURL       = "https://www.javbus.org"
		searchURL     = baseURL + "/search/ABC-123&type=0&parent=uc"
		uncensoredURL = baseURL + "/uncensored/search/ABC-123&type=0&parent=uc"
		detailURL     = baseURL + "/ja/detail/opaque-fixture"
		// No candidate field contains the requested ID: only the unique-link
		// fallback can resolve this result, and the detail page supplies its ID.
		uniqueResult = `<a class="movie-box" href="/detail/opaque-fixture" title="Fixture result"></a>`
	)

	for _, tt := range []struct {
		name         string
		searchHTML   string
		coverHTML    string
		wantCover    string
		wantNotFound bool
	}{
		{
			name:       "blank preferred attributes use later cover",
			searchHTML: uniqueResult,
			coverHTML: `<a class="bigImage" href=" "><img src=" "></a>
<div id="cover"><img src="/fixtures/cover.png"></div>`,
			wantCover: baseURL + "/fixtures/cover.png",
		},
		{
			name:       "nonblank preferred cover keeps precedence",
			searchHTML: uniqueResult,
			coverHTML: `<a class="bigImage" href="/fixtures/preferred.png"><img src="/fixtures/thumbnail.png"></a>
<div id="cover"><img src="/fixtures/cover.png"></div>`,
			wantCover: baseURL + "/fixtures/preferred.png",
		},
		{
			name: "ambiguous opaque results do not fetch details or media",
			searchHTML: `<a class="movie-box" href="/detail/first-fixture" title="First fixture"></a>
<a class="movie-box" href="/detail/second-fixture" title="Second fixture"></a>`,
			wantNotFound: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			settings := models.ScraperSettings{
				Enabled: true, Language: "ja", BaseURL: baseURL, RateLimit: 0, RetryCount: 0,
			}
			require.NoError(t, settings.Validate("javbus"))
			require.NoError(t, validateScraperSettings(&settings))
			s := newScraper(&settings, nil, models.FlareSolverrConfig{})
			t.Cleanup(func() { require.NoError(t, s.Close()) })

			responses := map[string]string{searchURL: tt.searchHTML}
			wantRequests := []string{searchURL, detailURL}
			if tt.wantNotFound {
				responses[uncensoredURL] = tt.searchHTML
				wantRequests = []string{searchURL, uncensoredURL}
			} else {
				responses[detailURL] = `<html><head><title>ABC-123 Quiet River - JavBus</title></head><body>` + tt.coverHTML + `</body></html>`
			}
			// This owned exact-route fixture never dials. Unconfigured requests
			// return 404 and remain visible in the complete URL/method ledgers.
			transport := &javbusRoundTripper{responses: responses}
			var methods []string
			s.client.SetTransport(javbusFixtureRoundTripper(func(req *http.Request) (*http.Response, error) {
				methods = append(methods, req.Method)
				return transport.RoundTrip(req)
			}))

			result, err := s.Search(context.Background(), "ABC-123")
			require.Equal(t, wantRequests, transport.lastReqs, "no extra search, detail, or media requests")
			require.Equal(t, []string{http.MethodGet, http.MethodGet}, methods)
			if tt.wantNotFound {
				require.Nil(t, result)
				require.Error(t, err)
				scraperErr, ok := models.AsScraperError(err)
				require.True(t, ok, "expected typed scraper not-found, got %v", err)
				require.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "ABC-123", result.ID)
			require.Equal(t, "ABC-123", result.ContentID)
			require.Equal(t, "Quiet River", result.Title)
			require.Equal(t, "Quiet River", result.OriginalTitle)
			require.Equal(t, "javbus", result.Source)
			require.Equal(t, "ja", result.Language)
			require.Equal(t, detailURL, result.SourceURL)
			require.Equal(t, tt.wantCover, result.CoverURL)
			require.Equal(t, tt.wantCover, result.PosterURL)
			require.True(t, result.ShouldCropPoster)
			require.Empty(t, result.ScreenshotURL)
			require.Empty(t, result.TrailerURL)
		})
	}
}
