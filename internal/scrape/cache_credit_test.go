package scrape

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestTryCacheLoadsPersistedCredits(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	movie := &models.Movie{ContentID: "cache-credit", ID: "CACHE-CREDIT", Title: "Cached", Credits: []models.MovieCredit{{CreditedName: "Reported", Scraped: models.Actress{FirstName: "Candidate"}}}}
	_, err = repos.MovieRepo.Upsert(t.Context(), movie)
	require.NoError(t, err)
	credits, err := repos.MovieCreditRepo.ListByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, credits, 1)
	require.NoError(t, repos.MovieCreditRepo.UpdateSuppressed(t.Context(), credits[0].ID, true))

	s := &Scraper{movieRepo: repos.MovieRepo, cfg: &Config{}}
	result := s.tryCache(t.Context(), ScrapeCmd{MovieID: movie.ID}, nil, time.Now())
	require.NotNil(t, result)
	require.Len(t, result.Movie.Credits, 1)
	require.True(t, result.Movie.Credits[0].Suppressed)
	require.NotNil(t, result.Movie.Credits[0].Actress)
}
