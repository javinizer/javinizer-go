package scrape

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testMetadataResolver struct {
	name     string
	enabled  bool
	metadata models.ActressInfo
	calls    int
}

func (m *testMetadataResolver) Name() string { return m.name }
func (m *testMetadataResolver) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *testMetadataResolver) GetURL(context.Context, string) (string, error) { return "", nil }
func (m *testMetadataResolver) IsEnabled() bool                                { return m.enabled }
func (m *testMetadataResolver) Config() *models.ScraperSettings                { return nil }
func (m *testMetadataResolver) Close() error                                   { return nil }
func (m *testMetadataResolver) ResolveActressMetadata(_ context.Context, actress models.ActressInfo) models.ActressInfo {
	m.calls++
	return m.metadata
}

func TestEnrichActressesFromResolversFillsBlankFields(t *testing.T) {
	registry := newTestRegistry(&testMetadataResolver{
		name:    "minnanoav",
		enabled: true,
		metadata: models.ActressInfo{
			DMMID:        19244,
			FirstName:    "Asami",
			LastName:     "Abe",
			JapaneseName: "安倍亜沙美",
			ThumbURL:     "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg",
		},
	})
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 19244}}}
	cfg := &Config{ScrapeActress: true}

	enriched := enrichActressesFromResolvers(context.Background(), movie, registry, cfg)
	require.Equal(t, 1, enriched)
	assert.Equal(t, "Asami", movie.Actresses[0].FirstName)
	assert.Equal(t, "Abe", movie.Actresses[0].LastName)
	assert.Equal(t, "安倍亜沙美", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg", movie.Actresses[0].ThumbURL)
}

func TestEnrichActressesFromResolversSkipsCompleteActresses(t *testing.T) {
	registry := newTestRegistry(&testMetadataResolver{
		name:    "minnanoav",
		enabled: true,
		metadata: models.ActressInfo{
			DMMID:     19244,
			FirstName: "ShouldNotUse",
		},
	})
	movie := &models.Movie{Actresses: []models.Actress{{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe",
		JapaneseName: "安倍亜沙美", ThumbURL: "https://example.com/thumb.jpg",
	}}}
	cfg := &Config{ScrapeActress: true}

	enriched := enrichActressesFromResolvers(context.Background(), movie, registry, cfg)
	assert.Zero(t, enriched)
	assert.Equal(t, "Asami", movie.Actresses[0].FirstName)
}

func TestEnrichActressesFromResolversGatedByScrapeActress(t *testing.T) {
	registry := newTestRegistry(&testMetadataResolver{name: "minnanoav", enabled: true, metadata: models.ActressInfo{DMMID: 1, FirstName: "X"}})
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 1}}}
	cfg := &Config{ScrapeActress: false}

	enriched := enrichActressesFromResolvers(context.Background(), movie, registry, cfg)
	assert.Zero(t, enriched)
}

func TestEnrichActressesFromResolversRepairsKnownInvalidThumbnail(t *testing.T) {
	resolver := &testMetadataResolver{name: "minnanoav", enabled: true, metadata: models.ActressInfo{
		DMMID: 19244, ThumbURL: "https://www.minnano-av.com/p_actress_125_125/001/811239.jpg",
	}}
	registry := newTestRegistry(resolver)
	movie := &models.Movie{Actresses: []models.Actress{{
		DMMID: 19244, FirstName: "Asami", LastName: "Abe", JapaneseName: "安倍亜沙美",
		ThumbURL: "https://pics.dmm.co.jp/mono/noimage/now_printing.jpg",
	}}}

	enriched := enrichActressesFromResolvers(context.Background(), movie, registry, &Config{ScrapeActress: true})
	require.Equal(t, 1, enriched)
	assert.Equal(t, resolver.metadata.ThumbURL, movie.Actresses[0].ThumbURL)
}

func TestEnrichActressesFromResolversComposesAcrossResolvers(t *testing.T) {
	namesOnly := &testMetadataResolver{
		name: "names", enabled: true,
		metadata: models.ActressInfo{DMMID: 100, FirstName: "Yui", LastName: "Hatano"},
	}
	thumbsOnly := &testMetadataResolver{
		name: "thumbs", enabled: true,
		metadata: models.ActressInfo{DMMID: 100, JapaneseName: "波多野結衣", ThumbURL: "https://example.com/thumb.jpg"},
	}
	registry := newTestRegistry(namesOnly, thumbsOnly)
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 100}}}
	cfg := &Config{ScrapeActress: true}

	enriched := enrichActressesFromResolvers(context.Background(), movie, registry, cfg)
	require.Equal(t, 1, enriched)
	assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
	assert.Equal(t, "Hatano", movie.Actresses[0].LastName)
	assert.Equal(t, "波多野結衣", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "https://example.com/thumb.jpg", movie.Actresses[0].ThumbURL)
	assert.Equal(t, 1, namesOnly.calls)
	assert.Equal(t, 1, thumbsOnly.calls)
}

type movieSearchOptOut struct {
	name    string
	enabled bool
}

