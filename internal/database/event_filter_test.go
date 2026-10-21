package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEventFilterTestRepository(t *testing.T) (*DB, *EventRepository) {
	t.Helper()
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate(&models.Event{}))
	return db, NewEventRepository(db)
}

func TestEventFilteredRepository_PredicateParity(t *testing.T) {
	t.Parallel()
	_, repo := newEventFilterTestRepository(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Second)
	events := []models.Event{
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Source: "a", CreatedAt: start.Add(-time.Second)},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Source: "a", CreatedAt: start},
		{EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Source: "a", CreatedAt: start.Add(time.Second)},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityError, Source: "a", CreatedAt: start.Add(2 * time.Second)},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Source: "b", CreatedAt: start.Add(3 * time.Second)},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Source: "a", CreatedAt: end},
	}
	for i := range events {
		require.NoError(t, repo.Create(ctx, &events[i]))
	}
	// Every combination includes independent coverage of each optional predicate.
	for mask := 0; mask < 32; mask++ {
		t.Run(fmt.Sprintf("filters_%05b", mask), func(t *testing.T) {
			filter := EventFilter{}
			if mask&1 != 0 {
				filter.EventType = models.EventCategoryScraper
			}
			if mask&2 != 0 {
				filter.Severity = models.SeverityInfo
			}
			if mask&4 != 0 {
				filter.Source = "a"
			}
			if mask&8 != 0 {
				filter.Start = &start
			}
			if mask&16 != 0 {
				filter.End = &end
			}
			want := []uint{}
			for i := len(events) - 1; i >= 0; i-- {
				e := events[i]
				if filter.EventType != "" && e.EventType != filter.EventType {
					continue
				}
				if filter.Severity != "" && e.Severity != filter.Severity {
					continue
				}
				if filter.Source != "" && e.Source != filter.Source {
					continue
				}
				if filter.Start != nil && e.CreatedAt.Before(start) {
					continue
				}
				if filter.End != nil && !e.CreatedAt.Before(end) {
					continue
				}
				want = append(want, e.ID)
			}
			rows, err := repo.FindFiltered(ctx, filter, -1, 0)
			require.NoError(t, err)
			got := []uint{}
			for _, row := range rows {
				got = append(got, row.ID)
			}
			assert.Equal(t, want, got)
			count, err := repo.CountFiltered(ctx, filter)
			require.NoError(t, err)
			assert.Equal(t, int64(len(want)), count)
		})
	}
	// A fresh query must not inherit predicates from any previous call.
	for _, filter := range []EventFilter{{Source: "missing"}, {Start: &end, End: &start}, {Start: &start, End: &start}} {
		rows, err := repo.FindFiltered(ctx, filter, -1, 0)
		require.NoError(t, err)
		assert.Empty(t, rows)
		count, err := repo.CountFiltered(ctx, filter)
		require.NoError(t, err)
		assert.Zero(t, count)
	}
}

func TestEventFilteredRepository_TimeZonesAndPrecision(t *testing.T) {
	t.Parallel()
	_, repo := newEventFilterTestRepository(t)
	ctx := context.Background()
	startUTC := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	// Preserve SQLite datetime's second precision, even for fractional bounds.
	for i, delta := range []time.Duration{-time.Second, 0, 500 * time.Millisecond, time.Second} {
		require.NoError(t, repo.Create(ctx, &models.Event{Message: fmt.Sprint(i), CreatedAt: startUTC.Add(delta)}))
	}
	for _, offset := range []int{-7 * 3600, 0, 5*3600 + 30*60} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			zone := time.FixedZone("filter", offset)
			start := startUTC.Add(250 * time.Millisecond).In(zone)
			end := startUTC.Add(time.Second + 750*time.Millisecond).In(zone)
			filter := EventFilter{Start: &start, End: &end}
			rows, err := repo.FindFiltered(ctx, filter, -1, 0)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			assert.Equal(t, "2", rows[0].Message)
			assert.Equal(t, "1", rows[1].Message)
			count, err := repo.CountFiltered(ctx, filter)
			require.NoError(t, err)
			assert.Equal(t, int64(2), count)
		})
	}
}

func TestEventFilteredRepository_PaginationDoesNotAffectCount(t *testing.T) {
	t.Parallel()
	_, repo := newEventFilterTestRepository(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(ctx, &models.Event{Source: "a", Message: fmt.Sprint(i), CreatedAt: time.Date(2026, 1, 2, 12, 0, i, 0, time.UTC)}))
	}
	filter := EventFilter{Source: "a"}
	for _, tc := range []struct {
		limit, offset int
		want          []string
	}{
		{2, 1, []string{"3", "2"}}, {2, 4, []string{"0"}}, {2, 5, []string{}},
		{0, 0, []string{}}, {0, 1, []string{}}, {-1, 1, []string{"3", "2", "1", "0"}},
		{-1, 0, []string{"4", "3", "2", "1", "0"}}, {2, -1, []string{"4", "3"}},
	} {
		t.Run(fmt.Sprintf("limit_%d_offset_%d", tc.limit, tc.offset), func(t *testing.T) {
			rows, err := repo.FindFiltered(ctx, filter, tc.limit, tc.offset)
			require.NoError(t, err)
			got := []string{}
			for _, row := range rows {
				got = append(got, row.Message)
			}
			assert.Equal(t, tc.want, got)
			count, err := repo.CountFiltered(ctx, filter)
			require.NoError(t, err)
			assert.Equal(t, int64(5), count)
		})
	}
}

func TestEventFilteredRepository_Errors(t *testing.T) {
	t.Parallel()
	db, repo := newEventFilterTestRepository(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows, err := repo.FindFiltered(ctx, EventFilter{}, 2, 0)
	assert.Nil(t, rows)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "find filtered events: context canceled", err.Error())
	count, err := repo.CountFiltered(ctx, EventFilter{})
	assert.Zero(t, count)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "count filtered events: context canceled", err.Error())
	require.NoError(t, db.Close())
	rows, err = repo.FindFiltered(context.Background(), EventFilter{}, 2, 0)
	assert.Nil(t, rows)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find filtered events:")
	count, err = repo.CountFiltered(context.Background(), EventFilter{})
	assert.Zero(t, count)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count filtered events:")
}
