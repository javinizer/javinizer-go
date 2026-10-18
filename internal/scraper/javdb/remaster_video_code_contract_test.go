package javdb

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A video slug is not a catalog ID. An explicit URL preserves that intent
// through the normal source dispatcher, even if its slug resembles a raw CID
// or a digit-led remaster display. No database or aggregation is involved.
func TestMarkerShapedVideoCodeExplicitURLContract(t *testing.T) {
	for _, code := range []string{"5aB3h", "5ab3h", "5AB3H", "5aB3HD", "5aB3AI", "dv00899ai", "300MIUM700H", "RCT156H", "RCT156HD", "RCT156AI", "IPX535EH", "IPX535ZAI", "FC2PPV123H"} {
		t.Run(code, func(t *testing.T) {
			ctx := context.Background()
			pageURL := "https://javdb.test/v/" + code
			searchURL := "https://javdb.test/search?q=" + url.QueryEscape(code) + "&f=all"
			transport := &videoCodeContractTransport{staticRoundTripper: staticRoundTripper{responses: map[string]string{
				pageURL:   remasterDetailPage("IPX-535"),
				searchURL: remasterSearchPage(),
			}}}
			s := newMarkerTestScraper(nil)
			s.client.SetTransport(transport)
			registry := videoCodeContractRegistry{s}
			parsed, err := matcher.ParseInput(pageURL, registry)
			require.NoError(t, err)
			assert.True(t, parsed.IsURL)
			assert.Equal(t, code, parsed.ID)
			assert.Equal(t, []string{"javdb"}, parsed.CompatibleScrapers)

			result, err := s.ScrapeURL(ctx, pageURL)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, "IPX-535", result.ID)
			assert.Equal(t, pageURL, result.SourceURL)
			assert.Equal(t, []string{pageURL}, transport.requests)

			transport.requests = nil
			engine := scrape.NewQueryOnly(registry)
			result, failure := engine.QueryRaw(ctx, "  "+pageURL+"  ", "javdb")
			require.Nil(t, failure)
			require.NotNil(t, result)
			assert.Equal(t, "IPX-535", result.ID)
			assert.Equal(t, pageURL, result.SourceURL)
			assert.Equal(t, []string{pageURL}, transport.requests, "the explicit URL must reach ScrapeURL without keyword search")

			// The identical valid page does not prove that a bare input names
			// its video slug rather than a catalog identity. Keep that input
			// on the guarded search path instead of reviving the old shortcut.
			transport.requests = nil
			result, failure = engine.QueryRaw(ctx, code, "javdb")
			require.NotNil(t, failure)
			assert.Nil(t, result)
			assert.Equal(t, models.ScraperErrorKindNotFound, failure.Kind)
			assert.Equal(t, []string{searchURL}, transport.requests)
		})
	}
}

func TestMarkerShapedVideoCodeBareAmbiguityContract(t *testing.T) {
	for _, code := range []string{"5aB3h", "5AB3H", "5aB3HD", "5aB3AI"} {
		for _, reverse := range []bool{false, true} {
			t.Run(code+map[bool]string{false: "/raw-first", true: "/display-first"}[reverse], func(t *testing.T) {
				marker := remasterMarkerSuffix(code)
				cards := []string{"AB-3" + marker, "5AB-3" + marker}
				if reverse {
					cards[0], cards[1] = cards[1], cards[0]
				}
				pageURL := "https://javdb.test/v/" + code
				searchURL := "https://javdb.test/search?q=" + url.QueryEscape(code) + "&f=all"
				transport := &videoCodeContractTransport{staticRoundTripper: staticRoundTripper{responses: map[string]string{
					// Even a plausible raw-interpretation page cannot bypass the
					// conflicting display card when the input is only a bare ID.
					pageURL:   remasterDetailPage("AB-3" + marker),
					searchURL: remasterSearchPage(cards...),
				}}}
				s := newMarkerTestScraper(nil)
				s.client.SetTransport(transport)
				engine := scrape.NewQueryOnly(videoCodeContractRegistry{s})
				result, failure := engine.QueryRaw(context.Background(), code, "javdb")
				require.NotNil(t, failure)
				assert.Equal(t, models.ScraperErrorKindNotFound, failure.Kind)
				assert.Nil(t, result)
				assert.Equal(t, []string{searchURL}, transport.requests)

				transport.requests = nil
				result, failure = engine.QueryRaw(context.Background(), pageURL, "javdb")
				require.Nil(t, failure)
				require.NotNil(t, result)
				assert.Equal(t, "AB-3"+marker, result.ID)
				assert.Equal(t, []string{pageURL}, transport.requests)
			})
		}
	}
}

func TestVideoCodeOrdinaryBareShortcutContract(t *testing.T) {
	pageURL := "https://javdb.test/v/5aB3d"
	transport := &videoCodeContractTransport{staticRoundTripper: staticRoundTripper{responses: map[string]string{
		pageURL: remasterDetailPage("IPX-535"),
	}}}
	s := newMarkerTestScraper(nil)
	s.client.SetTransport(transport)
	result, failure := scrape.NewQueryOnly(videoCodeContractRegistry{s}).QueryRaw(context.Background(), "5aB3d", "javdb")
	require.Nil(t, failure)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, []string{pageURL}, transport.requests, "the existing non-marker shortcut is unchanged")
}

func TestMarkerShapedVideoCodeExplicitURLRequiresMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		requests   int
	}{
		{"sparse", "<html><body></body></html>", 2},
		{"challenge", "<html><head><title>Just a moment...</title></head><body>Checking your browser before accessing</body></html>", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pageURL := "https://javdb.test/v/5aB3h"
			transport := &videoCodeContractTransport{staticRoundTripper: staticRoundTripper{responses: map[string]string{
				pageURL: tc.body,
			}}}
			s := newMarkerTestScraper(nil)
			s.client.SetTransport(transport)
			result, err := s.ScrapeURL(context.Background(), pageURL)
			require.Error(t, err)
			assert.Nil(t, result)
			require.Len(t, transport.requests, tc.requests)
			for _, requested := range transport.requests {
				assert.Equal(t, pageURL, requested)
			}
		})
	}
}

type videoCodeContractTransport struct {
	staticRoundTripper
	requests []string
}

func (t *videoCodeContractTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req.URL.String())
	return t.staticRoundTripper.RoundTrip(req)
}

type videoCodeContractRegistry struct{ scraper models.Scraper }

func (r videoCodeContractRegistry) GetInstance(name string) (models.Scraper, bool) {
	return r.scraper, name == r.scraper.Name()
}
func (r videoCodeContractRegistry) GetInstancesByPriorityForInput([]string, string) []models.Scraper {
	return r.GetAllInstances()
}
func (r videoCodeContractRegistry) GetAllInstances() []models.Scraper {
	return []models.Scraper{r.scraper}
}
func (r videoCodeContractRegistry) Names() []string { return []string{r.scraper.Name()} }