func (m *movieSearchOptOut) Name() string { return m.name }
func (m *movieSearchOptOut) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *movieSearchOptOut) GetURL(context.Context, string) (string, error) { return "", nil }
func (m *movieSearchOptOut) IsEnabled() bool                                { return m.enabled }
func (m *movieSearchOptOut) Config() *models.ScraperSettings                { return nil }
func (m *movieSearchOptOut) Close() error                                   { return nil }
func (m *movieSearchOptOut) SupportsMovieSearch() bool                      { return false }

func TestFilterMovieScrapersExcludesMetadataOnlySources(t *testing.T) {
	movieScraper := &testMetadataResolver{name: "r18dev", enabled: true}
	metadataOnly := &movieSearchOptOut{name: "minnanoav", enabled: true}

	filtered := filterMovieScrapers([]models.Scraper{movieScraper, metadataOnly})
	require.Len(t, filtered, 1)
	assert.Equal(t, "r18dev", filtered[0].Name())
}

func TestFilterMovieScrapersKeepsAllWhenNoOptOut(t *testing.T) {
	a := &testMetadataResolver{name: "r18dev", enabled: true}
	b := &testMetadataResolver{name: "dmm", enabled: true}

	filtered := filterMovieScrapers([]models.Scraper{a, b})
	require.Len(t, filtered, 2)
}

func newTestRegistry(scrapers ...models.Scraper) ScraperInstanceResolver {
	return &stubInstanceResolver{instances: scrapers}
}

type stubInstanceResolver struct {
	instances []models.Scraper
}

func (r *stubInstanceResolver) GetInstance(name string) (models.Scraper, bool) {
	for _, s := range r.instances {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}
func (r *stubInstanceResolver) GetInstancesByPriorityForInput(_ []string, _ string) []models.Scraper {
	return r.instances
}
func (r *stubInstanceResolver) GetAllInstances() []models.Scraper { return r.instances }
func (r *stubInstanceResolver) Names() []string {
	names := make([]string, 0, len(r.instances))
	for _, s := range r.instances {
		names = append(names, s.Name())
	}
	return names
}
func TestCachedAndFreshActressEnrichmentUseResolvedPriority(t *testing.T) {
	dmm := &testMetadataResolver{name: "dmm", enabled: true, metadata: models.ActressInfo{DMMID: 77, FirstName: "DMM"}}
	minnano := &testMetadataResolver{name: "minnanoav", enabled: true, metadata: models.ActressInfo{DMMID: 77, FirstName: "Minnano"}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(dmm)
	registry.RegisterInstance(minnano)
	cfg := &Config{ScrapeActress: true, ScrapersPriority: []string{"dmm", "minnanoav"}}
	cached := &models.Movie{ContentID: "URL-123", Actresses: []models.Actress{{DMMID: 77}}}
	movieRepo := &finalMovieRepo{movie: cached}
	s := New(registry, nil, nil, movieRepo, nil, cfg, nil, nil)
	result := s.tryCache(t.Context(), ScrapeCmd{MovieID: "URL-123", PriorityOverride: []string{"minnanoav", "dmm"}}, nil, time.Now())
	require.NotNil(t, result)
	require.Equal(t, "Minnano", result.Movie.Actresses[0].FirstName)

	fresh := &models.Movie{Actresses: []models.Actress{{DMMID: 77}}}
	require.Equal(t, 1, enrichActressesFromResolvers(t.Context(), fresh, registry, cfg, []string{"minnanoav", "dmm"}))
	require.Equal(t, "Minnano", fresh.Actresses[0].FirstName)
}

func TestEnrichActressesFromResolversHonorsPriority(t *testing.T) {
	dmm := &testMetadataResolver{name: "dmm", enabled: true, metadata: models.ActressInfo{DMMID: 77, FirstName: "DMM"}}
	minnano := &testMetadataResolver{name: "minnanoav", enabled: true, metadata: models.ActressInfo{DMMID: 77, FirstName: "Minnano"}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(dmm)
	registry.RegisterInstance(minnano)

	configured := &models.Movie{Actresses: []models.Actress{{DMMID: 77}}}
	require.Equal(t, 1, enrichActressesFromResolvers(t.Context(), configured, registry, &Config{ScrapeActress: true, ScrapersPriority: []string{"minnanoav", "dmm"}}))
	require.Equal(t, "Minnano", configured.Actresses[0].FirstName)

	selected := &models.Movie{Actresses: []models.Actress{{DMMID: 77}}}
	require.Equal(t, 1, enrichActressesFromResolvers(t.Context(), selected, registry, &Config{ScrapeActress: true, ScrapersPriority: []string{"dmm", "minnanoav"}}, []string{"minnanoav", "dmm"}))
	require.Equal(t, "Minnano", selected.Actresses[0].FirstName)
}
func TestCollectMetadataResolversSkipsNilInstances(t *testing.T) {
	require.Empty(t, collectMetadataResolvers(newTestRegistry(nil), []string{"missing"}))
}
