package eventlog

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// EventStats holds aggregated counts for events grouped by type, severity, and source.
// It is the deep module's output — callers (API handlers, CLI commands) adapt this
// into their presentation shape without re-querying the repository.
type EventStats struct {
	Total      int64            `json:"total"`
	ByType     map[string]int64 `json:"by_type"`
	BySeverity map[string]int64 `json:"by_severity"`
	BySource   map[string]int64 `json:"by_source"`
}

// GetStats aggregates event counts from the repository into a single EventStats.
// It performs 8 repository calls (1 total + 3 by-type + 4 by-severity + 1 by-source),
// concentrating the aggregation logic in one deep module so that all callers
// (API handlers, CLI commands) share the same implementation.
func GetStats(ctx context.Context, repo database.EventRepositoryInterface) (*EventStats, error) {
	total, err := repo.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count events: %w", err)
	}

	byType := make(map[string]int64)
	for _, t := range []models.EventCategory{models.EventCategoryScraper, models.EventCategoryOrganize, models.EventCategorySystem} {
		count, err := repo.CountFiltered(ctx, database.EventFilter{EventType: t})
		if err != nil {
			return nil, fmt.Errorf("count events by type %s: %w", t, err)
		}
		byType[string(t)] = count
	}

	bySeverity := make(map[string]int64)
	for _, s := range []models.EventSeverity{models.SeverityDebug, models.SeverityInfo, models.SeverityWarn, models.SeverityError} {
		count, err := repo.CountFiltered(ctx, database.EventFilter{Severity: s})
		if err != nil {
			return nil, fmt.Errorf("count events by severity %s: %w", s, err)
		}
		bySeverity[string(s)] = count
	}

	bySource, err := repo.CountGroupBySource(ctx)
	if err != nil {
		return nil, fmt.Errorf("count events by source: %w", err)
	}

	return &EventStats{
		Total:      total,
		ByType:     byType,
		BySeverity: bySeverity,
		BySource:   bySource,
	}, nil
}
