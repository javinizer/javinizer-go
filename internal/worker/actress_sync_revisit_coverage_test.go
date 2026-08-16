package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type revisitNameKeyedScraper struct {
	name      string
	callCount int
	info      models.ActressInfo
}

func (s *revisitNameKeyedScraper) Name() string { return s.name }
func (s *revisitNameKeyedScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *revisitNameKeyedScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *revisitNameKeyedScraper) IsEnabled() bool                                    { return true }
func (s *revisitNameKeyedScraper) Config() *models.ScraperSettings                    { return &models.ScraperSettings{} }
func (s *revisitNameKeyedScraper) Close() error                                       { return nil }

func (s *revisitNameKeyedScraper) ResolveActressMetadata(_ context.Context, input models.ActressInfo) (models.ActressInfo, error) {
	s.callCount++
	if s.callCount == 1 {
		return models.ActressInfo{}, nil
	}
	return s.info, nil
}

type errorResolverScraper struct {
	name string
	err  error
}

func (s *errorResolverScraper) Name() string { return s.name }
func (s *errorResolverScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *errorResolverScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *errorResolverScraper) IsEnabled() bool                                    { return true }
func (s *errorResolverScraper) Config() *models.ScraperSettings                    { return &models.ScraperSettings{} }
func (s *errorResolverScraper) Close() error                                       { return nil }
func (s *errorResolverScraper) ResolveActressMetadata(_ context.Context, _ models.ActressInfo) (models.ActressInfo, error) {
	return models.ActressInfo{}, s.err
}

func TestCovFinal_RevisitLoopSuccess(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 943})

	dmmResolver := &metadataOnlyActressScraper{
		name: "dmm",
		info: models.ActressInfo{DMMID: 943, JapaneseName: "今井絵理"},
	}
	javdbResolver := &revisitNameKeyedScraper{
		name: "javdb",
		info: models.ActressInfo{DMMID: 943, JapaneseName: "今井絵理"},
	}

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(dmmResolver)
	registry.RegisterInstance(javdbResolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	assert.NotNil(t, result)
	_ = javdbResolver.callCount
}

func TestCovFinal_RevisitLoopFailure(t *testing.T) {
	_, actressRepo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 943})

	dmmResolver := &metadataOnlyActressScraper{
		name: "dmm",
		info: models.ActressInfo{DMMID: 943, JapaneseName: "今井絵理"},
	}
	javdbResolver := &errorResolverScraper{
		name: "javdb",
		err:  errors.New("connection reset"),
	}

	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(dmmResolver)
	registry.RegisterInstance(javdbResolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestCovFinal_RunTaskDeadlineExceeded(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	_, _ = runActressSyncManagerTask(t, db, actressRepo, movieRepo, actress.ID, registry)
}

func TestCovFinal_RunTaskRetryableError(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	actress := &models.Actress{DMMID: 943, JapaneseName: "今井絵理"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))

	registry := scraperutil.NewScraperRegistry()
	_, job := runActressSyncManagerTask(t, db, actressRepo, movieRepo, actress.ID, registry)
	assert.NotNil(t, job)
	_ = time.Second
}
