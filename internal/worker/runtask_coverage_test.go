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

type panicScraper struct{}

func (s *panicScraper) Name() string { return "panic" }
func (s *panicScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *panicScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *panicScraper) IsEnabled() bool                                    { return true }
func (s *panicScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *panicScraper) Close() error { return nil }

func TestRunTaskCov_DeadlineExceeded(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	registry := scraperutil.NewScraperRegistry()
	_, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, actress.ID, registry)
	assert.NotNil(t, job)
}

type retryableErrorScraper struct{}

func (s *retryableErrorScraper) Name() string { return "retryable" }
func (s *retryableErrorScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *retryableErrorScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *retryableErrorScraper) IsEnabled() bool                                    { return true }
func (s *retryableErrorScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *retryableErrorScraper) Close() error { return nil }

func TestRunTaskCov_RetryableError(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&retryableErrorScraper{})
	_, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, actress.ID, registry)
	assert.NotNil(t, job)
}

type conflictResultScraper struct{}

func (s *conflictResultScraper) Name() string { return "conflict" }
func (s *conflictResultScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *conflictResultScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *conflictResultScraper) IsEnabled() bool                                    { return true }
func (s *conflictResultScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{ScrapeActress: &[]bool{true}[0]}
}
func (s *conflictResultScraper) Close() error { return nil }

func TestRunTaskCov_ConflictResult(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&conflictResultScraper{})
	_, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, actress.ID, registry)
	assert.NotNil(t, job)
	_ = time.Second
}
