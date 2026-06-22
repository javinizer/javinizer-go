package aggregator

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFieldSourcesInvariant_AggregationOutputMatchesProvenanceKeys(t *testing.T) {
	expectedFieldSourceKeys := map[string]struct{}{
		"id":                 {},
		"content_id":         {},
		"title":              {},
		"display_title":      {},
		"original_title":     {},
		"description":        {},
		"director":           {},
		"maker":              {},
		"label":              {},
		"series":             {},
		"poster_url":         {},
		"cover_url":          {},
		"trailer_url":        {},
		"runtime":            {},
		"release_date":       {},
		"rating_score":       {},
		"rating_votes":       {},
		"actresses":          {},
		"genres":             {},
		"screenshot_urls":    {},
		"should_crop_poster": {},
	}

	agg, results := newAggregatorWithFullResults(t)
	movie, aggResult, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	fieldSources := aggResult.FieldSources
	require.NotNil(t, fieldSources, "AggregateResult.FieldSources should not be nil after aggregation")

	actualKeys := make(map[string]struct{}, len(fieldSources))
	for k := range fieldSources {
		actualKeys[k] = struct{}{}
	}

	missingInActual := make([]string, 0)
	for k := range expectedFieldSourceKeys {
		if _, exists := actualKeys[k]; !exists {
			missingInActual = append(missingInActual, k)
		}
	}

	unexpectedInActual := make([]string, 0)
	for k := range actualKeys {
		if _, exists := expectedFieldSourceKeys[k]; !exists {
			unexpectedInActual = append(unexpectedInActual, k)
		}
	}

	assert.Empty(t, missingInActual, "provenance is missing keys that the aggregation produces; add them to aggregateWithPriority")
	assert.Empty(t, unexpectedInActual, "provenance has unexpected keys not in the expected set; update this test or the aggregation")
}

func TestFieldSourcesInvariant_NFOMetadataFieldsCoveredByProvenance(t *testing.T) {
	nfoFieldToProvenanceKey := map[string]string{
		"ID": "id", "ContentID": "content_id", "Title": "title",
		"DisplayTitle": "display_title", "OriginalTitle": "original_title",
		"Description": "description", "Director": "director",
		"Maker": "maker", "Label": "label", "Series": "series",
		"PosterURL": "poster_url", "CoverURL": "cover_url",
		"TrailerURL": "trailer_url", "Runtime": "runtime",
		"ReleaseDate": "release_date", "RatingScore": "rating_score",
		"RatingVotes": "rating_votes", "Actresses": "actresses",
		"Genres": "genres", "Screenshots": "screenshot_urls",
		"ShouldCropPoster": "should_crop_poster",
	}

	expectedFieldSourceKeys := map[string]struct{}{
		"id": {}, "content_id": {}, "title": {}, "display_title": {},
		"original_title": {}, "description": {}, "director": {},
		"maker": {}, "label": {}, "series": {}, "poster_url": {},
		"cover_url": {}, "trailer_url": {}, "runtime": {},
		"release_date": {}, "rating_score": {}, "rating_votes": {},
		"actresses": {}, "genres": {}, "screenshot_urls": {},
		"should_crop_poster": {},
	}

	uncovered := make([]string, 0)
	for nfoField, provKey := range nfoFieldToProvenanceKey {
		if _, exists := expectedFieldSourceKeys[provKey]; !exists {
			uncovered = append(uncovered, nfoField)
		}
	}
	assert.Empty(t, uncovered, "NFO metadata fields not covered by provenance keys; add missing provenance tracking in aggregateWithPriority")
}

func newAggregatorWithFullResults(t *testing.T) (*Aggregator, []*models.ScraperResult) {
	t.Helper()
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"src1"},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	require.NotNil(t, agg)

	releaseDate := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	results := []*models.ScraperResult{
		{
			Source: "src1", ID: "MOV-001", ContentID: "mov001", Title: "Test Movie",
			OriginalTitle: "オリジナル", Description: "A test", Director: "Dir",
			Maker: "Test Studio", Label: "LBL", Series: "SRS", Runtime: 120,
			ReleaseDate:      &releaseDate,
			Rating:           &models.Rating{Score: 8.5, Votes: 100},
			PosterURL:        "https://example.com/poster.jpg",
			CoverURL:         "https://example.com/cover.jpg",
			TrailerURL:       "https://example.com/trailer.mp4",
			Actresses:        []models.ActressInfo{{DMMID: 1, JapaneseName: "Test"}},
			Genres:           []string{"Genre1"},
			ScreenshotURL:    []string{"https://example.com/screen1.jpg"},
			ShouldCropPoster: true,
		},
	}
	return agg, results
}
