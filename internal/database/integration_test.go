package database

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIntegrationTestDB creates a fresh in-memory database with all migrations applied.
// Used for integration tests that verify cross-feature compatibility.
func newIntegrationTestDB(t *testing.T) *DB {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err, "Failed to create database")
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate(), "Failed to run migrations")
	return db
}

// TestIntegration_MigrationCreatesAllTables verifies that all new tables exist
// after running migrations on a fresh database.
func TestIntegration_MigrationCreatesAllTables(t *testing.T) {
	db := newIntegrationTestDB(t)

	// Verify batch_file_operations and events tables exist
	var tables []string
	err := db.Raw("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").Scan(&tables).Error
	require.NoError(t, err)

	tableSet := make(map[string]bool, len(tables))
	for _, tbl := range tables {
		tableSet[tbl] = true
	}

	assert.True(t, tableSet["batch_file_operations"], "batch_file_operations table should exist after migration")
	assert.True(t, tableSet["events"], "events table should exist after migration")

	// Verify history table has batch_job_id column
	type columnInfo struct {
		CID        int
		Name       string
		Type       string
		NotNull    int
		DefaultVal interface{}
		PK         int
	}
	var historyColumns []columnInfo
	err = db.Raw("PRAGMA table_info(history)").Scan(&historyColumns).Error
	require.NoError(t, err)

	historyColNames := make(map[string]bool, len(historyColumns))
	for _, col := range historyColumns {
		historyColNames[col.Name] = true
	}
	assert.True(t, historyColNames["batch_job_id"], "history table should have batch_job_id column")

	// Verify batch_file_operations table has all D-01 columns
	var bfoColumns []columnInfo
	err = db.Raw("PRAGMA table_info(batch_file_operations)").Scan(&bfoColumns).Error
	require.NoError(t, err)

	bfoColNames := make(map[string]bool, len(bfoColumns))
	for _, col := range bfoColumns {
		bfoColNames[col.Name] = true
	}

	expectedBFOColumns := []string{
		"id", "batch_job_id", "movie_id", "original_path", "new_path",
		"operation_type", "nfo_snapshot", "generated_files", "revert_status",
		"reverted_at", "in_place_renamed", "original_dir_path", "created_at", "updated_at",
	}
	for _, col := range expectedBFOColumns {
		assert.True(t, bfoColNames[col], "batch_file_operations should have %s column", col)
	}

	// Verify events table has all D-06 columns
	var eventColumns []columnInfo
	err = db.Raw("PRAGMA table_info(events)").Scan(&eventColumns).Error
	require.NoError(t, err)

	eventColNames := make(map[string]bool, len(eventColumns))
	for _, col := range eventColumns {
		eventColNames[col.Name] = true
	}

	expectedEventColumns := []string{
		"id", "event_type", "severity", "message", "context", "source", "created_at",
	}
	for _, col := range expectedEventColumns {
		assert.True(t, eventColNames[col], "events table should have %s column", col)
	}
}

// TestIntegration_CrossTableIndependence verifies that events, history, and
// batch_file_operations tables are independent — deleting all events does not
// affect history or batch_file_operations records.
func TestIntegration_CrossTableIndependence(t *testing.T) {
	db := newIntegrationTestDB(t)

	historyRepo := NewHistoryRepository(db)
	bfoRepo := NewBatchFileOperationRepository(db)
	eventRepo := NewEventRepository(db)

	// Create history record
	historyRecord := &models.History{
		MovieID:   "TEST-001",
		Operation: "scrape",
		Status:    "success",
	}
	require.NoError(t, historyRepo.Create(historyRecord))

	// Create batch_file_operations record
	bfoRecord := &models.BatchFileOperation{
		BatchJobID:    "job-001",
		OriginalPath:  "/source/file.mp4",
		NewPath:       "/dest/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	require.NoError(t, bfoRepo.Create(bfoRecord))

	// Create event record
	eventRecord := &models.Event{
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityInfo,
		Message:   "test event",
		Source:    "r18dev",
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, eventRepo.Create(eventRecord))

	// Delete all events
	cutoff := time.Now().UTC().Add(time.Hour) // future cutoff deletes everything
	require.NoError(t, eventRepo.DeleteOlderThan(cutoff))

	// Verify events are gone
	eventCount, err := eventRepo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(0), eventCount, "All events should be deleted")

	// Verify history is still accessible
	historyRecords, err := historyRepo.FindByMovieID("TEST-001")
	require.NoError(t, err)
	assert.Len(t, historyRecords, 1, "History record should still exist after events deletion")

	// Verify batch_file_operations is still accessible
	bfoRecords, err := bfoRepo.FindByBatchJobID("job-001")
	require.NoError(t, err)
	assert.Len(t, bfoRecords, 1, "BatchFileOperation record should still exist after events deletion")
}

