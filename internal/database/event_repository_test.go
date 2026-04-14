package database

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventRepository_Create(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	event := &models.Event{
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityInfo,
		Message:   "Scraped ABC-001 from r18dev",
		Context:   `{"movie_id":"ABC-001","source":"r18dev"}`,
		Source:    "r18dev_scraper",
		CreatedAt: time.Now().UTC(),
	}

	err := repo.Create(event)
	require.NoError(t, err)
	assert.NotZero(t, event.ID)
}

func TestEventRepository_FindByID(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	event := &models.Event{
		EventType: models.EventCategoryOrganize,
		Severity:  models.SeverityInfo,
		Message:   "Organized ABC-001",
		Source:    "organizer",
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(event))

	found, err := repo.FindByID(event.ID)
	require.NoError(t, err)
	assert.Equal(t, models.EventCategoryOrganize, found.EventType)
	assert.Equal(t, models.SeverityInfo, found.Severity)
	assert.Equal(t, "Organized ABC-001", found.Message)
	assert.Equal(t, "organizer", found.Source)
}

func TestEventRepository_FindByID_NotFound(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	_, err := repo.FindByID(99999)
	assert.Error(t, err)
}

func TestEventRepository_FindByType(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	// Create events with different types
	eventTypes := []struct {
		eventType string
		message   string
	}{
		{models.EventCategoryScraper, "scraper event 1"},
		{models.EventCategoryOrganize, "organize event 1"},
		{models.EventCategoryScraper, "scraper event 2"},
		{models.EventCategorySystem, "system event 1"},
	}
	for _, et := range eventTypes {
		event := &models.Event{
			EventType: et.eventType,
			Severity:  models.SeverityInfo,
			Message:   et.message,
			Source:    "test",
			CreatedAt: time.Now().UTC(),
		}
		require.NoError(t, repo.Create(event))
	}

	// Find only scraper events
	scraperEvents, err := repo.FindByType(models.EventCategoryScraper, 10, 0)
	require.NoError(t, err)
	assert.Len(t, scraperEvents, 2)
	for _, e := range scraperEvents {
		assert.Equal(t, models.EventCategoryScraper, e.EventType)
	}

	// Find only organize events
	organizeEvents, err := repo.FindByType(models.EventCategoryOrganize, 10, 0)
	require.NoError(t, err)
	assert.Len(t, organizeEvents, 1)
	assert.Equal(t, models.EventCategoryOrganize, organizeEvents[0].EventType)

	// Find only system events
	systemEvents, err := repo.FindByType(models.EventCategorySystem, 10, 0)
	require.NoError(t, err)
	assert.Len(t, systemEvents, 1)
	assert.Equal(t, models.EventCategorySystem, systemEvents[0].EventType)
}

func TestEventRepository_FindBySeverity(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	// Create events with different severities
	severities := []struct {
		severity string
		message  string
	}{
		{models.SeverityInfo, "info event 1"},
		{models.SeverityError, "error event 1"},
		{models.SeverityWarn, "warn event 1"},
		{models.SeverityInfo, "info event 2"},
	}
	for _, se := range severities {
		event := &models.Event{
			EventType: models.EventCategorySystem,
			Severity:  se.severity,
			Message:   se.message,
			Source:    "test",
			CreatedAt: time.Now().UTC(),
		}
		require.NoError(t, repo.Create(event))
	}

	// Find only info events
	infoEvents, err := repo.FindBySeverity(models.SeverityInfo, 10, 0)
	require.NoError(t, err)
	assert.Len(t, infoEvents, 2)
	for _, e := range infoEvents {
		assert.Equal(t, models.SeverityInfo, e.Severity)
	}

	// Find only error events
	errorEvents, err := repo.FindBySeverity(models.SeverityError, 10, 0)
	require.NoError(t, err)
	assert.Len(t, errorEvents, 1)
	assert.Equal(t, models.SeverityError, errorEvents[0].Severity)
}

func TestEventRepository_FindByTypeAndSeverity(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	// Create events with various type+severity combinations
	events := []*models.Event{
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "scraper info", Source: "test", CreatedAt: time.Now().UTC()},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityError, Message: "scraper error", Source: "test", CreatedAt: time.Now().UTC()},
		{EventType: models.EventCategoryOrganize, Severity: models.SeverityInfo, Message: "organize info", Source: "test", CreatedAt: time.Now().UTC()},
	}
	for _, e := range events {
		require.NoError(t, repo.Create(e))
	}

	// Find scraper errors
	scraperErrors, err := repo.FindByTypeAndSeverity(models.EventCategoryScraper, models.SeverityError, 10, 0)
	require.NoError(t, err)
	assert.Len(t, scraperErrors, 1)
	assert.Equal(t, "scraper error", scraperErrors[0].Message)

	// Find scraper info
	scraperInfo, err := repo.FindByTypeAndSeverity(models.EventCategoryScraper, models.SeverityInfo, 10, 0)
	require.NoError(t, err)
	assert.Len(t, scraperInfo, 1)
	assert.Equal(t, "scraper info", scraperInfo[0].Message)

	// Find organize errors (none exist)
	organizeErrors, err := repo.FindByTypeAndSeverity(models.EventCategoryOrganize, models.SeverityError, 10, 0)
	require.NoError(t, err)
	assert.Len(t, organizeErrors, 0)
}

