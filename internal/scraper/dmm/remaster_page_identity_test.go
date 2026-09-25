package dmm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Finding probe: a persistent cache maps the AI display query DV-818AI to a
// stale same-series cid whose page 品番 is DV-819-AI; Search must not return
// release 819 metadata for the query.
func TestCachedAIQueryWrongPageNumberRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "DV-818AI", "dv00999ai")
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "cid=dv00999ai") {
			return 200, `<html><body><h1 id="title" class="item">DV-819 AI Remaster</h1>` +
				`<table><tr><td>品番：</td><td>DV-819-AI</td></tr></table></body></html>`
		}
		return 404, ""
	}})
	res, err := s.Search(context.Background(), "DV-818AI")
	require.Error(t, err, "the wrong-release page must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// A matching page (DV-818-AI or DV-818AI) returns as today, with hyphenation
// folded by the identity comparison.
func TestCachedAIQueryMatchingPageReturns(t *testing.T) {
	for _, pageDisplay := range []string{"DV-818-AI", "DV-818AI"} {
		s, _ := newRemasterTestScraper(t)
		s.cacheContentID(context.Background(), "DV-818AI", "dv00899ai")
		s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
			if strings.Contains(u, "cid=dv00899ai") {
				return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1>` +
					`<table><tr><td>品番：</td><td>` + pageDisplay + `</td></tr></table></body></html>`
			}
			return 404, ""
		}})
		res, err := s.Search(context.Background(), "DV-818AI")
		require.NoError(t, err, pageDisplay)
		require.NotNil(t, res, pageDisplay)
		assert.Equal(t, "DV-818AI", res.ID, pageDisplay)
		assert.Equal(t, "dv00899ai", res.ContentID, pageDisplay)
	}
}

// A stale cached AI mapping (DV-818AI -> dv00999ai) whose page publishes no
// 品番 cannot be verified in-flow: AI cid numbers diverge from display
// numbers, so nothing on the page ties dv00999ai to the query. The mapping
// is invalidated and the query re-resolves through the verified resolver,
// which proves release 818 (dv00899ai) via its page 品番 and republishes the
// correct metadata — the cache is repaired with the verified release.
func TestCachedAIQueryNoPageIdentityInvalidatesAndReResolves(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "DV-818AI", "dv00999ai")
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "searchstr=dv00999ai/"):
			// Pass one: the URL finder only needs any dv00999ai product URL.
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=dv00999ai/">stale</a></body></html>`
		case strings.Contains(u, "searchstr=dv00818ai/"):
			// Re-resolution: the verified resolver's spelling finds release 818.
			return 200, `<html><body><a href="/mono/dvd/-/detail/=/cid=dv00899ai/">remaster</a></body></html>`
		case strings.Contains(u, "searchstr=dv00899ai/"):
			// The retried URL finder resolves the verified cid's product page.
			return 200, `<html><body><a href="/mono/dvd/-/detail/=/cid=dv00899ai/">remaster</a></body></html>`
		case strings.Contains(u, "cid=dv00999ai"):
			return 200, `<html><body><h1 id="title" class="item">Wrong release</h1></body></html>`
		case strings.Contains(u, "cid=dv00899ai"):
			return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1>` +
				`<table><tr><td>品番：</td><td>DV-818-AI</td></tr></table></body></html>`
		case strings.Contains(u, "/search/="):
			return 200, `<html><body></body></html>`
		}
		return 404, ""
	}})

	res, err := s.Search(context.Background(), "DV-818AI")
	require.NoError(t, err, "the stale mapping self-heals: release 818 resolves through the verified resolver")
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID, "the verified release replaces the stale mapping's product")
	assert.Equal(t, "DV-818AI", res.ID)
	assert.Equal(t, "AI Remaster", res.Title, "the stale product's metadata must not be published")

	cached, err := repo.FindBySearchID(context.Background(), "DV-818AI")
	require.NoError(t, err)
	assert.Equal(t, "dv00899ai", cached.ContentID, "the cache is repaired with the verified release")
}

// A healthy cached AI mapping whose page carries a 品番 verifies in-flow and
// returns without re-resolution: the identity comes from the page row, so
// the self-heal never fires and the HTTP traffic stays at the cache-hit
// flow's five URL-finder searches plus the one product-page fetch.
func TestCachedAIQueryMatchingPageSkipsReResolution(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "DV-818AI", "dv00899ai")
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body><a href="/mono/dvd/-/detail/=/cid=dv00899ai/">remaster</a></body></html>`
		case strings.Contains(u, "cid=dv00899ai"):
			return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1>` +
				`<table><tr><td>品番：</td><td>DV-818-AI</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	res, err := s.Search(context.Background(), "DV-818AI")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "DV-818AI", res.ID)
	assert.Equal(t, "dv00899ai", res.ContentID)
	// Zero extra fetch: a heal would re-run the five search queries and the
	// product fetch, roughly doubling both counters.
	assert.Equal(t, 5, rt.searchN, "the cache-hit URL finder runs its five query spellings once")
	assert.Equal(t, 1, rt.detailN, "only the selected product page is fetched")

	cached, err := repo.FindBySearchID(context.Background(), "DV-818AI")
	require.NoError(t, err)
	assert.Equal(t, "dv00899ai", cached.ContentID, "the healthy mapping is neither invalidated nor rewritten")
}

// When the unverified cached mapping cannot be invalidated (repository
// delete failure), Search must miss honestly rather than synthesize the
// query's identity onto the unverified page's metadata.
func TestCachedAIQueryInvalidateFailureMissesHonestly(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.contentIDRepo = &deleteFailingCIDRepo{cached: &models.ContentIDMapping{
		SearchID: "DV-818AI", ContentID: "dv00999ai", Source: "dmm",
	}}
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "searchstr=dv00999ai/"):
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=dv00999ai/">stale</a></body></html>`
		case strings.Contains(u, "cid=dv00999ai"):
			return 200, `<html><body><h1 id="title" class="item">Wrong release</h1></body></html>`
		}
		return 404, ""
	}})

	res, err := s.Search(context.Background(), "DV-818AI")
	require.Error(t, err, "an unverified mapping that cannot be invalidated must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "no identity")
}

