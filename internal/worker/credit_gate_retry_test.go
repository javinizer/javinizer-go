package worker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type creditGateOfflineRegistry struct{}

func (creditGateOfflineRegistry) GetInstance(string) (models.Scraper, bool) { return nil, false }
func (creditGateOfflineRegistry) GetInstancesByPriorityForInput([]string, string) []models.Scraper {
	return nil
}
func (creditGateOfflineRegistry) GetAllInstances() []models.Scraper { return nil }
func (creditGateOfflineRegistry) Names() []string                   { return []string{"offline"} }
func (creditGateOfflineRegistry) CollectDownloadProxyResolvers([]string) []models.DownloadProxyResolver {
	return nil
}

type creditGateOfflineScraper struct{}

func (creditGateOfflineScraper) Scrape(_ context.Context, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, error) {
	movie := &models.Movie{ID: cmd.MovieID, ContentID: cmd.MovieID, Title: "Synthetic", CreditPolicy: "block"}
	if cmd.MovieID == "SYNTH-001" {
		movie.Credits = []models.MovieCredit{{CreditedName: "Reported Different", Source: "offline", Scraped: models.Actress{DMMID: 910000, FirstName: "Different", LastName: "Reported"}}}
	}
	return &scrape.ScrapeResult{Movie: movie, Status: scrape.StatusCompleted}, nil
}

func TestCreditGateOnlyAttemptsRemainRetryable(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "all-blocked", true: "mixed-subset-reblocked"}[mixed], func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("JAVINIZER_HOME", filepath.Join(root, "appdata"))
			fs := afero.NewOsFs()
			db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "db.sqlite"), LogLevel: "silent"})
			require.NoError(t, err)
			defer db.Close()
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repos := db.Repositories()
			require.NoError(t, repos.ActressRepo.Create(t.Context(), &models.Actress{DMMID: 910000, FirstName: "Canonical", LastName: "Person", Verified: true, Origin: database.ActressOriginUser}))
			fc, _ := workflow.NewFactoryConfigFromRepos(config.DefaultConfig(nil, nil), creditGateOfflineRegistry{}, repos)
			fc.Fs = fs
			fc.TemplateEngine = template.NewEngine()
			fc.Scraper = creditGateOfflineScraper{}
			fc.Organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, fc.TemplateEngine, nil)
			fc.NFOGenerator = nfo.NewGenerator(fs, &nfo.Config{FilenameTemplate: "movie.nfo", TemplateEngine: fc.TemplateEngine})
			factory, err := workflow.NewWorkflowFactory(fc)
			require.NoError(t, err)
			wf, err := factory.NewWorkflow("credit-gate-retry")
			require.NoError(t, err)
			cfg := BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 10 * time.Second, NFOEnabled: true, PosterDisabled: true}
			store := NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, filepath.Join(root, "temp"), template.NewEngine(), fs, WithCollisionRepo(repos.CreditCollisionRepo))
			files := []string{filepath.Join(root, "SYNTH-001.mkv")}
			if mixed {
				files = append(files, filepath.Join(root, "SYNTH-002.mkv"))
			}
			matches := map[string]models.FileMatchInfo{}
			ids := map[string]string{}
			for i, path := range files {
				id := []string{"SYNTH-001", "SYNTH-002"}[i]
				ids[path] = id
				matches[path] = models.FileMatchInfo{Path: path, Name: filepath.Base(path), Extension: ".mkv", MovieID: id}
				require.NoError(t, afero.WriteFile(fs, path, []byte("synthetic payload"), 0600))
			}
			deps := NewBatchJobDeps(wf, nil, nil, cfg)
			deps.CollisionRepo = repos.CreditCollisionRepo
			deps.PublicationFence = database.NewMovieRepository(db)
			job := store.CreateJob(files, &JobConfig{ID: "credit-gate-retry", Destination: root, BatchJobDeps: deps})
			require.NoError(t, job.StartScrape(t.Context(), files, ScrapePhaseConfig{MovieIDOverride: ids, FileMatchInfo: matches}))
			require.NoError(t, job.Wait())
			apply := ApplyPhaseConfig{Destination: root, GenerateNFO: true, OrganizeOptions: workflow.OrganizeOptions{MoveFiles: false}}
			require.NoError(t, job.StartApply(t.Context(), apply))
			require.NoError(t, job.Wait())
			require.Equal(t, models.JobStatusCompleted, job.GetJobStatus())
			apply.RetryFilePaths = []string{files[0]}
			require.NoError(t, job.StartApply(t.Context(), apply))
			require.NoError(t, job.Wait())
			require.Equal(t, models.JobStatusCompleted, job.GetJobStatus())
			row := job.GetStatus().Results[files[0]]
			require.Equal(t, models.JobStatusFailed, row.Status)
			require.Equal(t, models.FileErrorCodeCreditConflict, row.ErrorCode)
			service := database.NewCollisionService(db)
			open, err := service.Collisions.ListOpenByMovie(t.Context(), "SYNTH-001")
			require.NoError(t, err)
			require.Len(t, open, 1)
			_, err = service.Resolve(t.Context(), open[0].ID, models.CollisionResolutionKeepIdentity, 0)
			require.NoError(t, err)
			require.NoError(t, job.StartApply(t.Context(), apply))
			require.NoError(t, job.Wait())
			require.Equal(t, models.JobStatusOrganized, job.GetJobStatus())
			require.Equal(t, models.JobStatusCompleted, job.GetStatus().Results[files[0]].Status)
		})
	}
}
