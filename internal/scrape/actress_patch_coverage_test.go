package scrape

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestBuiltinEnrichmentIdentityAndFirstNameBranches(t *testing.T) {
	old := lookupBuiltinActress
	t.Cleanup(func() { lookupBuiltinActress = old })
	lookupBuiltinActress = func(int, string, string, string) (actresscache.Record, bool) {
		return actresscache.Record{DMMID: 2, FirstName: "Filled"}, true
	}
	mismatch := &models.Movie{Actresses: []models.Actress{{DMMID: 1}}}
	assert.Zero(t, enrichActressesFromBuiltinCache(mismatch))

	matching := &models.Movie{Actresses: []models.Actress{{DMMID: 2}}}
	assert.Equal(t, 1, enrichActressesFromBuiltinCache(matching))
	assert.Equal(t, "Filled", matching.Actresses[0].FirstName)
}

func TestValidateActressThumbnailsNilAndTransientFailure(t *testing.T) {
	assert.Zero(t, validateActressThumbnails(t.Context(), nil, &Config{}))
	assert.Zero(t, validateActressThumbnails(t.Context(), &models.Movie{}, nil))
	movie := &models.Movie{Actresses: []models.Actress{{ThumbURL: " https://example.com/valid.jpg "}}}
	calls := 0
	cfg := &Config{ValidateActressThumbnail: func(context.Context, string) error { calls++; return errors.New("temporary") }}
	assert.Zero(t, validateActressThumbnails(t.Context(), movie, cfg))
	assert.Equal(t, 1, calls)
	assert.NotEmpty(t, movie.Actresses[0].ThumbURL)
}

func TestResolverEnrichmentSkipsMismatchesAndRejectedThumbnails(t *testing.T) {
	mismatch := &testMetadataResolver{name: "mismatch", enabled: true, metadata: models.ActressInfo{DMMID: 9, FirstName: "wrong"}}
	rejected := &testMetadataResolver{name: "rejected", enabled: true, metadata: models.ActressInfo{DMMID: 7, FirstName: "Right", LastName: "Name", JapaneseName: "正", ThumbURL: "https://example.com/rejected.jpg"}}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 7}}}
	cfg := &Config{ScrapeActress: true, ValidateActressThumbnail: func(context.Context, string) error { return errors.New("bad image") }}
	assert.Equal(t, 1, enrichActressesFromResolvers(t.Context(), movie, newTestRegistry(mismatch, rejected), cfg))
	assert.Equal(t, "Right", movie.Actresses[0].FirstName)
	assert.Empty(t, movie.Actresses[0].ThumbURL)
	assert.Equal(t, 1, mismatch.calls)
}

type unnamedMetadataResolver struct{}

func (unnamedMetadataResolver) ResolveActressMetadata(context.Context, models.ActressInfo) models.ActressInfo {
	return models.ActressInfo{}
}

func TestFilterMovieScrapersSkipsNil(t *testing.T) {
	assert.Empty(t, filterMovieScrapers([]models.Scraper{nil}))
}

func TestPostProcessRunsThumbnailAndResolverPasses(t *testing.T) {
	old := lookupBuiltinActress
	lookupBuiltinActress = func(int, string, string, string) (actresscache.Record, bool) { return actresscache.Record{}, false }
	t.Cleanup(func() { lookupBuiltinActress = old })
	resolver := &testMetadataResolver{name: "resolver", enabled: true, metadata: models.ActressInfo{DMMID: 5, FirstName: "Resolved", LastName: "Name", JapaneseName: "解決", ThumbURL: "https://example.com/new.jpg"}}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 5, ThumbURL: "https://pics.dmm.co.jp/mono/noimage/now_printing.jpg"}}}
	result, err := postProcessScraped(t.Context(), movie, nil, nil, newTestRegistry(resolver), &Config{ScrapeActress: true}, nil, nil, ScrapeCmd{}, time.Now())
	assert.NoError(t, err)
	assert.Equal(t, "Resolved", result.Movie.Actresses[0].FirstName)
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Actresses[0].ThumbURL)
}

func TestResolverNameFallback(t *testing.T) {
	assert.Equal(t, "resolver", resolverName(unnamedMetadataResolver{}))
}
