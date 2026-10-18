package javdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// detailIdentityMatchesQuery is the post-fetch guard mirroring the DMM
// side's pageDisplayIdentityMatchesQuery (rounds 16a/18/20a): the fetched
// detail page's own published ID must satisfy the marker query's folded
// identity, not just the search card's. The rank function carries the
// round-42 AI slot exemption (a raw AI cid accepts its display listing
// number-free), the H/HD marker-class fold, and the pinned series/number
// boundary; the variant rung keeps rejecting the base release.
func TestDetailIdentityMatchesQuery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		query    string
		resultID string
		nilRes   bool
		want     bool
	}{
		{"base release on marker query", "RCT-156H", "RCT-156", false, false},
		{"another remaster number", "RCT-156H", "RCT-157-HD", false, false},
		{"markerless neighbor", "RCT-156H", "RCT-157", false, false},
		{"folded HD spelling accepted", "RCT-156H", "RCT-156-HD", false, true},
		{"exact spelling accepted", "RCT-156H", "RCT-156H", false, true},
		{"padded display number accepted", "RCT-156H", "RCT-0156H", false, true},
		{"compact display spelling accepted", "RCT-156H", "rct156h", false, true},
		{"foreign series rejected", "RCT-156H", "IPX-156-HD", false, false},
		{"raw ai cid accepts display listing", "dv00899ai", "DV-818AI", false, true},
		{"raw ai cid accepts neighboring display listing", "dv00899ai", "DV-819AI", false, true},
		{"raw ai cid rejects foreign series", "dv00899ai", "IPX-818AI", false, false},
		{"raw ai cid rejects marker class change", "dv00899ai", "DV-818H", false, false},
		{"raw ai cid listing stays literal", "dv00899ai", "dv00900ai", false, false},
		{"raw h cid folds padded number onto display", "1rct00156h", "RCT-156-HD", false, true},
		{"raw h cid keeps number binding", "1rct00156h", "RCT-157-HD", false, false},
		{"ez suffix rides the fold", "IPX-535ZH", "IPX-535-Z-HD", false, true},
		{"missing ez suffix is another release", "IPX-535ZH", "IPX-535H", false, false},
		{"pinned boundary keeps t identities apart", "T-28123H", "T28-123H", false, false},
		{"non-marker query accepts any detail", "RCT-156", "RCT-157", false, true},
		{"non-marker query accepts base of remaster", "RCT-156", "RCT-156H", false, true},
		{"marker query without published id", "RCT-156H", "", false, true},
		{"nil result publishes nothing to validate", "RCT-156H", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result *models.ScraperResult
			if !tc.nilRes {
				result = &models.ScraperResult{ID: tc.resultID}
			}
			assert.Equal(t, tc.want, detailIdentityMatchesQuery(result, tc.query))
		})
	}
}

// The finding: an exact remaster search card can point at a stale or
// redirected detail page. findDetailURLCtx validated only the ID the
// search card showed, so an RCT-156H query whose selected link serves the
// base release's (or another remaster's) page returned that release's
// metadata. The post-fetch guard must reject those pages honestly.
func TestSearchMarkerQueryRejectsDetailPageServingOtherRelease(t *testing.T) {
	for _, tc := range []struct {
		name     string
		query    string
		card     string
		detailID string
	}{
		{"selected link serves the base release", "RCT-156H", "RCT-156H", "RCT-156"},
		{"selected link serves another remaster", "RCT-156H", "RCT-156H", "RCT-157-HD"},
		{"selected link serves the markerless neighbor", "RCT-156H", "RCT-156H", "RCT-157"},
		{"raw h cid link serves the neighboring remaster", "1rct00156h", "RCT-156-HD", "RCT-157-HD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMarkerTestScraper(map[string]string{
				"https://javdb.test/search?q=" + tc.query + "&f=all":                                              remasterSearchPage(tc.card),
				"https://javdb.test/v/" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(tc.card)): remasterDetailPage(tc.detailID),
			})
			res, err := s.Search(context.Background(), tc.query)
			require.Error(t, err, "a detail page publishing %s must not satisfy the %s query", tc.detailID, tc.query)
			assert.Nil(t, res, "the foreign release's metadata must not be returned")
			scraperErr, ok := models.AsScraperError(err)
			require.True(t, ok)
			assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
		})
	}
}

// Matching details pass through: the marker spelling fold (H/HD) and the
// exact spelling both satisfy the query's identity on the fetched page.
func TestSearchMarkerQueryAcceptsDetailPageSatisfyingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		query    string
		card     string
		detailID string
	}{
		{"folded HD spelling on the detail page", "RCT-156H", "RCT-156-HD", "RCT-156-HD"},
		{"exact spelling on the detail page", "RCT-156H", "RCT-156H", "RCT-156H"},
		{"folded H spelling under an HD query", "RCT-156-HD", "RCT-156H", "RCT-156H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMarkerTestScraper(map[string]string{
				"https://javdb.test/search?q=" + tc.query + "&f=all":                                              remasterSearchPage(tc.card),
				"https://javdb.test/v/" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(tc.card)): remasterDetailPage(tc.detailID),
			})
			res, err := s.Search(context.Background(), tc.query)
			require.NoError(t, err, "a detail page publishing %s satisfies the %s query", tc.detailID, tc.query)
			require.NotNil(t, res)
			assert.Equal(t, tc.detailID, res.ID)
		})
	}
}

