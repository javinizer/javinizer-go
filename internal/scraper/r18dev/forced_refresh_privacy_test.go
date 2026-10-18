package r18dev

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealR18MissingContentStaysPrivateUntilCommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "dvd_id=") {
			_, _ = w.Write([]byte(`{"dvd_id":"ipx535","content_id":"1ipx00535"}`))
			return
		}
		// The detail endpoint can omit a field even though the resolution endpoint had it.
		_, _ = w.Write([]byte(`{"dvd_id":"IPX-535","title_en":"Uncommitted fresh candidate"}`))
	}))
	defer srv.Close()
	source := newR18TestScraper(srv, true, "en")
	raw, err := source.Search(t.Context(), "IPX-535")
	require.NoError(t, err)
	require.Empty(t, raw.ContentID)
	require.Equal(t, "IPX-535", raw.ID)
	t.Logf("real adapter result ID=%q contentID=%q castNil=%v castLen=%d", raw.ID, raw.ContentID, raw.Actresses == nil, len(raw.Actresses))
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "pending.db"), LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repos := db.Repositories()
	before, err := repos.MovieRepo.Upsert(t.Context(), &models.Movie{ID: "IPX-535", ContentID: "1ipx00535", Title: "Old committed title"})
	require.NoError(t, err)
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.System.TempDir = t.TempDir()
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(source)
	fc, err := workflow.NewFactoryConfigFromRepos(cfg, registry, repos)
	require.NoError(t, err)
	factory, err := workflow.NewWorkflowFactory(fc)
	require.NoError(t, err)
	wf, err := factory.NewWorkflow("")
	require.NoError(t, err)
	store := worker.NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, cfg.System.TempDir, nil, nil, worker.WithEditTransactor(db))
	file := "/synthetic/IPX-535.mp4"
	job := store.CreateJobBatch([]string{file})
	job.Controller().SetWorkflow(wf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, job.Controller().StartScrape(ctx, []string{file}, worker.ScrapePhaseConfig{Force: true, SelectedScrapers: []string{"r18dev"}, MovieIDOverride: map[string]string{file: "IPX-535"}, OnFileScraped: func(string, string, string) {
		current := job.GetStatus().Results[file]
		require.NotNil(t, current)
		assert.Nil(t, current.Movie, "all forced deferred candidates must remain private even with missing ContentID")
		cancel()
	}}))
	require.ErrorContains(t, job.Controller().Wait(), "cancelled")
	row, err := repos.JobRepo.FindByID(t.Context(), job.GetID())
	require.NoError(t, err)

	decoded, errs := jobpersist.Decode(row)
	require.Empty(t, errs)
	require.Contains(t, decoded.Results, file)
	assert.Nil(t, decoded.Results[file].Movie, "recovered cancelled job must not expose uncommitted candidate")
	after, err := repos.MovieRepo.FindByContentID(t.Context(), before.ContentID)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