func TestEventRepository_FindByDateRange(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	now := time.Now().UTC()

	// Create events at different times
	events := []*models.Event{
		{EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "past event", Source: "test", CreatedAt: now.Add(-48 * time.Hour)},
		{EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "recent event", Source: "test", CreatedAt: now.Add(-1 * time.Hour)},
		{EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "now event", Source: "test", CreatedAt: now},
	}
	for _, e := range events {
		require.NoError(t, repo.Create(e))
	}

	// Find events in the last 2 hours (inclusive start, exclusive end is not how GORM works;
	// we use >= start AND < end per plan specification)
	start := now.Add(-2 * time.Hour)
	end := now.Add(1 * time.Minute) // slightly in the future to include "now"
	found, err := repo.FindByDateRange(start, end, 10, 0)
	require.NoError(t, err)
	assert.Len(t, found, 2) // "recent event" and "now event"

	// Verify past event is excluded
	for _, e := range found {
		assert.True(t, e.CreatedAt.After(start) || e.CreatedAt.Equal(start))
	}
}

func TestEventRepository_Count(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	// Initially zero
	count, err := repo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// Create 5 events
	for i := 0; i < 5; i++ {
		event := &models.Event{
			EventType: models.EventCategorySystem,
			Severity:  models.SeverityInfo,
			Message:   "test event",
			Source:    "test",
			CreatedAt: time.Now().UTC(),
		}
		require.NoError(t, repo.Create(event))
	}

	count, err = repo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
}

func TestEventRepository_CountByType(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	// Create 3 scraper events and 2 system events
	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(&models.Event{
			EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "s", Source: "test", CreatedAt: time.Now().UTC(),
		}))
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, repo.Create(&models.Event{
			EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "sy", Source: "test", CreatedAt: time.Now().UTC(),
		}))
	}

	scraperCount, err := repo.CountByType(models.EventCategoryScraper)
	require.NoError(t, err)
	assert.Equal(t, int64(3), scraperCount)

	systemCount, err := repo.CountByType(models.EventCategorySystem)
	require.NoError(t, err)
	assert.Equal(t, int64(2), systemCount)

	organizeCount, err := repo.CountByType(models.EventCategoryOrganize)
	require.NoError(t, err)
	assert.Equal(t, int64(0), organizeCount)
}

func TestEventRepository_CountBySeverity(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	// Create events with different severities
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityDebug, Message: "d", Source: "test", CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityError, Message: "e1", Source: "test", CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityError, Message: "e2", Source: "test", CreatedAt: time.Now().UTC(),
	}))

	errorCount, err := repo.CountBySeverity(models.SeverityError)
	require.NoError(t, err)
	assert.Equal(t, int64(2), errorCount)

	debugCount, err := repo.CountBySeverity(models.SeverityDebug)
	require.NoError(t, err)
	assert.Equal(t, int64(1), debugCount)
}

func TestEventRepository_DeleteOlderThan(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	now := time.Now().UTC()

	// Create old and new events
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "old event 1", Source: "test", CreatedAt: now.Add(-72 * time.Hour),
	}))
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "old event 2", Source: "test", CreatedAt: now.Add(-48 * time.Hour),
	}))
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "recent event", Source: "test", CreatedAt: now.Add(-1 * time.Hour),
	}))

	// Delete events older than 24 hours ago
	cutoff := now.Add(-24 * time.Hour)
	err := repo.DeleteOlderThan(cutoff)
	require.NoError(t, err)

	// Only recent event should remain
	count, err := repo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	remaining, err := repo.FindBySeverity(models.SeverityInfo, 10, 0)
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "recent event", remaining[0].Message)
}