// The round-42 AI slot-number exemption must survive the post-fetch
// guard: the guard ranks through remasterFoldMatchRank rather than a
// stricter literal comparison, so a raw AI cid query whose detail page
// serves the correct display listing (or a neighboring same-series one)
// still matches through the number-free rung.
func TestSearchRawAICIDQueryDetailPageKeepsExemption(t *testing.T) {
	for _, tc := range []struct {
		name     string
		detailID string
	}{
		{"the correct display listing", "DV-818AI"},
		{"a neighboring same-series display listing", "DV-819AI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMarkerTestScraper(map[string]string{
				"https://javdb.test/search?q=dv00899ai&f=all":                                                         remasterSearchPage(tc.detailID),
				"https://javdb.test/v/" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(tc.detailID)): remasterDetailPage(tc.detailID),
			})
			res, err := s.Search(context.Background(), "dv00899ai")
			require.NoError(t, err, "the raw AI cid's number-free rung must satisfy the detail guard")
			require.NotNil(t, res)
			assert.Equal(t, tc.detailID, res.ID)
		})
	}
}

// Non-marker queries carry no remaster identity to validate: their
// behavior is unchanged by the guard, whatever the detail page serves.
func TestSearchNonMarkerQueryUnchangedByDetailGuard(t *testing.T) {
	for _, detailID := range []string{"RCT-157", "RCT-156H", "IPX-999"} {
		t.Run("detail serves "+detailID, func(t *testing.T) {
			s := newMarkerTestScraper(map[string]string{
				"https://javdb.test/search?q=RCT-156&f=all": remasterSearchPage("RCT-156"),
				"https://javdb.test/v/rct156":               remasterDetailPage(detailID),
			})
			res, err := s.Search(context.Background(), "RCT-156")
			require.NoError(t, err, "non-marker queries keep the existing behavior")
			require.NotNil(t, res)
			assert.Equal(t, detailID, res.ID)
		})
	}
}

// The direct-URL path takes the guard too: a video-code-shaped marker
// query (a raw AI cid) whose /v/{cid} page serves a foreign release's
// metadata must fall back to search instead of returning it, and search
// then resolves the correct display listing.
func TestSearchDirectURLMarkerQueryFallsBackOnForeignPage(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/v/dv00899ai":              remasterDetailPage("IPX-818AI"),
		"https://javdb.test/search?q=dv00899ai&f=all": remasterSearchPage("DV-818AI"),
		"https://javdb.test/v/dv818ai":                remasterDetailPage("DV-818AI"),
	})
	res, err := s.Search(context.Background(), "dv00899ai")
	require.NoError(t, err, "the foreign direct page must fall through to search, which resolves the display listing")
	require.NotNil(t, res)
	assert.Equal(t, "DV-818AI", res.ID)
}

// The retry path validates the retried page's identity too: a sparse
// primary response followed by a rich direct retry that serves another
// release's page misses honestly instead of returning its metadata.
func TestSearchMarkerQueryRejectsForeignDetailAfterSparseRetry(t *testing.T) {
	sparseDetailHTML := `<html><body><h2 class="title is-4"><strong>RCT-156H</strong> RCT-156H</h2></body></html>`
	var detailRequests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(remasterSearchPage("RCT-156H")))
		case r.URL.Path == "/v/rct156h":
			if atomic.AddInt32(&detailRequests, 1) == 1 {
				_, _ = w.Write([]byte(sparseDetailHTML))
				return
			}
			_, _ = w.Write([]byte(remasterDetailPage("RCT-157-HD")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     server.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}
	res, err := scraper.Search(context.Background(), "RCT-156H")
	require.Error(t, err, "a retried page publishing RCT-157-HD must not satisfy the RCT-156H query")
	assert.Nil(t, res)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// A selected detail URL that fails to fetch surfaces the existing fetch
// error rather than any fallback.
func TestSearchDetailFetchErrorAfterCardMatch(t *testing.T) {
	s := newMarkerTestScraper(map[string]string{
		"https://javdb.test/search?q=SSIS-001&f=all": remasterSearchPage("SSIS-001"),
	})
	_, err := s.Search(context.Background(), "SSIS-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch detail page")
}

// A sparse primary detail page whose direct retry also fails to fetch
// surfaces the wrapped retry error.
func TestSearchDetailRetryFetchErrorAfterSparsePage(t *testing.T) {
	sparseDetailHTML := `<html><body><h2 class="title is-4"><strong>SSIS-001</strong> SSIS-001</h2></body></html>`
	var detailRequests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(remasterSearchPage("SSIS-001")))
		case r.URL.Path == "/v/ssis001":
			if atomic.AddInt32(&detailRequests, 1) == 1 {
				_, _ = w.Write([]byte(sparseDetailHTML))
				return
			}
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     server.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}
	_, err := scraper.Search(context.Background(), "SSIS-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsed sparse detail page and direct retry failed")
}
