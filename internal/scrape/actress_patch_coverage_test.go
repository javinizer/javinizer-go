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

func TestValidateActressThumbnailsSkipsRemoteValidation(t *testing.T) {
	assert.Zero(t, validateActressThumbnails(nil, &Config{}))
	assert.Zero(t, validateActressThumbnails(&models.Movie{}, nil))
	movie := &models.Movie{Actresses: []models.Actress{{ThumbURL: " https://example.com/valid.jpg "}}}
	calls := 0
	cfg := &Config{ValidateActressThumbnail: func(context.Context, string) error { calls++; return errors.New("temporary") }}
	assert.Zero(t, validateActressThumbnails(movie, cfg))
	assert.Zero(t, calls)
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

type sessionValidatingMetadataResolver struct {
	*testMetadataResolver
	validationCalls int
	validationErr   error
}

func (r *sessionValidatingMetadataResolver) ValidateActressThumbnail(context.Context, string) error {
	r.validationCalls++
	return r.validationErr
}

func TestResolverEnrichmentPrefersSessionValidation(t *testing.T) {
	resolver := &sessionValidatingMetadataResolver{testMetadataResolver: &testMetadataResolver{
		name: "session", enabled: true,
		metadata: models.ActressInfo{DMMID: 8, ThumbURL: "https://session.example/thumb.jpg"},
	}}
	fallbackCalls := 0
	cfg := &Config{
		ScrapeActress: true,
		ValidateActressThumbnail: func(context.Context, string) error {
			fallbackCalls++
			return errors.New("direct request rejected")
		},
	}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 8}}}

	assert.Equal(t, 1, enrichActressesFromResolvers(t.Context(), movie, newTestRegistry(resolver), cfg))
	assert.Equal(t, 1, resolver.validationCalls)
	assert.Zero(t, fallbackCalls)
	assert.Equal(t, resolver.metadata.ThumbURL, movie.Actresses[0].ThumbURL)
}

func TestResolverEnrichmentSkipsReplacementValidationForExistingThumbnail(t *testing.T) {
	resolver := &sessionValidatingMetadataResolver{testMetadataResolver: &testMetadataResolver{
		name: "session", enabled: true,
		metadata: models.ActressInfo{DMMID: 8, FirstName: "Filled", ThumbURL: "https://replacement.example/thumb.jpg"},
	}}
	fallbackCalls := 0
	cfg := &Config{ScrapeActress: true, ValidateActressThumbnail: func(context.Context, string) error {
		fallbackCalls++
		return nil
	}}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 8, ThumbURL: "https://existing.example/thumb.jpg"}}}

	assert.Equal(t, 1, enrichActressesFromResolvers(t.Context(), movie, newTestRegistry(resolver), cfg))
	assert.Zero(t, resolver.validationCalls)
	assert.Zero(t, fallbackCalls)
	assert.Equal(t, "https://existing.example/thumb.jpg", movie.Actresses[0].ThumbURL)
	assert.Equal(t, "Filled", movie.Actresses[0].FirstName)
}

type unnamedMetadataResolver struct{}

func (unnamedMetadataResolver) ResolveActressMetadata(context.Context, models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{}, nil
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
	result, err := postProcessScraped(t.Context(), movie, nil, nil, newTestRegistry(resolver), &Config{ScrapeActress: true, ValidateActressThumbnail: func(context.Context, string) error { return nil }}, nil, nil, ScrapeCmd{}, false, time.Now())
	assert.NoError(t, err)
	assert.Equal(t, "Resolved", result.Movie.Actresses[0].FirstName)
	assert.Equal(t, "https://example.com/new.jpg", result.Movie.Actresses[0].ThumbURL)
}

func TestResolverNameFallback(t *testing.T) {
	assert.Equal(t, "resolver", resolverName(unnamedMetadataResolver{}))
}
