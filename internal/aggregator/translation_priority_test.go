package aggregator

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/require"
)

func TestTranslationPriorityPerFieldAndResultOrder(t *testing.T) {
	permutations := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	for _, mode := range []string{"legacy", "explicit", "mixed"} {
		for _, order := range permutations {
			for _, custom := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%v/custom=%v", mode, order, custom), func(t *testing.T) {
					agg := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{
						Priority: []string{"a", "b", "c"}, Fields: map[string][]string{
							"title": {"c"}, "original_title": {"b"}, "description": {"a"},
							"director": {"b"}, "maker": {"a"}, "label": {"c"}, "series": {"b"},
						},
					}}}, nil, nil, nil)
					var results []*models.ScraperResult
					for _, i := range order {
						source := []string{"a", "b", "c"}[i]
						tr := models.MovieTranslation{Language: "ja", Title: source + " title", OriginalTitle: source + " original", Description: source + " description", Director: source + " director", Maker: source + " maker", Label: source + " label", Series: source + " series"}
						r := &models.ScraperResult{Source: source, Language: "ja", Title: tr.Title, OriginalTitle: tr.OriginalTitle, Description: tr.Description, Director: tr.Director, Maker: tr.Maker, Label: tr.Label, Series: tr.Series}
						if mode == "explicit" || mode == "mixed" && i != 1 {
							r.Translations = []models.MovieTranslation{tr}
						}
						if mode == "explicit" {
							r.Language = ""
						}
						results = append(results, r)
					}
					var movie *models.Movie
					var err error
					if custom {
						movie, _, err = agg.AggregateWithPriority(results, []string{"c", "b", "a"})
					} else {
						movie, _, err = agg.Aggregate(results)
					}
					require.NoError(t, err)
					require.Len(t, movie.Translations, 1)
					got := movie.Translations[0]
					require.Equal(t, []string{"c title", "b original", "a description", "b director", "a maker", "c label", "b series"}, []string{got.Title, got.OriginalTitle, got.Description, got.Director, got.Maker, got.Label, got.Series})
					require.Equal(t, movie.Title, got.Title)
					require.Equal(t, "c", got.SourceName)
					ctx := template.NewContextFromMovie(movie)
					ctx.DefaultLanguage = "ja"
					for _, format := range []string{"<TITLE:JA>", "<TITLE>"} {
						rendered, err := template.NewEngine().Execute(format, ctx)
						require.NoError(t, err)
						require.Equal(t, "c title", rendered)
					}
				})
			}
		}
	}
}

func TestTranslationPriorityInheritanceAndAlternativeLanguages(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprintf("custom=%v", custom), func(t *testing.T) {
			agg := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{
				Priority: []string{"a", "b"}, Fields: map[string][]string{"title": {}, "series": {"absent"}},
			}}}, nil, nil, nil)
			results := []*models.ScraperResult{
				{Source: "b", Language: "ja", Title: "b title", Maker: "shared", Translations: []models.MovieTranslation{
					{Language: "ja", Title: "b title", Description: "b description", Series: "excluded"},
					{Language: "en", Title: "English", SourceName: "b"},
					{Language: "en", Title: "later duplicate", Description: "English description"},
				}},
				{Source: "unlisted", Translations: []models.MovieTranslation{{Language: "zh", Title: "excluded"}, {Language: "ja", Title: "excluded", Series: "excluded"}}},
				{Source: "a", Language: "ja", Title: "a title", Maker: "shared", Translations: []models.MovieTranslation{{Language: "ja", Title: "a title"}, {Language: "fr", Title: "French"}}},
			}
			var movie *models.Movie
			var err error
			wantTitle := "a title"
			if custom {
				movie, _, err = agg.AggregateWithPriority(results, []string{"b", "a"})
				wantTitle = "b title"
			} else {
				movie, _, err = agg.Aggregate(results)
			}
			require.NoError(t, err)
			require.Len(t, movie.Translations, 3)
			require.Equal(t, []string{"ja", "en", "fr"}, []string{movie.Translations[0].Language, movie.Translations[1].Language, movie.Translations[2].Language})
			require.Equal(t, wantTitle, movie.Translations[0].Title)
			require.Equal(t, "b description", movie.Translations[0].Description, "empty preferred values fall back within priority")
			require.Empty(t, movie.Translations[0].Series)
			require.Equal(t, "English", movie.Translations[1].Title)
			require.Equal(t, "English description", movie.Translations[1].Description)
			require.Equal(t, "b", movie.Translations[1].SourceName)
			require.Equal(t, "French", movie.Translations[2].Title)
			require.Equal(t, "b title", results[0].Translations[0].Title, "merge must not mutate scraper inputs")
			require.Equal(t, "excluded", results[0].Translations[0].Series)
		})
	}
}

