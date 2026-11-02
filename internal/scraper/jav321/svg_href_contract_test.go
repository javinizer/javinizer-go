package jav321_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/jav321"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

// SVG anchors can carry both href and xlink:href. These distinct raw names
// survive tokenization; foreign-content parsing assigns both the local key href.
// CSS selection may match xlink:href while Attr reads the earlier empty href.
// Such a node is not an actionable product link and must not hide later results.
func TestRegisteredJav321IgnoresEmptyEffectiveHrefInSVGAnchors(t *testing.T) {
	for _, first := range []string{"", " \t "} {
		t.Run(fmt.Sprintf("first=%q", first), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/search" {
					http.Error(w, "unexpected fixture request", http.StatusBadRequest)
					return
				}
				if err := r.ParseForm(); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				switch r.PostForm.Get("sn") {
				case "ABC-001":
					_, _ = fmt.Fprintf(w, `<html><body><svg><a href="%s" xlink:href="/video/poison">ABC-001</a></svg><div><a href="/video/valid">ABC-001</a></div></body></html>`, first)
				case "DEF-002":
					_, _ = fmt.Fprintf(w, `<html><body><svg><a href="%s" xlink:href="/video/poison">DEF-002</a></svg></body></html>`, first)
				default:
					http.Error(w, "unexpected product identifier", http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			registry := scraperutil.NewScraperRegistry()
			t.Cleanup(func() { require.NoError(t, registry.Close()) })
			jav321.Register(registry)
			constructor, ok := registry.GetScraperConstructor("jav321")
			require.True(t, ok)
			settings := models.ScraperSettings{Enabled: true, BaseURL: server.URL, Timeout: 5}
			require.NoError(t, config.ValidateHTTPBaseURL("jav321.base_url", settings.BaseURL))
			before := settings.Clone()
			scraper, err := constructor(scraperutil.ScraperDeps{Settings: settings})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, scraper.Close()) })
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			got, err := scraper.GetURL(ctx, "ABC-001")
			require.NoError(t, err)
			require.Equal(t, server.URL+"/video/valid", got, "skip the unusable SVG anchor and continue to a genuine link")
			got, err = scraper.GetURL(ctx, "DEF-002")
			require.Empty(t, got)
			var notFound *models.ScraperError
			require.ErrorAs(t, err, &notFound)
			require.Equal(t, models.ScraperErrorKindNotFound, notFound.Kind)
			require.Equal(t, "Jav321", notFound.Scraper)
			require.False(t, notFound.Retryable)
			require.False(t, notFound.Temporary)
			require.Equal(t, int32(2), requests.Load(), "only owned search requests; no detail fetch or redirect")
			cancelled, stop := context.WithCancel(ctx)
			stop()
			got, err = scraper.GetURL(cancelled, "ABC-001")
			require.Empty(t, got)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, int32(2), requests.Load(), "cancelled request must not reach the server")
			got, err = scraper.GetURL(ctx, "ABC-001")
			require.NoError(t, err)
			require.Equal(t, server.URL+"/video/valid", got, "prior malformed response or cancelled request must not poison later resolution")
			require.Equal(t, int32(3), requests.Load())
			require.Equal(t, before, settings)
			require.Equal(t, before, *scraper.Config())
		})
	}
}