func TestEventRepository_EventsTableIsIndependent(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	eventRepo := NewEventRepository(db)
	historyRepo := NewHistoryRepository(db)
	bfoRepo := NewBatchFileOperationRepository(db)

	// Create an event
	require.NoError(t, eventRepo.Create(&models.Event{
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityInfo,
		Message:   "test event",
		Source:    "test",
		CreatedAt: time.Now().UTC(),
	}))

	// Create a history record
	require.NoError(t, historyRepo.Create(&models.History{
		MovieID:   "INDEP-001",
		Operation: "scrape",
		Status:    "success",
	}))

	// Create a batch file operation
	require.NoError(t, bfoRepo.Create(&models.BatchFileOperation{
		BatchJobID:    "indep-batch-001",
		OriginalPath:  "/original/file.mp4",
		NewPath:       "/new/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))

	// Verify all records exist
	eventCount, err := eventRepo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(1), eventCount)

	historyCount, err := historyRepo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(1), historyCount)

	bfoCount, err := bfoRepo.CountByBatchJobID("indep-batch-001")
	require.NoError(t, err)
	assert.Equal(t, int64(1), bfoCount)

	// Delete all events
	cutoff := time.Now().UTC().Add(1 * time.Hour) // future cutoff deletes everything
	err = eventRepo.DeleteOlderThan(cutoff)
	require.NoError(t, err)

	// Verify events are gone
	eventCount, err = eventRepo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(0), eventCount)

	// Verify history records still exist
	historyCount, err = historyRepo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(1), historyCount)

	// Verify batch file operations still exist
	bfoCount, err = bfoRepo.CountByBatchJobID("indep-batch-001")
	require.NoError(t, err)
	assert.Equal(t, int64(1), bfoCount)
}

func TestEventRepository_FindByType_WithPagination(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	// Create 5 scraper events
	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(&models.Event{
			EventType: models.EventCategoryScraper,
			Severity:  models.SeverityInfo,
			Message:   "scraper event",
			Source:    "test",
			CreatedAt: time.Now().UTC(),
		}))
	}

	// Get first 2
	page1, err := repo.FindByType(models.EventCategoryScraper, 2, 0)
	require.NoError(t, err)
	assert.Len(t, page1, 2)

	// Get next 2
	page2, err := repo.FindByType(models.EventCategoryScraper, 2, 2)
	require.NoError(t, err)
	assert.Len(t, page2, 2)

	// Get last 1
	page3, err := repo.FindByType(models.EventCategoryScraper, 2, 4)
	require.NoError(t, err)
	assert.Len(t, page3, 1)
}

func TestEventRepository_FindBySource(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	events := []*models.Event{
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "r18dev scrape", Source: "r18dev", CreatedAt: time.Now().UTC()},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityError, Message: "dmm scrape failed", Source: "dmm", CreatedAt: time.Now().UTC()},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "r18dev another", Source: "r18dev", CreatedAt: time.Now().UTC()},
		{EventType: models.EventCategoryOrganize, Severity: models.SeverityInfo, Message: "organize done", Source: "organizer", CreatedAt: time.Now().UTC()},
	}
	for _, e := range events {
		require.NoError(t, repo.Create(e))
	}

	r18devEvents, err := repo.FindBySource("r18dev", 10, 0)
	require.NoError(t, err)
	assert.Len(t, r18devEvents, 2)
	for _, e := range r18devEvents {
		assert.Equal(t, "r18dev", e.Source)
	}

	dmmEvents, err := repo.FindBySource("dmm", 10, 0)
	require.NoError(t, err)
	assert.Len(t, dmmEvents, 1)
	assert.Equal(t, "dmm", dmmEvents[0].Source)

	organizerEvents, err := repo.FindBySource("organizer", 10, 0)
	require.NoError(t, err)
	assert.Len(t, organizerEvents, 1)
	assert.Equal(t, "organizer", organizerEvents[0].Source)

	emptyEvents, err := repo.FindBySource("nonexistent", 10, 0)
	require.NoError(t, err)
	assert.Len(t, emptyEvents, 0)
}

func TestEventRepository_CountBySource(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(&models.Event{
			EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "r18dev event", Source: "r18dev", CreatedAt: time.Now().UTC(),
		}))
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, repo.Create(&models.Event{
			EventType: models.EventCategoryScraper, Severity: models.SeverityError, Message: "dmm event", Source: "dmm", CreatedAt: time.Now().UTC(),
		}))
	}

	r18devCount, err := repo.CountBySource("r18dev")
	require.NoError(t, err)
	assert.Equal(t, int64(3), r18devCount)

	dmmCount, err := repo.CountBySource("dmm")
	require.NoError(t, err)
	assert.Equal(t, int64(2), dmmCount)

	organizerCount, err := repo.CountBySource("organizer")
	require.NoError(t, err)
	assert.Equal(t, int64(0), organizerCount)
}