func TestSourceMetadataUsesTitlePriorityConsistently(t *testing.T) {
	for _, titlePriority := range [][]string{{"b"}, {"missing", "b"}, {"missing"}, {}} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/reverse=%v", titlePriority, reverse), func(t *testing.T) {
				agg := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"a", "b"}, Fields: map[string][]string{"title": titlePriority}}}}, nil, nil, nil)
				// Source metadata intentionally uses the first available result in title
				// priority, even if it has no title. This is not field-level provenance.
				a := &models.ScraperResult{Source: "a", Title: "a title", SourceURL: "https://example.invalid/a"}
				b := &models.ScraperResult{Source: "b", SourceURL: "https://example.invalid/b"}
				results := []*models.ScraperResult{a, b}
				if reverse {
					results = []*models.ScraperResult{b, a}
				}
				normal, _, err := agg.Aggregate(results)
				require.NoError(t, err)
				custom, _, err := agg.AggregateWithPriority(results, []string{"a", "b"})
				require.NoError(t, err)
				want := b
				if len(titlePriority) == 0 {
					want = a
				} else if len(titlePriority) == 1 && titlePriority[0] == "missing" {
					want = results[0]
				}
				require.Equal(t, want.Source, normal.SourceName)
				require.Equal(t, want.SourceURL, normal.SourceURL)
				require.Equal(t, normal.SourceName, custom.SourceName)
				require.Equal(t, normal.SourceURL, custom.SourceURL)
			})
		}
	}
}

func TestTranslationPriorityExcludesEachField(t *testing.T) {
	for _, field := range []struct{ key, name string }{{"title", "Title"}, {"original_title", "OriginalTitle"}, {"description", "Description"}, {"director", "Director"}, {"maker", "Maker"}, {"label", "Label"}, {"series", "Series"}} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit=%v", field.key, explicit), func(t *testing.T) {
				agg := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"a"}, Fields: map[string][]string{field.key: {"missing"}}}}}, nil, nil, nil)
				r := &models.ScraperResult{Source: "a", Language: "ja", Title: "title", OriginalTitle: "original", Description: "description", Director: "director", Maker: "maker", Label: "label", Series: "series"}
				if explicit {
					r.Translations = []models.MovieTranslation{{Language: "ja", Title: "title", OriginalTitle: "original", Description: "description", Director: "director", Maker: "maker", Label: "label", Series: "series"}}
					r.Language = ""
				}
				movie, _, err := agg.Aggregate([]*models.ScraperResult{r})
				require.NoError(t, err)
				require.Len(t, movie.Translations, 1)
				require.Empty(t, reflect.ValueOf(movie.Translations[0]).FieldByName(field.name).String())
				require.Empty(t, reflect.ValueOf(*movie).FieldByName(field.name).String())
			})
		}
	}
}

func TestTranslationPriorityDoesNotChangeActorEvidence(t *testing.T) {
	agg := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"a", "b"}, Fields: map[string][]string{"title": {"b"}, "actress": {"a"}}}}}, nil, nil, nil)
	selected := models.ActressInfo{JapaneseName: "Selected actor", DMMID: 42, ThumbURL: "selected.jpg"}
	results := []*models.ScraperResult{
		{Source: "b", Language: "ja", Title: "winning title", Maker: "shared", Actresses: []models.ActressInfo{{JapaneseName: "Other actor", DMMID: 43}}},
		{Source: "a", Language: "ja", Title: "losing title", Maker: "shared", Actresses: []models.ActressInfo{selected}},
	}
	movie, info, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.Equal(t, "winning title", movie.Translations[0].Title)
	require.Equal(t, "b", info.FieldSources["title"])
	require.Len(t, movie.Actresses, 1)
	source, actor, rank := movie.Actresses[0].ScrapeEvidence()
	require.Equal(t, "a", source)
	require.Equal(t, selected, actor)
	require.Zero(t, rank)
}
