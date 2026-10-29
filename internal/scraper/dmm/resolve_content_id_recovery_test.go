package dmm

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestResolveContentIDCtx_NoMatchPreservesCacheAndRecovers(t *testing.T) {
	ctx := context.Background()
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(ctx, &models.ContentIDMapping{
		SearchID: "ABP-420", ContentID: "abp00420", Source: "dmm",
	}))
	before, err := repo.GetAllChunked(ctx, 100)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Equal(t, "ABP-420", before[0].SearchID)
	require.Equal(t, "abp00420", before[0].ContentID)
	require.Equal(t, "dmm", before[0].Source)
	_, err = repo.FindBySearchID(ctx, "IPX-535")
	require.True(t, database.IsNotFound(err), "target must start uncached: %v", err)

	var settings models.ScraperSettings
	// Decode explicit zeros through the normal configuration path, rather than
	// mutating the prepared client or limiter to suppress retries and delays.
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true,"rate_limit":0,"timeout":5,"retry_count":0,"use_browser":false,"scrape_actress":false}`), &settings))
	s := newScraper(&settings, nil, models.FlareSolverrConfig{}, dmmOptions{ContentIDRepo: repo})
	require.Zero(t, s.Config().RateLimit)
	require.Zero(t, s.client.RetryCount)
	require.False(t, s.useBrowser)
	require.False(t, s.scrapeActress)

	// Literal URLs keep the expected query order independent of the resolver's
	// query builder. Every accepted request is served by this owned transport.
	queries := []string{
		"https://www.dmm.co.jp/search/=/searchstr=ipx535/",
		"https://www.dmm.co.jp/search/=/searchstr=ipx00535/",
		"https://www.dmm.co.jp/search/=/searchstr=ipx-535/",
	}
	const nonProductAnchors = `
		<a href="/help/">Help</a>
		<a href="/search/=/searchstr=ipx535/">IPX-535</a>
		<a href="/mono/dvd/-/detail/=/cid=abp00420/">Other product</a>`
	responses := map[string]string{
		queries[0]: "<html><body>" + nonProductAnchors + "</body></html>",
		queries[1]: "<html><body>" + nonProductAnchors + "</body></html>",
		queries[2]: "<html><body>" + nonProductAnchors + "</body></html>",
	}
	var requested []string
	s.client.SetTransport(dmmFixtureTransport(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.Method+" "+req.URL.String())
		require.Equal(t, http.MethodGet, req.Method)
		body, ok := responses[req.URL.String()]
		require.True(t, ok, "unexpected offline request: %s", req.URL)
		return dmmFixtureResponse(req, http.StatusOK, body), nil
	}))

	// A successful search response is not evidence of a matching product.
	// Navigation text and an unrelated product must not create any cache row.
	got, err := s.ResolveContentIDCtx(ctx, "IPX-535")
	require.Empty(t, got)
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	require.Equal(t, "DMM", scraperErr.Scraper)
	require.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
	require.False(t, scraperErr.Retryable)
	require.False(t, scraperErr.Temporary)
	require.Zero(t, scraperErr.StatusCode)
	require.Contains(t, scraperErr.Message, "no matching content-id")
	wantRequests := []string{"GET " + queries[0], "GET " + queries[1], "GET " + queries[2]}
	require.Equal(t, wantRequests, requested)
	_, err = repo.FindBySearchID(ctx, "IPX-535")
	require.True(t, database.IsNotFound(err), "a miss must remain uncached: %v", err)
	afterMiss, err := repo.GetAllChunked(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, before, afterMiss, "a miss must preserve every persisted field of unrelated mappings")

	// Only the upstream page changes. The same scraper and real repository must
	// recover on the first query, without a cached miss preventing fresh work.
	responses[queries[0]] = "<html><body>" + nonProductAnchors + `
		<a href="/mono/dvd/-/detail/=/cid=ipx00535/">IPX-535 result</a>
		</body></html>`
	got, err = s.ResolveContentIDCtx(ctx, "IPX-535")
	require.NoError(t, err)
	require.Equal(t, "ipx00535", got)
	wantRequests = append(wantRequests, "GET "+queries[0])
	require.Equal(t, wantRequests, requested, "a genuine match must stop query fallback")
	cached, err := repo.FindBySearchID(ctx, "IPX-535")
	require.NoError(t, err)
	require.Equal(t, "IPX-535", cached.SearchID)
	require.Equal(t, "ipx00535", cached.ContentID)
	require.Equal(t, "dmm", cached.Source)
	require.NotZero(t, cached.ID)
	require.False(t, cached.CreatedAt.IsZero())
	afterRecovery, err := repo.GetAllChunked(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, []models.ContentIDMapping{before[0], *cached}, afterRecovery,
		"recovery must add only the verified target, preserving the unrelated row")

	got, err = s.ResolveContentIDCtx(ctx, "IPX-535")
	require.NoError(t, err)
	require.Equal(t, "ipx00535", got)
	require.Equal(t, wantRequests, requested, "a cache hit must perform no HTTP requests")
	afterCacheHit, err := repo.GetAllChunked(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, afterRecovery, afterCacheHit, "a cache hit must not rewrite persisted mappings")
}
