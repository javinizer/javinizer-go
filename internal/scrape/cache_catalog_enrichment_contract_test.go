package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCachedSparseCreditEnrichmentDoesNotRewriteIdentityOrEvidence(t *testing.T) {
	f := newFixture(t)
	repo := database.NewActressRepository(f.db)
	// A sparse DMM-less credit is not permission to relink to a catalog identity.
	movie := &models.Movie{ContentID: "cache-sparse", ID: "CACHE-SPARSE", Title: "Cached sparse credit", SourceName: "reported", Credits: []models.MovieCredit{{CreditedName: "報告名", CreditedJapaneseName: "報告名", Source: "reported", Origin: string(models.CreditOriginScrape), Scraped: models.Actress{JapaneseName: "報告名"}}}}
	saved, err := f.movieRepo.Upsert(t.Context(), movie)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	candidateID := saved.Credits[0].ActressID
	verified := models.Actress{DMMID: 927031, JapaneseName: "報告名", FirstName: "Catalog", LastName: "Name", ThumbURL: "https://example.test/catalog.jpg", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, repo.Create(t.Context(), &verified))
	before, err := f.movieRepo.FindByID(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Len(t, before.Actresses, 1)
	require.Empty(t, before.Actresses[0].ThumbURL)
	f.cfg.Metadata.ActressDatabase.Enabled = true
	engine := f.build()
	result, err := engine.Scrape(t.Context(), ScrapeCmd{MovieID: movie.ID})
	require.NoError(t, err)
	require.True(t, result.Cached)
	require.False(t, result.NeedsPersistence)
	require.True(t, result.Movie.SkipCreditReconcile)
	require.Len(t, result.Movie.Actresses, 1)
	require.Equal(t, candidateID, result.Movie.Actresses[0].ID)
	require.Zero(t, result.Movie.Actresses[0].DMMID)
	require.Equal(t, verified.ThumbURL, result.Movie.Actresses[0].ThumbURL)
	require.Equal(t, verified.FirstName, result.Movie.Actresses[0].FirstName)
	require.Equal(t, verified.LastName, result.Movie.Actresses[0].LastName)
	require.Equal(t, before.Credits, result.Movie.Credits, "presentation enrichment must preserve raw credit evidence and identity")
	after, err := f.movieRepo.FindByID(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "Scrape cache reads must not persist enrichment")
}
