package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Re-export scrape status for convenience
const (
	testStatusCompleted = scrape.StatusCompleted
	testStatusFailed    = scrape.StatusFailed
)

// --- scrapeOrchImpl: nil ctx defaults to Background ---

func TestScrapeOrchImpl_Miss_NilCtx(t *testing.T) {
	mockScraper := &mockScraperScrape{
		result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "NILCTX-001"}, Status: testStatusCompleted},
	}
	orch := newScrapeOrchestrator(mockScraper, nil, "", nil, nfo.NFONameConfig{}, nil)

	result, meta, err := orch.Execute(nil, scrape.ScrapeCmd{}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, meta)
}

// --- scrapeOrchImpl: ForceRefresh with movie repo ---

func TestScrapeOrchImpl_Miss_ForceRefresh(t *testing.T) {
	mockScraper := &mockScraperScrape{
		result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "FORCE-001"}, Status: testStatusCompleted},
	}
	mockRepo := &mockMovieRepoScrape{}

	orch := newScrapeOrchestrator(mockScraper, mockRepo, "", nil, nfo.NFONameConfig{}, nil)

	result, meta, err := orch.Execute(context.Background(), scrape.ScrapeCmd{
		MovieID:      "FORCE-001",
		ForceRefresh: true,
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, meta.Persisted)
	assert.True(t, mockRepo.deleteCalled)
}

// --- scrapeOrchImpl: nil scraper returns error ---

func TestScrapeOrchImpl_Miss_NilScraper(t *testing.T) {
	orch := newScrapeOrchestrator(nil, nil, "", nil, nfo.NFONameConfig{}, nil)

	_, _, err := orch.Execute(context.Background(), scrape.ScrapeCmd{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scraper not configured")
}

// --- scrapeOrchImpl: scrape returns error with nil result ---

func TestScrapeOrchImpl_Miss_ScrapeErrorNilResult(t *testing.T) {
	mockScraper := &mockScraperScrape{
		err:    assert.AnError,
		result: nil,
	}
	orch := newScrapeOrchestrator(mockScraper, nil, "", nil, nfo.NFONameConfig{}, nil)

	result, _, err := orch.Execute(context.Background(), scrape.ScrapeCmd{}, nil)
	require.Error(t, err)
	assert.Nil(t, result)
}

// --- scrapeOrchImpl: scrape returns error but has movie (partial success) ---

func TestScrapeOrchImpl_Miss_ScrapeErrorWithMovie(t *testing.T) {
	mockScraper := &mockScraperScrape{
		err:    assert.AnError,
		result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "PART-001"}, Status: testStatusCompleted},
	}
	orch := newScrapeOrchestrator(mockScraper, nil, "", nil, nfo.NFONameConfig{}, nil)

	result, meta, err := orch.Execute(context.Background(), scrape.ScrapeCmd{}, nil)
	// Error is returned but result should still be available
	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, meta)
	assert.True(t, meta.DisplayTitleApplied)
}

// --- scrapeOrchImpl: persist upsert failure ---

func TestScrapeOrchImpl_Miss_PersistUpsertFailure(t *testing.T) {
	mockScraper := &mockScraperScrape{
		result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "UPSERT-001"}, Status: testStatusCompleted},
	}
	mockRepo := &mockMovieRepoScrape{
		upsertErr: assert.AnError,
	}

	orch := newScrapeOrchestrator(mockScraper, mockRepo, "", nil, nfo.NFONameConfig{}, nil)

	_, _, err := orch.Execute(context.Background(), scrape.ScrapeCmd{}, nil)
	require.Error(t, err)
}

// --- scrapeOrchImpl: poster generation has moved to worker phase ---

func TestScrapeOrchImpl_Miss_PosterGenMovedToPhase(t *testing.T) {
	// Poster generation is no longer in the scrape orchestrator.
	// The worker's scrape phase calls posterGen.GeneratePoster directly.
	// This test verifies the orchestrator no longer sets PosterGenerated/PosterError.
	mockScraper := &mockScraperScrape{
		result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "POSTER-001"}, Status: testStatusCompleted},
	}

	orch := newScrapeOrchestrator(mockScraper, nil, "", nil, nfo.NFONameConfig{}, nil)

	result, meta, err := orch.Execute(context.Background(), scrape.ScrapeCmd{}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, meta.PosterGenerated, "poster generation should not be set by orchestrator")
	assert.Nil(t, meta.PosterError, "poster error should not be set by orchestrator")
}

// --- scrapeOrchImpl: translation warning propagated ---

func TestScrapeOrchImpl_Miss_TranslationWarning(t *testing.T) {
	mockScraper := &mockScraperScrape{
		result: &scrape.ScrapeResult{
			Movie:              &models.Movie{ID: "TRANS-001"},
			Status:             testStatusCompleted,
			TranslationWarning: "partial translation",
		},
	}

	orch := newScrapeOrchestrator(mockScraper, nil, "", nil, nfo.NFONameConfig{}, nil)

	result, meta, err := orch.Execute(context.Background(), scrape.ScrapeCmd{}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, meta.TranslationWarning)
	assert.Equal(t, "partial translation", *meta.TranslationWarning)
}

// --- scrapeOrchImpl: NeedsPersistence propagated and cleared ---

func TestScrapeOrchImpl_Miss_NeedsPersistence(t *testing.T) {
	mockScraper := &mockScraperScrape{
		result: &scrape.ScrapeResult{
			Movie:            &models.Movie{ID: "NEEDP-001"},
			Status:           testStatusCompleted,
			NeedsPersistence: true,
		},
	}
	mockRepo := &mockMovieRepoScrape{}

	orch := newScrapeOrchestrator(mockScraper, mockRepo, "", nil, nfo.NFONameConfig{}, nil)

	result, meta, err := orch.Execute(context.Background(), scrape.ScrapeCmd{}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, meta.Persisted)
	// NeedsPersistence should be cleared after persistence
	assert.False(t, meta.NeedsPersistence)
}

// --- mock types for scrape orchestrator tests ---

type mockScraperScrape struct {
	result *scrape.ScrapeResult
	err    error
}

func (m *mockScraperScrape) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, error) {
	return m.result, m.err
}

type mockMovieRepoScrape struct {
	deleteCalled bool
	upsertErr    error
}

func (m *mockMovieRepoScrape) Delete(_ context.Context, _ string) error {
	m.deleteCalled = true
	return nil
}

func (m *mockMovieRepoScrape) Create(_ context.Context, _ *models.Movie) error { return nil }
func (m *mockMovieRepoScrape) Update(_ context.Context, _ *models.Movie) error { return nil }
func (m *mockMovieRepoScrape) UpsertWithTranslations(_ context.Context, movie *models.Movie, _ []models.GenreTranslationData, _ []models.ActressTranslationData) (*models.Movie, error) {
	if m.upsertErr != nil {
		return nil, m.upsertErr
	}
	return movie, nil
}

// Add remaining interface methods as no-ops
func (m *mockMovieRepoScrape) FindByID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, nil
}
func (m *mockMovieRepoScrape) FindByContentID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, nil
}
func (m *mockMovieRepoScrape) List(_ context.Context, _, _ int) ([]models.Movie, error) {
	return nil, nil
}
func (m *mockMovieRepoScrape) Upsert(_ context.Context, _ *models.Movie) (*models.Movie, error) {
	return nil, nil
}
