package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/require"
)

func TestFreshScrapeDisplayTitleMatchesPersistedCredits(t *testing.T) {
	for _, skipPersist := range []bool{false, true} {
		t.Run(map[bool]string{false: "inline", true: "deferred"}[skipPersist], func(t *testing.T) {
			db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			canonical := models.Actress{DMMID: 71001, FirstName: "Canonical", LastName: "Person", Verified: true, Origin: "user"}
			require.NoError(t, db.Create(&canonical).Error)
			repo := database.NewMovieRepository(db)
			movie := &models.Movie{ID: "AUDIT-001", ContentID: "audit001", Title: "Synthetic", Actresses: []models.Actress{canonical}}
			scrape.BuildCreditsFromScrape(movie, nil, nil)
			require.Len(t, movie.Credits, 1)
			require.Nil(t, movie.Credits[0].Actress)
			engine := template.NewEngine()
			nameCfg := nfo.NFONameConfig{FirstNameOrder: true}
			orch := newScrapeOrchestrator(&mockScraperScrape{result: &scrape.ScrapeResult{Movie: movie, Status: scrape.StatusCompleted}}, repo, "<ID> <ACTORS>", engine, nameCfg, nil)
			fresh, meta, err := orch.Execute(t.Context(), scrape.ScrapeCmd{MovieID: movie.ID, SkipPersist: skipPersist})
			require.NoError(t, err)
			if skipPersist {
				require.False(t, meta.Persisted)
				require.NotNil(t, meta.RenderDisplayTitle)
				fresh.Movie, err = database.UpsertWithDisplayTitle(t.Context(), repo, fresh.Movie, nil, nil, meta.RenderDisplayTitle)
				require.NoError(t, err)
			} else {
				require.True(t, meta.Persisted)
			}
			require.NotNil(t, fresh.Movie.Credits[0].Actress)
			stored, err := repo.FindByID(t.Context(), movie.ID)
			require.NoError(t, err)
			require.Equal(t, "AUDIT-001 Canonical Person", stored.DisplayTitle)
			cachedOrch := newScrapeOrchestrator(&mockScraperScrape{result: &scrape.ScrapeResult{Movie: stored, Status: scrape.StatusCompleted}}, repo, "<ID> <ACTORS>", engine, nameCfg, nil)
			cached, _, err := cachedOrch.Execute(t.Context(), scrape.ScrapeCmd{MovieID: movie.ID})
			require.NoError(t, err)
			require.Equal(t, "AUDIT-001 Canonical Person", cached.Movie.DisplayTitle, "cache-hit control")
			t.Logf("fresh title=%q cached title=%q", fresh.Movie.DisplayTitle, cached.Movie.DisplayTitle)
			require.Equal(t, cached.Movie.DisplayTitle, fresh.Movie.DisplayTitle, "fresh scrape must resolve credits before composing display title")
		})
	}
}

func TestFreshScrapeDisplayTitleKeepsQuarantinedCreditsHidden(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	for _, first := range []string{"One", "Two"} {
		require.NoError(t, db.Create(&models.Actress{JapaneseName: "同名", FirstName: first, Verified: true, Origin: "user"}).Error)
	}
	repo := database.NewMovieRepository(db)
	movie := &models.Movie{ID: "TITLE-003", ContentID: "title003", Title: "Synthetic", Actresses: []models.Actress{{JapaneseName: "同名"}}}
	scrape.BuildCreditsFromScrape(movie, nil, nil)
	orch := newScrapeOrchestrator(&mockScraperScrape{result: &scrape.ScrapeResult{Movie: movie}}, repo, "<ID> <ACTORS>", template.NewEngine(), nfo.NFONameConfig{}, nil)
	result, _, err := orch.Execute(t.Context(), scrape.ScrapeCmd{MovieID: movie.ID})
	require.NoError(t, err)
	require.Len(t, result.Movie.Credits, 1)
	require.True(t, result.Movie.Credits[0].Actress.AmbiguityQuarantined)
	require.False(t, result.Movie.Credits[0].RenderVisible())
	require.Equal(t, "TITLE-003 ", result.Movie.DisplayTitle)
	stored, err := repo.FindByID(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Equal(t, result.Movie.DisplayTitle, stored.DisplayTitle)
}

func TestFreshScrapeDisplayTitleKeepsSuppressedCreditsHidden(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	canonical := models.Actress{DMMID: 71004, FirstName: "Hidden", LastName: "Person", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&canonical).Error)
	repo := database.NewMovieRepository(db)
	stored, err := repo.Upsert(t.Context(), &models.Movie{ID: "TITLE-004", ContentID: "title004", Title: "Synthetic", Actresses: []models.Actress{canonical}})
	require.NoError(t, err)
	require.NoError(t, database.NewMovieCreditRepository(db).UpdateSuppressed(t.Context(), stored.Credits[0].ID, true))
	movie := &models.Movie{ID: stored.ID, ContentID: stored.ContentID, Title: stored.Title, Actresses: []models.Actress{canonical}}
	scrape.BuildCreditsFromScrape(movie, nil, nil)
	orch := newScrapeOrchestrator(&mockScraperScrape{result: &scrape.ScrapeResult{Movie: movie}}, repo, "<ID> <ACTORS>", template.NewEngine(), nfo.NFONameConfig{}, nil)
	result, _, err := orch.Execute(t.Context(), scrape.ScrapeCmd{MovieID: movie.ID})
	require.NoError(t, err)
	require.Len(t, result.Movie.Credits, 1)
	require.True(t, result.Movie.Credits[0].Suppressed)
	require.Equal(t, "TITLE-004 ", result.Movie.DisplayTitle)
}
