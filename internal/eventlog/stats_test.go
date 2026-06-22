package eventlog

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubEventRepo struct {
	total          int64
	totalErr       error
	filteredCounts map[database.EventFilter]int64
	filteredErr    error
	groupBySource  map[string]int64
	groupByErr     error
}

func (s *stubEventRepo) Create(_ context.Context, _ *models.Event) error { return nil }
func (s *stubEventRepo) FindByID(_ context.Context, _ uint) (*models.Event, error) {
	return nil, nil
}
func (s *stubEventRepo) FindFiltered(_ context.Context, _ database.EventFilter, _, _ int) ([]models.Event, error) {
	return nil, nil
}
func (s *stubEventRepo) CountFiltered(_ context.Context, f database.EventFilter) (int64, error) {
	if s.filteredErr != nil {
		return 0, s.filteredErr
	}
	if s.filteredCounts != nil {
		if c, ok := s.filteredCounts[f]; ok {
			return c, nil
		}
	}
	return 0, nil
}
func (s *stubEventRepo) List(_ context.Context, _, _ int) ([]models.Event, error) { return nil, nil }
func (s *stubEventRepo) Count(_ context.Context) (int64, error)                   { return s.total, s.totalErr }
func (s *stubEventRepo) CountGroupBySource(_ context.Context) (map[string]int64, error) {
	return s.groupBySource, s.groupByErr
}
func (s *stubEventRepo) DeleteOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func TestGetStats_Success(t *testing.T) {
	repo := &stubEventRepo{
		total: 42,
		filteredCounts: map[database.EventFilter]int64{
			{EventType: models.EventCategoryScraper}:  10,
			{EventType: models.EventCategoryOrganize}: 20,
			{EventType: models.EventCategorySystem}:   12,
			{Severity: models.SeverityDebug}:          5,
			{Severity: models.SeverityInfo}:           15,
			{Severity: models.SeverityWarn}:           10,
			{Severity: models.SeverityError}:          12,
		},
		groupBySource: map[string]int64{
			"r18dev": 10,
			"system": 12,
		},
	}

	stats, err := GetStats(context.Background(), repo)
	require.NoError(t, err)

	assert.Equal(t, int64(42), stats.Total)
	assert.Equal(t, int64(10), stats.ByType["scraper"])
	assert.Equal(t, int64(20), stats.ByType["organize"])
	assert.Equal(t, int64(12), stats.ByType["system"])
	assert.Equal(t, int64(5), stats.BySeverity["debug"])
	assert.Equal(t, int64(15), stats.BySeverity["info"])
	assert.Equal(t, int64(10), stats.BySeverity["warn"])
	assert.Equal(t, int64(12), stats.BySeverity["error"])
	assert.Equal(t, int64(10), stats.BySource["r18dev"])
	assert.Equal(t, int64(12), stats.BySource["system"])
}

func TestGetStats_CountError(t *testing.T) {
	repo := &stubEventRepo{
		totalErr: assert.AnError,
	}

	_, err := GetStats(context.Background(), repo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count events")
}

func TestGetStats_FilteredError(t *testing.T) {
	repo := &stubEventRepo{
		total:       10,
		filteredErr: assert.AnError,
	}

	_, err := GetStats(context.Background(), repo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count events by type")
}

func TestGetStats_GroupBySourceError(t *testing.T) {
	repo := &stubEventRepo{
		total:          10,
		filteredCounts: map[database.EventFilter]int64{},
		groupByErr:     assert.AnError,
	}

	_, err := GetStats(context.Background(), repo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count events by source")
}