func TestEventRepository_CountGroupBySource(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(&models.Event{
			EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "r18dev", Source: "r18dev", CreatedAt: time.Now().UTC(),
		}))
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, repo.Create(&models.Event{
			EventType: models.EventCategoryScraper, Severity: models.SeverityError, Message: "dmm", Source: "dmm", CreatedAt: time.Now().UTC(),
		}))
	}

	bySource, err := repo.CountGroupBySource()
	require.NoError(t, err)
	assert.Equal(t, int64(3), bySource["r18dev"])
	assert.Equal(t, int64(2), bySource["dmm"])
	assert.Len(t, bySource, 2)
}

func TestEventRepository_FindFiltered(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	events := []*models.Event{
		{EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "r18dev scrape ok", Source: "r18dev", CreatedAt: time.Now().UTC().Add(-2 * time.Hour)},
		{EventType: models.EventCategoryScraper, Severity: models.SeverityError, Message: "dmm scrape fail", Source: "dmm", CreatedAt: time.Now().UTC().Add(-1 * time.Hour)},
		{EventType: models.EventCategoryOrganize, Severity: models.SeverityInfo, Message: "organize ok", Source: "organizer", CreatedAt: time.Now().UTC().Add(-30 * time.Minute)},
		{EventType: models.EventCategoryOrganize, Severity: models.SeverityWarn, Message: "conflict", Source: "organizer", CreatedAt: time.Now().UTC().Add(-20 * time.Minute)},
		{EventType: models.EventCategorySystem, Severity: models.SeverityDebug, Message: "server start", Source: "server", CreatedAt: time.Now().UTC().Add(-10 * time.Minute)},
	}
	for _, e := range events {
		require.NoError(t, repo.Create(e))
	}

	noFilter, err := repo.FindFiltered(EventFilter{}, 10, 0)
	require.NoError(t, err)
	assert.Len(t, noFilter, 5)

	typeOnly, err := repo.FindFiltered(EventFilter{EventType: models.EventCategoryScraper}, 10, 0)
	require.NoError(t, err)
	assert.Len(t, typeOnly, 2)

	typeAndSource, err := repo.FindFiltered(EventFilter{EventType: models.EventCategoryOrganize, Source: "organizer"}, 10, 0)
	require.NoError(t, err)
	assert.Len(t, typeAndSource, 2)
	for _, e := range typeAndSource {
		assert.Equal(t, models.EventCategoryOrganize, e.EventType)
		assert.Equal(t, "organizer", e.Source)
	}

	severityAndSource, err := repo.FindFiltered(EventFilter{Severity: models.SeverityInfo, Source: "r18dev"}, 10, 0)
	require.NoError(t, err)
	assert.Len(t, severityAndSource, 1)
	assert.Equal(t, "r18dev", severityAndSource[0].Source)

	noMatch, err := repo.FindFiltered(EventFilter{EventType: models.EventCategorySystem, Source: "r18dev"}, 10, 0)
	require.NoError(t, err)
	assert.Len(t, noMatch, 0)

	now := time.Now().UTC()
	withDateRange, err := repo.FindFiltered(EventFilter{Start: &now}, 10, 0)
	require.NoError(t, err)
	assert.Len(t, withDateRange, 0)
}

func TestEventRepository_CountFiltered(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(&models.Event{
			EventType: models.EventCategoryScraper, Severity: models.SeverityInfo, Message: "r18dev", Source: "r18dev", CreatedAt: time.Now().UTC(),
		}))
	}
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategoryScraper, Severity: models.SeverityError, Message: "dmm", Source: "dmm", CreatedAt: time.Now().UTC(),
	}))

	total, err := repo.CountFiltered(EventFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)

	scraperCount, err := repo.CountFiltered(EventFilter{EventType: models.EventCategoryScraper})
	require.NoError(t, err)
	assert.Equal(t, int64(4), scraperCount)

	r18devInfoCount, err := repo.CountFiltered(EventFilter{Source: "r18dev", Severity: models.SeverityInfo})
	require.NoError(t, err)
	assert.Equal(t, int64(3), r18devInfoCount)
}

func TestEventRepository_OrderByCreatedAtDesc(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	now := time.Now().UTC()

	// Create events in chronological order
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "first", Source: "test", CreatedAt: now.Add(-2 * time.Hour),
	}))
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "second", Source: "test", CreatedAt: now.Add(-1 * time.Hour),
	}))
	require.NoError(t, repo.Create(&models.Event{
		EventType: models.EventCategorySystem, Severity: models.SeverityInfo, Message: "third", Source: "test", CreatedAt: now,
	}))

	// Results should be ordered by created_at DESC (newest first)
	events, err := repo.FindByType(models.EventCategorySystem, 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, "third", events[0].Message)
	assert.Equal(t, "second", events[1].Message)
	assert.Equal(t, "first", events[2].Message)
}
