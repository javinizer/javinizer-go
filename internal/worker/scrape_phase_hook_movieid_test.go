package worker

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingPersistRepo is a MovieRepositoryInterface whose UpsertWithTranslations
// always errors, so the persist pool's failure path (which re-fires the
// per-file OnFileScrapeFailed hook with the resolved movie ID) is exercised.
// All other methods are no-ops — only UpsertWithTranslations is reached by the
// scrape phase's persist pool.
type failingPersistRepo struct{}

func (failingPersistRepo) Create(_ context.Context, _ *models.Movie) error { return nil }
func (failingPersistRepo) Update(_ context.Context, _ *models.Movie) error { return nil }
func (failingPersistRepo) Upsert(_ context.Context, movie *models.Movie) (*models.Movie, error) {
	return movie, nil
}
func (failingPersistRepo) UpsertWithTranslations(_ context.Context, _ *models.Movie, _ []models.GenreTranslationData, _ []models.ActressTranslationData) (*models.Movie, error) {
	return nil, fmt.Errorf("simulated persist failure")
}
func (failingPersistRepo) FindByID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, nil
}
func (failingPersistRepo) FindByContentID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, nil
}
func (failingPersistRepo) Delete(_ context.Context, _ string) error { return nil }
func (failingPersistRepo) List(_ context.Context, _, _ int) ([]models.Movie, error) {
	return nil, nil
}

// recordingHook captures the arguments passed to an OnFileScraped /
// OnFileScrapeFailed hook. The scrape phase invokes hooks concurrently from
// worker goroutines, so access is mutex-guarded.
type recordingHook struct {
	mu       sync.Mutex
	calls    []hookCall
	filePath []string
	movieID  []string
	msg      []string
}

type hookCall struct {
	filePath string
	movieID  string
	msg      string
}

func (h *recordingHook) record(filePath, movieID, msg string) {
	h.mu.Lock()
	h.calls = append(h.calls, hookCall{filePath: filePath, movieID: movieID, msg: msg})
	h.filePath = append(h.filePath, filePath)
	h.movieID = append(h.movieID, movieID)
	h.msg = append(h.msg, msg)
	h.mu.Unlock()
}

func (h *recordingHook) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

// TestScrapePhase_Run_OnFileScraped_PassesMovieID covers the success-path hook
// call added in PR #144 (cfg.OnFileScraped now receives outcome.MovieID).
func TestScrapePhase_Run_OnFileScraped_PassesMovieID(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)

	scraped := &recordingHook{}
	cfg := ScrapePhaseConfig{OnFileScraped: scraped.record}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)

	require.Equal(t, 1, scraped.callCount(), "OnFileScraped should fire once for the scraped file")
	assert.Equal(t, "file.mp4", scraped.calls[0].filePath)
	assert.Equal(t, "TEST-001", scraped.calls[0].movieID, "OnFileScraped must carry the resolved movie ID")
	assert.Contains(t, scraped.calls[0].msg, "TEST-001")
}

// TestScrapePhase_Run_OnFileScrapeFailed_PassesMovieID covers the failed-path
// hook call added in PR #144 (cfg.OnFileScrapeFailed now receives
// outcome.MovieID) for a scrape that errors.
func TestScrapePhase_Run_OnFileScrapeFailed_PassesMovieID(t *testing.T) {
	wf := &stubWorkflow{scrapeErr: fmt.Errorf("network error")}
	inputs := makeInputs(wf)

	failed := &recordingHook{}
	cfg := ScrapePhaseConfig{OnFileScrapeFailed: failed.record}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)

	require.Equal(t, 1, failed.callCount(), "OnFileScrapeFailed should fire once for the failed file")
	assert.Equal(t, "file.mp4", failed.calls[0].filePath)
	assert.Equal(t, "TEST-001", failed.calls[0].movieID, "OnFileScrapeFailed must carry the resolved movie ID")
	assert.Contains(t, failed.calls[0].msg, "network error")
}

// TestScrapePhase_Run_PersistFailure_FiresOnFileScrapeFailedWithMovieID
// covers the persist-pool failure path added in PR #144: when a successful
// scrape's DB persist fails, the per-file failure hook is re-fired with the
// resolved movie ID so the frontend's messagesByFile flips from success to
// error instead of leaving a stale "success".
func TestScrapePhase_Run_PersistFailure_FiresOnFileScrapeFailedWithMovieID(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	inputs := makeInputs(wf)
	inputs.MovieRepo = failingPersistRepo{}

	failed := &recordingHook{}
	cfg := ScrapePhaseConfig{OnFileScrapeFailed: failed.record}

	NewScrapePhase().Run(context.Background(), inputs, []string{"file.mp4"}, cfg)

	require.Equal(t, 1, failed.callCount(), "OnFileScrapeFailed should fire once when persist fails")
	assert.Equal(t, "file.mp4", failed.calls[0].filePath)
	assert.Equal(t, "TEST-001", failed.calls[0].movieID, "persist-failure hook must carry the resolved movie ID")
	assert.Contains(t, failed.calls[0].msg, "persist failed")
}
