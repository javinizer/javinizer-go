package history

import (
	"context"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Logger wraps HistoryRepositoryInterface for CLI history commands.
// GetStats is the only method with genuine depth — it aggregates 6 repo calls
// (Count, CountByStatus×3, CountByOperation×2+) into a single stats struct.
// The other methods are intentionally kept as thin wrappers for CLI ergonomics
// (callers say logger.GetRecent(...) rather than repo.FindRecent(...));
// they do not add depth and should not gain logic.
type Logger struct {
	repo database.HistoryRepositoryInterface
}

func NewLogger(repo database.HistoryRepositoryInterface) *Logger {
	return &Logger{
		repo: repo,
	}
}

// GetRecent returns the most recent history records.
// Pass-through to repo.FindRecent — retained for CLI call-site readability.
func (l *Logger) GetRecent(ctx context.Context, limit int) ([]models.History, error) {
	return l.repo.FindRecent(ctx, limit)
}

// GetByMovieID returns history records for a given movie ID.
// Pass-through to repo.FindByMovieID — retained for CLI call-site readability.
func (l *Logger) GetByMovieID(ctx context.Context, movieID string) ([]models.History, error) {
	return l.repo.FindByMovieID(ctx, movieID)
}

// GetByOperation returns history records for a given operation type.
// Pass-through to repo.FindByOperation — retained for CLI call-site readability.
func (l *Logger) GetByOperation(ctx context.Context, operation models.HistoryOperation, limit int) ([]models.History, error) {
	return l.repo.FindByOperation(ctx, operation, limit)
}

// GetByStatus returns history records for a given status.
// Pass-through to repo.FindByStatus — retained for CLI call-site readability.
func (l *Logger) GetByStatus(ctx context.Context, status models.HistoryStatus, limit int) ([]models.History, error) {
	return l.repo.FindByStatus(ctx, status, limit)
}

// GetStats aggregates counts by status and operation into a single struct.
// This is the only method with real depth — it consolidates 6+ repo calls
// behind a single interface, giving callers leverage and maintainers locality.
func (l *Logger) GetStats(ctx context.Context) (*Stats, error) {
	s := &Stats{}

	total, err := l.repo.Count(ctx)
	if err != nil {
		return nil, err
	}
	s.Total = total

	success, err := l.repo.CountByStatus(ctx, models.HistoryStatusSuccess)
	if err != nil {
		return nil, err
	}
	s.Success = success

	failed, err := l.repo.CountByStatus(ctx, models.HistoryStatusFailed)
	if err != nil {
		return nil, err
	}
	s.Failed = failed

	reverted, err := l.repo.CountByStatus(ctx, models.HistoryStatusReverted)
	if err != nil {
		return nil, err
	}
	s.Reverted = reverted

	scrapeCount, err := l.repo.CountByOperation(ctx, models.HistoryOpScrape)
	if err != nil {
		return nil, err
	}
	s.Scrape = scrapeCount

	organizeCount, err := l.repo.CountByOperation(ctx, models.HistoryOpOrganize)
	if err != nil {
		return nil, err
	}
	s.Organize = organizeCount

	downloadCount, err := l.repo.CountByOperation(ctx, models.HistoryOpDownload)
	if err != nil {
		return nil, err
	}
	s.Download = downloadCount

	nfoCount, err := l.repo.CountByOperation(ctx, models.HistoryOpNFO)
	if err != nil {
		return nil, err
	}
	s.NFO = nfoCount

	return s, nil
}

// Stats holds aggregated history counts by status and operation.
// Populated by Logger.GetStats, which consolidates 6+ repository calls
// behind a single interface — giving callers leverage and maintainers locality.
type Stats struct {
	Total    int64
	Success  int64
	Failed   int64
	Reverted int64
	Scrape   int64
	Organize int64
	Download int64
	NFO      int64
}

// CleanupOldRecords removes history records older than the given duration.
// Pass-through to repo.DeleteOlderThan — retained for CLI call-site readability.
func (l *Logger) CleanupOldRecords(ctx context.Context, olderThan time.Duration) error {
	cutoffDate := time.Now().UTC().Add(-olderThan)
	return l.repo.DeleteOlderThan(ctx, cutoffDate)
}

// Count returns the total number of history records.
func (l *Logger) Count(ctx context.Context) (int64, error) {
	return l.repo.Count(ctx)
}
