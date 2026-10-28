package aggregator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func outputIntegrityDatabase(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "metadata.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return db
}

func TestAggregateLiteralReplacementPreservesLeadingLatin(t *testing.T) {
	db := outputIntegrityDatabase(t)
	repo := database.NewWordReplacementRepository(db)
	require.NoError(t, repo.Create(context.Background(), &models.WordReplacement{
		Original: "X***", Replacement: "Replacement", MatchMode: models.MatchModeLiteral,
	}))
	cfg := ConfigFromAppConfig(&config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	})
	agg := New(cfg, nil, NewWordProcessor(cfg.Metadata, repo), nil)
	input := &models.ScraperResult{
		Source: "r18dev", Language: "en", ID: "IPX-001",
		Title: "preX*** X*** éX*** 日X***",
	}
	original := input.Clone()

	movie, result, err := agg.Aggregate([]*models.ScraperResult{input})
	require.NoError(t, err)
	require.NotNil(t, movie)
	require.NotNil(t, result)
	// Reject both ASCII and multibyte Latin prefixes, but keep scanning so
	// standalone tokens and tokens immediately after CJK text still replace.
	require.Equal(t, "preX*** Replacement éX*** 日Replacement", movie.Title)
	require.Equal(t, "r18dev", result.FieldSources["title"])
	require.Equal(t, original, input, "replacement must not rewrite the retained scraper evidence")
}

func TestAggregateRatingDiagnosticSurvivesFallback(t *testing.T) {
	for _, entrypoint := range []string{"Aggregate", "AggregateWithPriority"} {
		for _, tc := range []struct {
			name                string
			preferred, fallback models.Rating
			wantScore           float64
			wantVotes           int
			wantSource          string
			wantRejected        []string
		}{
			{
				name:      "invalid preferred uses valid fallback",
				preferred: models.Rating{Score: 85, Votes: 100}, fallback: models.Rating{Score: 7.5, Votes: 200},
				wantScore: 7.5, wantVotes: 200, wantSource: "dmm", wantRejected: []string{"r18dev(85.00)"},
			},
			{
				name:      "all invalid leaves rating and provenance absent",
				preferred: models.Rating{Score: 85, Votes: 100}, fallback: models.Rating{Score: 50, Votes: 200},
				wantRejected: []string{"r18dev(85.00)", "dmm(50.00)"},
			},
			{
				name:      "all valid uses preferred without warning",
				preferred: models.Rating{Score: 6, Votes: 100}, fallback: models.Rating{Score: 7.5, Votes: 200},
				wantScore: 6, wantVotes: 100, wantSource: "r18dev",
			},
		} {
			t.Run(entrypoint+"/"+tc.name, func(t *testing.T) {
				priority := []string{"r18dev", "dmm"}
				configuredPriority := priority
				if entrypoint == "AggregateWithPriority" {
					// The caller's override must win over the configured global order.
					configuredPriority = []string{"dmm", "r18dev"}
				}
				cfg := ConfigFromAppConfig(&config.Config{
					Scrapers: config.ScrapersConfig{Priority: configuredPriority},
				})
				agg := New(cfg, nil, nil, nil)
				// Input order must not decide which source supplies the rating.
				inputs := []*models.ScraperResult{
					{Source: "dmm", Language: "ja", ID: "IPX-001", Title: "Fallback", Rating: &tc.fallback},
					{Source: "r18dev", Language: "en", ID: "IPX-001", Title: "Preferred", Rating: &tc.preferred},
				}
				originals := []*models.ScraperResult{inputs[0].Clone(), inputs[1].Clone()}
				var movie *models.Movie
				var result *AggregateResult
				var err error
				if entrypoint == "Aggregate" {
					movie, result, err = agg.Aggregate(inputs)
				} else {
					movie, result, err = agg.AggregateWithPriority(inputs, priority)
				}
				require.NoError(t, err)
				require.NotNil(t, movie)
				require.NotNil(t, result)
				require.Equal(t, tc.wantScore, movie.RatingScore)
				require.Equal(t, tc.wantVotes, movie.RatingVotes)
				for _, field := range []string{"rating_score", "rating_votes"} {
					if tc.wantSource == "" {
						require.NotContains(t, result.FieldSources, field)
					} else {
						require.Equal(t, tc.wantSource, result.FieldSources[field])
					}
				}
				if len(tc.wantRejected) == 0 {
					require.Empty(t, movie.RatingWarning)
				} else {
					for _, rejected := range tc.wantRejected {
						require.Contains(t, movie.RatingWarning, rejected)
					}
					if tc.wantSource != "" {
						require.NotContains(t, movie.RatingWarning, tc.wantSource, "accepted source must not be reported as rejected")
					}
				}
				require.Equal(t, originals, inputs, "rating selection must not sanitize the original scraper evidence")
			})
		}
	}
}

func TestAggregateAliasCacheSurvivesCanceledReload(t *testing.T) {
	db := outputIntegrityDatabase(t)
	repo := database.NewActressAliasRepository(db)
	require.NoError(t, repo.Create(context.Background(), &models.ActressAlias{
		AliasName: "旧名花子", CanonicalName: "正名花子",
	}))
	cfg := ConfigFromAppConfig(&config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{Enabled: true, ConvertAlias: true},
		},
	})
	agg := New(cfg, nil, nil, NewAliasResolver(cfg.Metadata, repo))
	assertNames := func(want []string) {
		t.Helper()
		// Fresh inputs prevent a prior aggregation's output from hiding a lost
		// cache entry or an accidental mutation of scraper evidence.
		input := &models.ScraperResult{
			Source: "r18dev", Language: "ja", ID: "IPX-001", Title: "Alias cache",
			Actresses: []models.ActressInfo{{JapaneseName: "旧名花子"}, {JapaneseName: "旧名葵"}},
		}
		original := input.Clone()
		movie, _, err := agg.Aggregate([]*models.ScraperResult{input})
		require.NoError(t, err)
		require.NotNil(t, movie)
		names := make([]string, len(movie.Actresses))
		for i, actress := range movie.Actresses {
			names[i] = actress.JapaneseName
		}
		require.ElementsMatch(t, want, names)
		require.Equal(t, original, input)
	}
	assertNames([]string{"正名花子", "旧名葵"})

	// Add a distinct alias through the real repository; do not reassign an
	// existing alias's canonical owner or tamper with the resolver's cache.
	require.NoError(t, repo.Create(context.Background(), &models.ActressAlias{
		AliasName: "旧名葵", CanonicalName: "正名葵",
	}))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	agg.ReloadReplacementCaches(canceled)
	assertNames([]string{"正名花子", "旧名葵"})

	agg.ReloadReplacementCaches(context.Background())
	assertNames([]string{"正名花子", "正名葵"})
}
