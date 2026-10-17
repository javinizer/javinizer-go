package r18dev

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// URL extraction and adapter search normalization deliberately accept aliases
// that are neither the stored display key nor its prefixed content primary key.
func TestRealR18AlternateManualInputCanRefreshExistingContent(t *testing.T) {
	for _, rawInput := range []string{"IPX535", "IPX 535", "https://r18.dev/videos/vod/movies/detail/-/id=ipx535/json", "https://r18.dev/videos/vod/movies/detail/-/combined=1ipx00535/json"} {
		t.Run(rawInput, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"dvd_id":"IPX-535","content_id":"1ipx00535","title_en":"Alternate manual query succeeded"}`))
			}))
			defer srv.Close()
			source := newR18TestScraper(srv, true, "en")
			db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "alternate.db"), LogLevel: "silent"})
			require.NoError(t, err)
			defer db.Close()
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repos := db.Repositories()
			old, err := repos.MovieRepo.Upsert(t.Context(), &models.Movie{ID: "OLD-101", ContentID: "old00101", Title: "Old UI family", Credits: []models.MovieCredit{{CreditedName: "Old actor", OverrideName: "Old movie alias", UserOverride: true, Origin: "user", Scraped: models.Actress{FirstName: "Old", LastName: "Actor"}}}})
			require.NoError(t, err)
			target, err := repos.MovieRepo.Upsert(t.Context(), &models.Movie{ID: "IPX-535", ContentID: "1ipx00535", Title: "Original manual target", Credits: []models.MovieCredit{{CreditedName: "Own actor", OverrideName: "Own movie alias", UserOverride: true, Origin: "user", Scraped: models.Actress{FirstName: "Own", LastName: "Actor"}}}})
			require.NoError(t, err)
			old, err = repos.MovieRepo.FindByContentID(t.Context(), old.ContentID)
			require.NoError(t, err)
			cfg := config.DefaultConfig(nil, nil)
			cfg.Scrapers.Priority = []string{"r18dev"}
			registry := scraperutil.NewScraperRegistry()
			registry.RegisterInstance(source)
			fc, err := workflow.NewFactoryConfigFromRepos(cfg, registry, repos)
			require.NoError(t, err)
			factory, err := workflow.NewWorkflowFactory(fc)
			require.NoError(t, err)
			wf, err := factory.NewWorkflow("")
			require.NoError(t, err)
			res, _, err := wf.Scrape(t.Context(), scrape.ScrapeCmd{MovieID: old.ID, RawInput: rawInput, ForceRefresh: true})
			require.NoError(t, err)
			require.Equal(t, target.ContentID, res.Movie.ContentID)
			assert.Equal(t, "Alternate manual query succeeded", res.Movie.Title)
			require.Len(t, res.Movie.Credits, 1)
			assert.Equal(t, "Own movie alias", res.Movie.Credits[0].OverrideName)
			after, err := repos.MovieRepo.FindByContentID(t.Context(), old.ContentID)
			require.NoError(t, err)
			assert.Equal(t, old, after)
		})
	}
}
