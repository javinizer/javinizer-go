package eventlog_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Cancel at the repository-call boundary, then let the real SQL repository
// observe the ordinary canceled context. No errors are synthesized here.
type cancelSeverityRepository struct {
	*database.EventRepository
	cancel        context.CancelFunc
	totalCalls    int
	typeCounts    map[models.EventCategory]int64
	severityCalls []models.EventSeverity
	groupCalls    int
	severityErr   error
	beforeCancel  error
}

func (r *cancelSeverityRepository) Count(ctx context.Context) (int64, error) {
	r.totalCalls++
	return r.EventRepository.Count(ctx)
}

func (r *cancelSeverityRepository) CountFiltered(ctx context.Context, filter database.EventFilter) (int64, error) {
	if filter.Severity != "" {
		r.severityCalls = append(r.severityCalls, filter.Severity)
		r.beforeCancel = ctx.Err()
		r.cancel()
	}
	count, err := r.EventRepository.CountFiltered(ctx, filter)
	if filter.Severity != "" {
		r.severityErr = err
	} else if err == nil {
		r.typeCounts[filter.EventType] = count
	}
	return count, err
}

func (r *cancelSeverityRepository) CountGroupBySource(ctx context.Context) (map[string]int64, error) {
	r.groupCalls++
	return r.EventRepository.CountGroupBySource(ctx)
}

func TestGetStatsRejectsPartialAggregateAfterSeverityCancellation(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "events.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := database.NewEventRepository(db)
	for _, event := range []models.Event{
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Source: "scraper", Message: "metadata received"},
		{EventType: models.EventCategoryOrganize, Severity: models.SeverityWarn, Source: "organizer", Message: "destination occupied"},
		{EventType: models.EventCategorySystem, Severity: models.SeverityError, Source: "system", Message: "operation refused"},
		{EventType: models.EventCategorySystem, Severity: models.SeverityDebug, Source: "system", Message: "request admitted"},
	} {
		require.NoError(t, repo.Create(context.Background(), &event))
	}
	before, err := repo.List(context.Background(), 100, 0)
	require.NoError(t, err)
	require.Len(t, before, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boundary := &cancelSeverityRepository{EventRepository: repo, cancel: cancel, typeCounts: make(map[models.EventCategory]int64)}

	stats, err := eventlog.GetStats(ctx, boundary)
	require.Nil(t, stats, "incomplete statistics must not be returned as a usable aggregate")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "count events by severity debug:")
	require.NoError(t, boundary.beforeCancel)
	require.ErrorIs(t, boundary.severityErr, context.Canceled, "the real repository must observe cancellation")
	require.Equal(t, 1, boundary.totalCalls)
	require.Equal(t, map[models.EventCategory]int64{
		models.EventCategoryScraper: 1, models.EventCategoryOrganize: 1, models.EventCategorySystem: 2,
	}, boundary.typeCounts)
	require.Equal(t, []models.EventSeverity{models.SeverityDebug}, boundary.severityCalls)
	require.Zero(t, boundary.groupCalls, "aggregation must stop at the failed severity query")

	after, err := repo.List(context.Background(), 100, 0)
	require.NoError(t, err)
	require.Equal(t, before, after, "canceled read-only aggregation must preserve persisted events")
	complete, err := eventlog.GetStats(context.Background(), repo)
	require.NoError(t, err)
	require.NotNil(t, complete)
	require.Equal(t, int64(4), complete.Total)
	require.Equal(t, map[string]int64{"scraper": 1, "organize": 1, "system": 2}, complete.ByType)
	require.Equal(t, map[string]int64{"debug": 1, "info": 1, "warn": 1, "error": 1}, complete.BySeverity)
	require.Equal(t, map[string]int64{"scraper": 1, "organizer": 1, "system": 2}, complete.BySource)
}
