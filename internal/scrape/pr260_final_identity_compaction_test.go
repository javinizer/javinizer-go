package scrape

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestBuildCreditsCompactsStableIdentityComponents(t *testing.T) {
	for _, actresses := range [][]models.Actress{
		{{FirstName: "Alpha"}, {DMMID: 983001, FirstName: "Alpha", JapaneseName: "アルファ", ThumbURL: "rich.jpg"}},
		{{DMMID: 983001, FirstName: "Alpha", JapaneseName: "アルファ", ThumbURL: "rich.jpg"}, {FirstName: "Alpha"}},
	} {
		movie := &models.Movie{Actresses: actresses}
		BuildCreditsFromScrape(movie, map[string]string{"dmmid:983001": "rich"}, []*models.ScraperResult{{Source: "rich", Actresses: []models.ActressInfo{{DMMID: 983001, FirstName: "Alpha", JapaneseName: "アルファ", ThumbURL: "reported.jpg"}}}})
		require.Len(t, movie.Actresses, 1)
		require.Len(t, movie.Credits, 1)
		require.Equal(t, 983001, movie.Actresses[0].DMMID)
		require.Equal(t, "Alpha", movie.Actresses[0].FirstName)
		require.Equal(t, "アルファ", movie.Actresses[0].JapaneseName)
		require.Equal(t, "rich.jpg", movie.Actresses[0].ThumbURL)
		require.Equal(t, movie.Actresses[0], movie.Credits[0].Scraped)
		require.Equal(t, "rich", movie.Credits[0].Source)
		require.Equal(t, "reported.jpg", movie.Credits[0].ReportedThumbURL)
	}
}

func TestBuildCreditsDoesNotMergeConflictingDMMIdentitiesWithSharedName(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 983011, FirstName: "Shared", LastName: "Name"}, {DMMID: 983012, FirstName: "Shared", LastName: "Name"}}}
	BuildCreditsFromScrape(movie, nil, nil)
	require.Len(t, movie.Actresses, 2)
	require.Len(t, movie.Credits, 2)
	require.Equal(t, []int{983011, 983012}, []int{movie.Actresses[0].DMMID, movie.Actresses[1].DMMID})
}

func TestBuildCreditsDropsAmbiguousPartialFromConflictingDMMIdentities(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{FirstName: "Shared", LastName: "Name"}, {DMMID: 983013, FirstName: "Shared", LastName: "Name"}, {DMMID: 983014, FirstName: "Shared", LastName: "Name"}}}
	BuildCreditsFromScrape(movie, nil, nil)
	require.Len(t, movie.Actresses, 2)
	require.Len(t, movie.Credits, 2)
}

func TestBuildCreditsCompactsTransitiveStableIdentityOverlap(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{FirstName: "Alpha"}, {FirstName: "Alpha", JapaneseName: "アルファ"}, {DMMID: 983021, JapaneseName: "アルファ", ThumbURL: "rich.jpg"}}}
	BuildCreditsFromScrape(movie, nil, nil)
	require.Len(t, movie.Actresses, 1)
	require.Len(t, movie.Credits, 1)
	require.Equal(t, models.Actress{DMMID: 983021, FirstName: "Alpha", JapaneseName: "アルファ", ThumbURL: "rich.jpg"}, movie.Actresses[0])
	require.Equal(t, movie.Actresses[0], movie.Credits[0].Scraped)
}

func TestBuildCreditsFillsMissingFirstNameFromOverlappingEvidence(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{JapaneseName: "アルファ"}, {FirstName: "Alpha", JapaneseName: "アルファ"}}}
	BuildCreditsFromScrape(movie, nil, nil)
	require.Len(t, movie.Actresses, 1)
	require.Equal(t, "Alpha", movie.Actresses[0].FirstName)
}

func TestCompactedEnrichedActressTranslationPersistsToResolvedIdentity(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	movie := &models.Movie{ID: "COMPACT-PERSIST", ContentID: "compact-persist", Actresses: []models.Actress{{FirstName: "Alpha"}, {DMMID: 983031, FirstName: "Alpha", ThumbURL: "rich.jpg"}}}
	processed, err := postProcessScraped(context.Background(), movie, []*models.ScraperResult{{Source: "rich", Actresses: []models.ActressInfo{{DMMID: 983031, FirstName: "Alpha", ThumbURL: "rich.jpg"}}}}, nil, &Config{TranslationEnabled: true}, indexedActressTranslator{}, nil, ScrapeCmd{MovieID: movie.ID}, time.Now())
	require.NoError(t, err)
	require.Len(t, processed.Movie.Actresses, 1)
	require.Len(t, processed.Movie.Credits, 1)
	saved, err := database.NewMovieRepository(db).UpsertWithTranslations(t.Context(), processed.Movie, nil, processed.TranslationOutput.ActressTranslations)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	translation, err := db.Repositories().ActressTranslationRepo.FindByActressAndLanguage(t.Context(), saved.Credits[0].ActressID, "en")
	require.NoError(t, err)
	require.Equal(t, "Alpha EN", translation.DisplayName)
}
