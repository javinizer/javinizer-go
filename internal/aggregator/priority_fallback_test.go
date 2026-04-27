package aggregator

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateWithPerFieldOverrideExcludingSource(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev", "mgstage", "libredmm"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Fields: map[string][]string{
					"id":         {"dmm", "r18dev", "libredmm"},
					"content_id": {"dmm", "r18dev", "libredmm"},
					"title":      {"dmm", "r18dev", "libredmm"},
					"maker":      {"dmm", "r18dev", "libredmm"},
					"actress":    {"dmm", "r18dev", "libredmm"},
				},
			},
		},
	}

	agg := New(cfg)
	t.Logf("resolvedPriorities[ID] = %v", agg.resolvedPriorities["ID"])
	t.Logf("resolvedPriorities[Title] = %v", agg.resolvedPriorities["Title"])
	t.Logf("resolvedPriorities[Actress] = %v", agg.resolvedPriorities["Actress"])

	releaseDate := time.Date(2025, 5, 31, 0, 0, 0, 0, time.UTC)

	results := []*models.ScraperResult{
		{
			Source:      "mgstage",
			ID:          "200GANA-3215",
			ContentID:   "200GANA-3215",
			Title:       "マジ軟派、初撮。 2172",
			Maker:       "ナンパTV",
			ReleaseDate: &releaseDate,
			Actresses: []models.ActressInfo{
				{JapaneseName: "テスト女優"},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, "200GANA-3215", movie.ID, "ID should fall back to mgstage when per-field sources have no data")
	assert.Equal(t, "200GANA-3215", movie.ContentID)
	assert.Equal(t, "マジ軟派、初撮。 2172", movie.Title)
	assert.Equal(t, "ナンパTV", movie.Maker)
	assert.Equal(t, 1, len(movie.Actresses), "Actresses should fall back to mgstage")
}

func TestAggregatePerFieldPreference(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev", "mgstage"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Fields: map[string][]string{
					"title": {"mgstage", "dmm"},
				},
			},
		},
	}

	agg := New(cfg)

	results := []*models.ScraperResult{
		{
			Source: "dmm",
			ID:     "200GANA-3215",
			Title:  "DMM Title",
		},
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "MGStage Title",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, "MGStage Title", movie.Title, "Per-field override should set mgstage as preferred for title")
}

func TestUnknownActressFilteredFromScraperResults(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"mgstage"},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				UnknownActressText: "Unknown",
			},
		},
	}

	agg := New(cfg)

	results := []*models.ScraperResult{
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "マジ軟派、初撮。 2172",
			Actresses: []models.ActressInfo{
				{FirstName: "Unknown"},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, 0, len(movie.Actresses), "Actress named 'Unknown' should be filtered out")
}

func TestUnknownActressJapaneseNameFiltered(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"mgstage"},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				UnknownActressText: "Unknown",
			},
		},
	}

	agg := New(cfg)

	results := []*models.ScraperResult{
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "マジ軟派、初撮。 2172",
			Actresses: []models.ActressInfo{
				{JapaneseName: "Unknown"},
				{JapaneseName: "テスト女優"},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, 1, len(movie.Actresses), "Only non-Unknown actress should remain")
	assert.Equal(t, "テスト女優", movie.Actresses[0].JapaneseName)
}

func TestIsUnknownActress(t *testing.T) {
	tests := []struct {
		name        string
		info        models.ActressInfo
		unknownText string
		want        bool
	}{
		{"first name unknown", models.ActressInfo{FirstName: "Unknown"}, "unknown", true},
		{"japanese name unknown", models.ActressInfo{JapaneseName: "Unknown"}, "unknown", true},
		{"last name unknown", models.ActressInfo{LastName: "Unknown"}, "unknown", true},
		{"case insensitive", models.ActressInfo{FirstName: "UNKNOWN"}, "unknown", true},
		{"normal name", models.ActressInfo{JapaneseName: "テスト女優"}, "unknown", false},
		{"empty unknown text", models.ActressInfo{FirstName: "Unknown"}, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nameKey := resolveNameKey(tc.info.JapaneseName, tc.info.FirstName, tc.info.LastName)
			got := isUnknownActress(tc.info, nameKey, tc.unknownText)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMergePriorityLists(t *testing.T) {
	tests := []struct {
		name     string
		perField []string
		global   []string
		expected []string
	}{
		{"empty both", nil, nil, []string{}},
		{"empty per-field", nil, []string{"a", "b"}, []string{"a", "b"}},
		{"empty global", []string{"a", "b"}, nil, []string{"a", "b"}},
		{"no overlap", []string{"a"}, []string{"b"}, []string{"a", "b"}},
		{"full overlap", []string{"a", "b"}, []string{"a", "b"}, []string{"a", "b"}},
		{"partial overlap", []string{"a", "b"}, []string{"b", "c"}, []string{"a", "b", "c"}},
		{"per-field excludes source", []string{"dmm", "r18dev", "libredmm"}, []string{"dmm", "r18dev", "mgstage", "libredmm"}, []string{"dmm", "r18dev", "libredmm", "mgstage"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergePriorityLists(tc.perField, tc.global)
			if tc.expected == nil {
				assert.Nil(t, got)
			} else {
				assert.Equal(t, tc.expected, got)
			}
		})
	}
}
