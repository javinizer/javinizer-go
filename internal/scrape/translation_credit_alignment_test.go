package scrape

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/stretchr/testify/require"
)

type indexedActressTranslator struct{}

func (indexedActressTranslator) Translate(_ context.Context, movie *models.Movie) (string, string, bool, *translation.TranslationOutput) {
	data := make([]models.ActressTranslationData, len(movie.Actresses))
	for i := range movie.Actresses {
		data[i] = models.ActressTranslationData{
			ActressIndex: i,
			Language:     "en",
			DisplayName:  movie.Actresses[i].FirstName + " EN",
			SourceName:   "translation:test",
		}
	}
	return "", "", true, &translation.TranslationOutput{ActressTranslations: data}
}

func TestPostProcessTranslationPersistenceKeepsDeduplicatedActressIndicesAligned(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))

	movie := &models.Movie{
		ID:        "ALIGN-001",
		ContentID: "align-001",
		Actresses: []models.Actress{
			{DMMID: 771001, FirstName: "Alpha"},
			{DMMID: 771001, FirstName: "Alpha duplicate"},
			{DMMID: 771002, FirstName: "Beta"},
		},
	}
	results := []*models.ScraperResult{{
		Source: "provider-a",
		Actresses: []models.ActressInfo{
			{DMMID: 771001, FirstName: "Alpha"},
			{DMMID: 771002, FirstName: "Beta"},
		},
	}}

	processed, err := postProcessScraped(t.Context(), movie, results, nil, &Config{TranslationEnabled: true}, indexedActressTranslator{}, nil, ScrapeCmd{MovieID: movie.ID}, time.Now())
	require.NoError(t, err)
	require.Len(t, processed.Movie.Actresses, 2)
	require.Len(t, processed.Movie.Credits, 2)
	require.Equal(t, "provider-a", processed.Movie.Credits[0].Source)
	require.Equal(t, "provider-a", processed.Movie.Credits[1].Source)
	require.Len(t, processed.TranslationOutput.ActressTranslations, 2)

	saved, err := database.NewMovieRepository(db).UpsertWithTranslations(t.Context(), processed.Movie, processed.TranslationOutput.GenreTranslations, processed.TranslationOutput.ActressTranslations)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 2)

	translations := db.Repositories().ActressTranslationRepo
	alpha, err := translations.FindByActressAndLanguage(t.Context(), saved.Credits[0].ActressID, "en")
	require.NoError(t, err)
	beta, err := translations.FindByActressAndLanguage(t.Context(), saved.Credits[1].ActressID, "en")
	require.NoError(t, err)
	require.Equal(t, "Alpha EN", alpha.DisplayName)
	require.Equal(t, "Beta EN", beta.DisplayName)
	require.Equal(t, "translation:test", beta.SourceName)
}

func TestBuildCreditsCompactsUnidentifiableAndDuplicateActresses(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{
		{},
		{DMMID: 1, FirstName: "First"},
		{DMMID: 1, FirstName: "Duplicate"},
		{DMMID: 2, FirstName: "Second"},
	}}
	BuildCreditsFromScrape(movie, map[string]string{"dmmid:1": "one", "dmmid:2": "two"}, nil)
	require.Equal(t, []int{1, 2}, []int{movie.Actresses[0].DMMID, movie.Actresses[1].DMMID})
	require.Len(t, movie.Credits, 2)
	require.Equal(t, "one", movie.Credits[0].Source)
	require.Equal(t, "two", movie.Credits[1].Source)
}

func TestPostProcessExplicitCreditsPreserveCallerActressOrdering(t *testing.T) {
	actresses := []models.Actress{{DMMID: 1, FirstName: "First"}, {DMMID: 1, FirstName: "Duplicate"}}
	credits := []models.MovieCredit{{CreditedName: "Caller", Origin: string(models.CreditOriginUser)}}
	movie := &models.Movie{ID: "EXPLICIT-ORDER", Actresses: actresses, Credits: credits}
	processed, err := postProcessScraped(t.Context(), movie, []*models.ScraperResult{{Source: "raw", Actresses: []models.ActressInfo{{DMMID: 1}}}}, nil, &Config{}, nil, nil, ScrapeCmd{MovieID: movie.ID}, time.Now())
	require.NoError(t, err)
	require.Equal(t, actresses, processed.Movie.Actresses)
	require.Equal(t, credits, processed.Movie.Credits)
}
