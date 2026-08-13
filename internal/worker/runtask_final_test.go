package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestRunTaskFinal_DeadlineCompleteTaskError(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&timeoutResolverScraper{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-final-dl", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	taskID := uint(actress.ID)
	task := models.ActressSyncTask{ID: "task-final-dl", JobID: job.ID, ActressID: &taskID, Label: "test", DedupeKey: "actress:final-dl", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)

	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = db.Close()
	}()

	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTaskWithContext(context.Background(), claimed, 1*time.Second, nil, registry)
}

type successAndFailScraper struct{}

func (s *successAndFailScraper) Name() string { return "dmm" }
func (s *successAndFailScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *successAndFailScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *successAndFailScraper) IsEnabled() bool                                    { return true }
func (s *successAndFailScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *successAndFailScraper) Close() error { return nil }
func (s *successAndFailScraper) ResolveActressMetadata(_ context.Context, actress models.ActressInfo) (models.ActressInfo, error) {
	if actress.DMMID == 943 {
		return models.ActressInfo{DMMID: 943, FirstName: "Eri"}, nil
	}
	return models.ActressInfo{}, nil
}

type failResolverScraper struct{}

func (s *failResolverScraper) Name() string { return "javdb" }
func (s *failResolverScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *failResolverScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *failResolverScraper) IsEnabled() bool                                    { return true }
func (s *failResolverScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *failResolverScraper) Close() error { return nil }
func (s *failResolverScraper) ResolveActressMetadata(_ context.Context, _ models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{}, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Retryable: true}
}

func TestRunTaskFinal_UpdatedWithWarning(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&successAndFailScraper{})
	registry.RegisterInstance(&failResolverScraper{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-final-uw", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	taskID := uint(actress.ID)
	task := models.ActressSyncTask{ID: "task-final-uw", JobID: job.ID, ActressID: &taskID, Label: "test", DedupeKey: "actress:final-uw", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTaskWithContext(context.Background(), claimed, 3*time.Second, nil, registry)
}
