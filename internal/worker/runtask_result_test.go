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

type conflictResolverScraper struct{}

func (s *conflictResolverScraper) Name() string { return "r18dev" }
func (s *conflictResolverScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *conflictResolverScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *conflictResolverScraper) IsEnabled() bool                                    { return true }
func (s *conflictResolverScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *conflictResolverScraper) Close() error { return nil }
func (s *conflictResolverScraper) ResolveActressMetadata(_ context.Context, actress models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{DMMID: actress.DMMID, FirstName: "Different"}, nil
}

func TestRunTaskResult_ConflictResult(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理", FirstName: "Eri"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&successAndFailScraper{})   // returns FirstName: "Eri"
	registry.RegisterInstance(&conflictResolverScraper{}) // returns FirstName: "Different"
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-conflict", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	taskID := uint(actress.ID)
	task := models.ActressSyncTask{ID: "task-conflict", JobID: job.ID, ActressID: &taskID, Label: "test", DedupeKey: "actress:conflict", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTaskWithContext(context.Background(), claimed, 30*time.Second, nil, registry)
}

type verifiedResolverScraper2 struct{}

func (s *verifiedResolverScraper2) Name() string { return "dmm" }
func (s *verifiedResolverScraper2) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *verifiedResolverScraper2) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *verifiedResolverScraper2) IsEnabled() bool { return true }
func (s *verifiedResolverScraper2) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *verifiedResolverScraper2) Close() error { return nil }
func (s *verifiedResolverScraper2) ResolveActressMetadata(_ context.Context, actress models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{DMMID: actress.DMMID, JapaneseName: actress.JapaneseName, FirstName: actress.FirstName}, nil
}

func TestRunTaskResult_VerifiedResult(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理", FirstName: "Eri"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&verifiedResolverScraper2{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-verified2", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	taskID := uint(actress.ID)
	task := models.ActressSyncTask{ID: "task-verified2", JobID: job.ID, ActressID: &taskID, Label: "test", DedupeKey: "actress:verified2", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTaskWithContext(context.Background(), claimed, 30*time.Second, nil, registry)
}

type retryableRepoErrorScraper struct{}

func (s *retryableRepoErrorScraper) Name() string { return "dmm" }
func (s *retryableRepoErrorScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *retryableRepoErrorScraper) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *retryableRepoErrorScraper) IsEnabled() bool { return true }
func (s *retryableRepoErrorScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *retryableRepoErrorScraper) Close() error { return nil }
func (s *retryableRepoErrorScraper) ResolveActressMetadata(_ context.Context, actress models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{DMMID: actress.DMMID, FirstName: "NewName"}, nil
}

func TestRunTaskResult_RetryableRepoError(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&retryableRepoErrorScraper{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-retry-repo", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	taskID := uint(actress.ID)
	task := models.ActressSyncTask{ID: "task-retry-repo", JobID: job.ID, ActressID: &taskID, Label: "test", DedupeKey: "actress:retry-repo", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)

	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTaskWithContext(context.Background(), claimed, 30*time.Second, nil, registry)
}
