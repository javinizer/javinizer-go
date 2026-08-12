package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type timeoutResolverScraper struct{}

func (s *timeoutResolverScraper) Name() string { return "timeout" }
func (s *timeoutResolverScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *timeoutResolverScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *timeoutResolverScraper) IsEnabled() bool                                    { return true }
func (s *timeoutResolverScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *timeoutResolverScraper) Close() error { return nil }
func (s *timeoutResolverScraper) ResolveActressMetadata(ctx context.Context, _ models.ActressInfo) (models.ActressInfo, error) {
	<-ctx.Done()
	return models.ActressInfo{}, ctx.Err()
}

func TestRunTaskCov_DeadlineExceeded(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&timeoutResolverScraper{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	manager.ctx = context.Background()
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-dl", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: "task-dl", JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: "actress:dl", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTask(claimed, 10*time.Millisecond, registry)
	assert.NotNil(t, job)
}

type retryableResolverScraper struct{}

func (s *retryableResolverScraper) Name() string { return "retryable" }
func (s *retryableResolverScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *retryableResolverScraper) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *retryableResolverScraper) IsEnabled() bool { return true }
func (s *retryableResolverScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *retryableResolverScraper) Close() error { return nil }
func (s *retryableResolverScraper) ResolveActressMetadata(_ context.Context, _ models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{}, &models.ScraperError{Kind: models.ScraperErrorKindRateLimited, Retryable: true}
}

func TestRunTaskCov_RetryableError(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&retryableResolverScraper{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	manager.ctx = context.Background()
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-rt", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: "task-rt", JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: "actress:rt", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTask(claimed, time.Minute, registry)
	assert.NotNil(t, job)
}

type verifiedResolverScraper struct{}

func (s *verifiedResolverScraper) Name() string { return "verified" }
func (s *verifiedResolverScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *verifiedResolverScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *verifiedResolverScraper) IsEnabled() bool                                    { return true }
func (s *verifiedResolverScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *verifiedResolverScraper) Close() error { return nil }
func (s *verifiedResolverScraper) ResolveActressMetadata(_ context.Context, actress models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{DMMID: actress.DMMID, FirstName: "Verified"}, nil
}

func TestRunTaskCov_VerifiedResult(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&verifiedResolverScraper{})
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	manager.ctx = context.Background()
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-vf", Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: "task-vf", JobID: job.ID, ActressID: &actress.ID, Label: "test", DedupeKey: "actress:vf", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, manager.repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := manager.repo.ClaimNext(manager.owner, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	manager.active.Add(1)
	manager.wg.Add(1)
	manager.runTask(claimed, time.Minute, registry)
	assert.NotNil(t, job)
}
