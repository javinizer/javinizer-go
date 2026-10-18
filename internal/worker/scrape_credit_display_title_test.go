package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestDeferredScrapePersistsCreditDisplayTitle(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	canonical := models.Actress{DMMID: 71002, FirstName: "Canonical", LastName: "Person", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&canonical).Error)
	movie := &models.Movie{ID: "TITLE-002", ContentID: "title002", Title: "Synthetic", DisplayTitle: "TITLE-002 ", Actresses: []models.Actress{canonical}}
	scrape.BuildCreditsFromScrape(movie, nil, nil)
	require.Nil(t, movie.Credits[0].Actress)
	engine := template.NewEngine()
	render := func(ctx context.Context, loaded *models.Movie) string {
		require.NotNil(t, loaded.Credits[0].Actress)
		rendered := *loaded
		workflow.ApplyDisplayTitleFromSource(ctx, &rendered, loaded, "<ID> <ACTORS>", engine, nfo.NFONameConfig{FirstNameOrder: true})
		return rendered.DisplayTitle
	}
	inputs := makeInputs(nil)
	inputs.MovieRepo = database.NewMovieRepository(db)
	inputs.Updater.UpdateFileResult("synthetic.mp4", &resultstore.MovieResult{Movie: movie})
	outcome := scrapeFileOutcome{FilePath: "synthetic.mp4", MovieID: movie.ID, Result: &scrape.ScrapeResult{Movie: movie}, Meta: &workflow.OrchestrationMeta{RenderDisplayTitle: render}}
	require.True(t, persistScrapeOutcome(t.Context(), outcome, inputs, nil))
	stored, err := inputs.MovieRepo.FindByID(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Equal(t, "TITLE-002 Canonical Person", stored.DisplayTitle)
	published := inputs.Updater.(*stubUpdater).getResult("synthetic.mp4")
	require.True(t, published.Persisted)
	require.Equal(t, stored.DisplayTitle, published.Movie.DisplayTitle)
	require.Equal(t, "TITLE-002 ", movie.DisplayTitle, "deferred persistence must not mutate the shared pre-persist movie")
	require.Nil(t, movie.Credits[0].Actress)
}