// deleteFailingCIDRepo serves cached lookups but fails deletes, pinning the
// miss-honestly branch of the cache self-heal.
type deleteFailingCIDRepo struct{ cached *models.ContentIDMapping }

func (f *deleteFailingCIDRepo) FindBySearchID(ctx context.Context, searchID string) (*models.ContentIDMapping, error) {
	return f.cached, nil
}

func (f *deleteFailingCIDRepo) Create(ctx context.Context, mapping *models.ContentIDMapping) error {
	return nil
}

func (f *deleteFailingCIDRepo) Delete(ctx context.Context, searchID string) error {
	return errors.New("delete failed")
}

func (f *deleteFailingCIDRepo) GetAllPaginated(ctx context.Context, limit, offset int) ([]models.ContentIDMapping, error) {
	return nil, nil
}

func (f *deleteFailingCIDRepo) GetAll(ctx context.Context) ([]models.ContentIDMapping, error) {
	return nil, nil
}

func (f *deleteFailingCIDRepo) GetAllChunked(ctx context.Context, chunkSize int) ([]models.ContentIDMapping, error) {
	return nil, nil
}

// H/HD queries keep working through the cache-hit path: their number
// binding already exists in cachedRemasterIdentityMatches, and a matching
// page 品番 must not be double-rejected here.
func TestCachedHQueryMatchingPageReturns(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "RCT-156H", "1rct00156h")
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "cid=1rct00156h") {
			return 200, `<html><body><h1 id="title" class="item">Remaster</h1>` +
				`<table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}})
	res, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "RCT-156H", res.ID)
	assert.Equal(t, "1rct00156h", res.ContentID)
}

// Round-25b: a cached H query whose page serves a markerless 品番 must miss
// honestly: the row names the base release (cid=1rct00156h under RCT-157),
// not the queried remaster, so returning its metadata would publish release
// 157's content under RCT-156H's identity.
func TestCachedHQueryMarkerlessPageRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "RCT-156H", "1rct00156h")
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "cid=1rct00156h") {
			return 200, `<html><body><h1 id="title" class="item">Base release</h1>` +
				`<table><tr><td>品番：</td><td>RCT-157</td></tr></table></body></html>`
		}
		return 404, ""
	}})
	res, err := s.Search(context.Background(), "RCT-156H")
	require.Error(t, err, "the markerless 品番 names the base release and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The AI request case of the same rule: the query is marker-bearing, so a
// markerless 品番 (DV-818, the base release the AI cid diverged from) still
// conflicts and must not pass through like an absent identity.
func TestCachedAIQueryMarkerlessPageRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "DV-818AI", "dv00899ai")
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "cid=dv00899ai") {
			return 200, `<html><body><h1 id="title" class="item">Base release</h1>` +
				`<table><tr><td>品番：</td><td>DV-818</td></tr></table></body></html>`
		}
		return 404, ""
	}})
	res, err := s.Search(context.Background(), "DV-818AI")
	require.Error(t, err, "the markerless 品番 names the base release and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The identity guard publishes nothing authoritative to compare for nil
// documents, unparseable display ids, or unparseable queries, and must keep
// the existing behavior in those cases rather than reject the page. A
// nonempty markerless 品番 is the exception: on a marker-bearing query it
// names the base release — never the remaster — and rejects the page.
func TestPageDisplayIdentityMatchesQueryGuards(t *testing.T) {
	page := func(display string) *goquery.Document {
		t.Helper()
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(
			"<html><body><table><tr><td>品番：</td><td>" + display + "</td></tr></table></body></html>"))
		require.NoError(t, err)
		return doc
	}
	for _, tc := range []struct {
		name  string
		doc   *goquery.Document
		query string
		want  bool
	}{
		{"nil document keeps existing behavior", nil, "DV-818AI", true},
		{"missing display value keeps existing behavior", page("???"), "DV-818AI", true},
		{"unparseable display keeps existing behavior", page("12345"), "DV-818AI", true},
		{"unparseable query keeps existing behavior", page("DV-818-AI"), "remastered", true},
		{"markerless display rejects a marker-bearing query", page("RCT-157"), "RCT-156H", false},
		{"compact markerless display rejects a marker-bearing query", page("RCT157"), "RCT-156H", false},
		{"markerless display with unparseable query keeps existing behavior", page("RCT-157"), "remastered", true},
		{"mismatched release rejects", page("DV-819-AI"), "DV-818AI", false},
		{"matching release accepts", page("DV-818-AI"), "DV-818AI", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, pageDisplayIdentityMatchesQuery(tc.doc, tc.query))
		})
	}
}