// TestIntegration_BatchCentricQuery verifies HIST-12: history records can be
// queried by batch_job_id, returning all records in a batch.
func TestIntegration_BatchCentricQuery(t *testing.T) {
	db := newIntegrationTestDB(t)
	repo := NewHistoryRepository(db)

	batchJobID := "batch-abc-123"

	// Create history records with the same batch_job_id
	for i := 0; i < 3; i++ {
		record := &models.History{
			MovieID:    "TEST-" + string(rune('A'+i)),
			Operation:  "organize",
			Status:     "success",
			BatchJobID: strPtr(batchJobID),
		}
		require.NoError(t, repo.Create(record))
	}

	// Create a history record with a different batch_job_id
	isolatedRecord := &models.History{
		MovieID:    "TEST-ISOLATED",
		Operation:  "scrape",
		Status:     "success",
		BatchJobID: strPtr("other-batch"),
	}
	require.NoError(t, repo.Create(isolatedRecord))

	// Create a history record with no batch_job_id (legacy record)
	legacyRecord := &models.History{
		MovieID:   "TEST-LEGACY",
		Operation: "download",
		Status:    "success",
	}
	require.NoError(t, repo.Create(legacyRecord))

	// Query by batch_job_id
	results, err := repo.FindByBatchJobID(batchJobID)
	require.NoError(t, err)
	assert.Len(t, results, 3, "Should return exactly 3 records for the batch")

	// Verify all returned records have the correct batch_job_id
	for _, h := range results {
		require.NotNil(t, h.BatchJobID)
		assert.Equal(t, batchJobID, *h.BatchJobID)
	}

	// Verify query with non-existent batch returns empty
	emptyResults, err := repo.FindByBatchJobID("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, emptyResults, "Non-existent batch should return empty")
}

// TestIntegration_EventEmission verifies LOG-03: events are persisted with
// correct event_type and context JSON through the EventRepository. The
// EventEmitter interface wraps this repository; this test verifies the data
// path that EventEmitter produces.
func TestIntegration_EventEmission(t *testing.T) {
	db := newIntegrationTestDB(t)
	repo := NewEventRepository(db)

	// Simulate what EventEmitter.EmitScraperEvent does: create an Event
	// with event_type="scraper" and context JSON
	context := map[string]interface{}{
		"movie_id": "ABC-123",
		"url":      "https://example.com/abc-123",
		"duration": 1.5,
	}
	contextBytes, err := json.Marshal(context)
	require.NoError(t, err)

	scraperEvent := &models.Event{
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityInfo,
		Message:   "Scrape completed successfully",
		Context:   string(contextBytes),
		Source:    "r18dev",
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(scraperEvent))

	// Verify the event was stored with correct fields
	events, err := repo.FindByType(models.EventCategoryScraper, 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 1, "Should have exactly one scraper event")

	event := events[0]
	assert.Equal(t, models.EventCategoryScraper, event.EventType)
	assert.Equal(t, models.SeverityInfo, event.Severity)
	assert.Equal(t, "r18dev", event.Source)
	assert.Equal(t, "Scrape completed successfully", event.Message)

	// Verify context JSON was stored correctly
	assert.NotEmpty(t, event.Context, "Context should not be empty")
	var contextData map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(event.Context), &contextData))
	assert.Equal(t, "ABC-123", contextData["movie_id"])
	assert.Equal(t, "https://example.com/abc-123", contextData["url"])

	// Simulate organize and system events
	organizeEvent := &models.Event{
		EventType: models.EventCategoryOrganize,
		Severity:  models.SeverityInfo,
		Message:   "File moved successfully",
		Source:    "file_move",
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(organizeEvent))

	systemEvent := &models.Event{
		EventType: models.EventCategorySystem,
		Severity:  models.SeverityInfo,
		Message:   "Server started",
		Context:   `{"port":8080}`,
		Source:    "server",
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(systemEvent))

	// Verify all three event types are stored
	totalCount, err := repo.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(3), totalCount, "Should have 3 events total")

	scraperCount, err := repo.CountByType(models.EventCategoryScraper)
	require.NoError(t, err)
	assert.Equal(t, int64(1), scraperCount, "Should have 1 scraper event")

	organizeCount, err := repo.CountByType(models.EventCategoryOrganize)
	require.NoError(t, err)
	assert.Equal(t, int64(1), organizeCount, "Should have 1 organize event")

	systemCount, err := repo.CountByType(models.EventCategorySystem)
	require.NoError(t, err)
	assert.Equal(t, int64(1), systemCount, "Should have 1 system event")

	// Verify combined type+severity query works
	infoScraperEvents, err := repo.FindByTypeAndSeverity(models.EventCategoryScraper, models.SeverityInfo, 10, 0)
	require.NoError(t, err)
	assert.Len(t, infoScraperEvents, 1, "Should have 1 info-level scraper event")
}

// TestIntegration_ServerDependenciesSmokeTest verifies that the new
// repositories can be created from the same DB and work together without panic.
// This tests the DI wiring at the repository level.
func TestIntegration_ServerDependenciesSmokeTest(t *testing.T) {
	db := newIntegrationTestDB(t)

	// Verify the new repositories can be created from the same DB
	bfoRepo := NewBatchFileOperationRepository(db)
	eventRepo := NewEventRepository(db)

	// Verify they work correctly
	require.NoError(t, bfoRepo.Create(&models.BatchFileOperation{
		BatchJobID:    "smoke-test",
		OriginalPath:  "/a",
		NewPath:       "/b",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}))

	require.NoError(t, eventRepo.Create(&models.Event{
		EventType: models.EventCategorySystem,
		Severity:  models.SeverityDebug,
		Message:   "smoke test",
		Source:    "test",
		CreatedAt: time.Now().UTC(),
	}))

	// Verify both repos return data from the same DB
	bfoResults, err := bfoRepo.FindByBatchJobID("smoke-test")
	require.NoError(t, err)
	assert.Len(t, bfoResults, 1)

	eventCount, err := eventRepo.CountByType(models.EventCategorySystem)
	require.NoError(t, err)
	assert.Equal(t, int64(1), eventCount)
}

// strPtr is a helper to create a string pointer (for nullable BatchJobID field).
func strPtr(s string) *string {
	return &s
}
