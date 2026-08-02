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
