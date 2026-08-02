package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// The documented three-state contract: a per-scraper scrape_actress override
// must win over the global flag in both directions.
func TestResolverEnrichmentPerScraperOverrideBeatsGlobal(t *testing.T) {
	opIn := true
	resolver := &testMetadataResolver{name: "optin", enabled: true, scrapeActress: &opIn, metadata: models.ActressInfo{DMMID: 7, FirstName: "Via", LastName: "Override"}}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 7}}}
	assert.Equal(t, 1, enrichActressesFromResolvers(context.Background(), movie, newTestRegistry(resolver), &Config{ScrapeActress: false}))
	assert.Equal(t, "Via", movie.Actresses[0].FirstName)
	assert.Equal(t, 1, resolver.calls)

	optOut := false
	muted := &testMetadataResolver{name: "optout", enabled: true, scrapeActress: &optOut, metadata: models.ActressInfo{DMMID: 7, FirstName: "No"}}
	movie2 := &models.Movie{Actresses: []models.Actress{{DMMID: 7}}}
	assert.Equal(t, 0, enrichActressesFromResolvers(context.Background(), movie2, newTestRegistry(muted), &Config{ScrapeActress: true}))
	assert.Equal(t, 0, muted.calls)
}

// A non-empty actress field override restricts enrichment to the named
// resolvers exclusively, and the skip sentinel suppresses it entirely.
func TestResolverEnrichmentActressFieldOverrideIsExclusive(t *testing.T) {
	first := &testMetadataResolver{name: "dmm", enabled: true, metadata: models.ActressInfo{DMMID: 7, FirstName: "DmmName"}}
	second := &testMetadataResolver{name: "minnanoav", enabled: true, metadata: models.ActressInfo{DMMID: 7, FirstName: "MinnanoName"}}

	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 7}}}
	excl := &Config{ScrapeActress: true, ActressFieldPriority: []string{"minnanoav"}}
	assert.Equal(t, 1, enrichActressesFromResolvers(context.Background(), movie, newTestRegistry(first, second), excl))
	assert.Equal(t, "MinnanoName", movie.Actresses[0].FirstName)
	assert.Equal(t, 0, first.calls, "override-unlisted resolver must not run")

	movie2 := &models.Movie{Actresses: []models.Actress{{DMMID: 7}}}
	skip := &Config{ScrapeActress: true, ActressFieldPriority: []string{"__skip__"}}
	assert.Equal(t, 0, enrichActressesFromResolvers(context.Background(), movie2, newTestRegistry(first), skip))
	assert.Equal(t, 0, first.calls)
}

type recordingMetadataResolver struct {
	*testMetadataResolver
	lastInput models.ActressInfo
}

func (r *recordingMetadataResolver) ResolveActressMetadata(_ context.Context, actress models.ActressInfo) (models.ActressInfo, error) {
	r.lastInput = actress
	return r.metadata, nil
}

// Each resolver must see values discovered by earlier ones: a name-keyed
// resolver can only contribute once another source found the Japanese name.
func TestResolverEnrichmentThreadsEarlierDiscoveries(t *testing.T) {
	discoverer := &testMetadataResolver{name: "dmm", enabled: true, metadata: models.ActressInfo{DMMID: 7, JapaneseName: "発見"}}
	recorder := &recordingMetadataResolver{testMetadataResolver: &testMetadataResolver{
		name: "minnanoav", enabled: true, metadata: models.ActressInfo{DMMID: 7, FirstName: "Filled"},
	}}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 7}}}
	assert.Equal(t, 1, enrichActressesFromResolvers(context.Background(), movie, newTestRegistry(discoverer, recorder), &Config{ScrapeActress: true}))
	assert.Equal(t, "発見", recorder.lastInput.JapaneseName)
	assert.Equal(t, "Filled", movie.Actresses[0].FirstName)
}

// priorityFilteringResolver mirrors the production instance store: priority
// selects the named instances, while GetAllInstances exposes every one.
type priorityFilteringResolver struct {
	instances []models.Scraper
}

func (r *priorityFilteringResolver) GetInstance(name string) (models.Scraper, bool) {
	for _, s := range r.instances {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}

func (r *priorityFilteringResolver) GetInstancesByPriorityForInput(priority []string, _ string) []models.Scraper {
	if len(priority) == 0 {
		return r.instances
	}
	order := make(map[string]int, len(priority))
	for i, name := range priority {
		order[name] = i
	}
	out := make([]models.Scraper, 0, len(priority))
	for _, s := range r.instances {
		if _, ok := order[s.Name()]; ok {
			out = append(out, s)
		}
	}
	return out
}

func (r *priorityFilteringResolver) GetAllInstances() []models.Scraper { return r.instances }

func (r *priorityFilteringResolver) Names() []string {
	names := make([]string, 0, len(r.instances))
	for _, s := range r.instances {
		names = append(names, s.Name())
	}
	return names
}

func TestCollectMetadataResolversExplicitSelectionIsExclusive(t *testing.T) {
	first := &testMetadataResolver{name: "dmm", enabled: true}
	second := &testMetadataResolver{name: "minnanoav", enabled: true}
	registry := &priorityFilteringResolver{instances: []models.Scraper{first, second}}

	both := collectMetadataResolvers(registry, []string{"dmm"}, &Config{ScrapeActress: true}, false)
	assert.Len(t, both, 2, "default list appends remaining registered resolvers")
	only := collectMetadataResolvers(registry, []string{"dmm"}, &Config{ScrapeActress: true}, true)
	assert.Len(t, only, 1, "explicit selection must not pull in other resolvers")

	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 7}}}
	first.metadata = models.ActressInfo{DMMID: 7, FirstName: "ViaDmm"}
	second.metadata = models.ActressInfo{DMMID: 7, FirstName: "ViaMinnanoAV"}
	enriched := enrichActressesFromResolvers(context.Background(), movie, registry, &Config{ScrapeActress: true}, []string{"dmm"})
	assert.Equal(t, 1, enriched)
	assert.Equal(t, "ViaDmm", movie.Actresses[0].FirstName)
	assert.Equal(t, 0, second.calls)
}
