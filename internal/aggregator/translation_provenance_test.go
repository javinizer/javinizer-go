package aggregator

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/formatter"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestTranslationProvenanceTracksFirstRetainedField(t *testing.T) {
	// Without a title, provenance follows the first nonempty field in this
	// documented order, not the order in which scraper results arrive.
	keys := []string{"title", "original_title", "description", "director", "maker", "label", "series"}
	for primary := range keys {
		for _, reverse := range []bool{false, true} {
			for _, custom := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/reverse=%v/custom=%v", keys[primary], reverse, custom), func(t *testing.T) {
					fields := map[string][]string{}
					for i, key := range keys {
						if i < primary {
							fields[key] = []string{"missing"}
						} else if i == primary {
							fields[key] = []string{"high"}
						} else {
							fields[key] = []string{"low"}
						}
					}
					agg := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"high", "low"}, Fields: fields}}}, nil, nil, nil)
					results := []*models.ScraperResult{}
					for _, source := range []string{"low", "high"} {
						results = append(results, &models.ScraperResult{Source: source, Translations: []models.MovieTranslation{{Language: "ja", Title: source + " title", OriginalTitle: source + " original", Description: source + " description", Director: source + " director", Maker: source + " maker", Label: source + " label", Series: source + " series"}}})
					}
					if reverse {
						results[0], results[1] = results[1], results[0]
					}
					var movie *models.Movie
					var err error
					if custom {
						movie, _, err = agg.AggregateWithPriority(results, []string{"low", "high"})
					} else {
						movie, _, err = agg.Aggregate(results)
					}
					require.NoError(t, err)
					require.Len(t, movie.Translations, 1)
					require.Equal(t, "high", movie.Translations[0].SourceName)
					require.Equal(t, "high", contracts.MovieTranslationViewFromModel(&movie.Translations[0]).SourceName)
					// Exercise the actual CLI/TUI formatter, including its language label
					// and source-list consumers, without reading files or contacting scrapers.
					movie.Translations = append(movie.Translations, models.MovieTranslation{Language: "en", Title: "English", SourceName: "high"})
					var output bytes.Buffer
					formatter.WriteMovie(&output, movie, nil)
					require.Contains(t, output.String(), "Japanese (high)")
					require.NotContains(t, output.String(), "Japanese (low)")
					require.Contains(t, output.String(), "Sources")
				})
			}
		}
	}
}

func TestTranslationProvenanceFullReplacement(t *testing.T) {
	agg := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"high", "low"}}}}, nil, nil, nil)
	for _, reverse := range []bool{false, true} {
		results := []*models.ScraperResult{
			{Source: "low", Translations: []models.MovieTranslation{{Language: "ja", Title: "low title", Description: "low description", SourceName: "low"}}},
			{Source: "high", Translations: []models.MovieTranslation{{Language: "ja", Title: "high title", Description: "high description", SourceName: "high"}}},
		}
		if reverse {
			results[0], results[1] = results[1], results[0]
		}
		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.Len(t, movie.Translations, 1)
		require.Equal(t, "high title", movie.Translations[0].Title)
		require.Equal(t, "high description", movie.Translations[0].Description)
		require.Equal(t, "high", movie.Translations[0].SourceName)
		require.Equal(t, "high", contracts.MovieTranslationViewFromModel(&movie.Translations[0]).SourceName)
	}
}

func TestTranslationProvenancePreservesWinningProviderLabel(t *testing.T) {
	agg := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"high", "low"}}}}, nil, nil, nil)
	for _, label := range []string{"provider:high", ""} {
		for _, reverse := range []bool{false, true} {
			results := []*models.ScraperResult{
				{Source: "low", Translations: []models.MovieTranslation{{Language: "ja", Title: "low title", SourceName: "provider:low"}}},
				{Source: "high", Language: "ja", Title: "legacy title", Translations: []models.MovieTranslation{{Language: "ja", Title: "explicit title", SourceName: label}}},
			}
			if reverse {
				results[0], results[1] = results[1], results[0]
			}
			movie, _, err := agg.Aggregate(results)
			require.NoError(t, err)
			require.Len(t, movie.Translations, 1)
			want := label
			if want == "" {
				want = "high"
			}
			require.Equal(t, want, movie.Translations[0].SourceName)
			require.Equal(t, "explicit title", movie.Translations[0].Title, "legacy same-source tie must not replace explicit title or provider")
			require.Equal(t, want, contracts.MovieTranslationViewFromModel(&movie.Translations[0]).SourceName)
		}
	}
}
