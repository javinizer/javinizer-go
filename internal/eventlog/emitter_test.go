package eventlog

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEventTestDB creates an in-memory database for eventlog tests
func newEventTestDB(t *testing.T) *database.DB {
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
	db, err := database.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate())
	return db
}

func TestEmitScraperEvent(t *testing.T) {
	t.Parallel()
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)

	err := emitter.EmitScraperEvent("r18dev", "Scraped ABC-001 successfully", models.SeverityInfo, map[string]interface{}{
		"movie_id": "ABC-001",
		"url":      "https://r18.dev/videos/ABC-001",
	})
	require.NoError(t, err)

	// Verify the event was persisted correctly
	events, err := repo.FindByType(models.EventCategoryScraper, 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)

	event := events[0]
	assert.Equal(t, models.EventCategoryScraper, event.EventType)
	assert.Equal(t, models.SeverityInfo, event.Severity)
	assert.Equal(t, "Scraped ABC-001 successfully", event.Message)
	assert.Equal(t, "r18dev", event.Source)
	assert.NotZero(t, event.CreatedAt)

	// Verify context is valid JSON
	var context map[string]interface{}
	err = json.Unmarshal([]byte(event.Context), &context)
	require.NoError(t, err)
	assert.Equal(t, "ABC-001", context["movie_id"])
	assert.Equal(t, "https://r18.dev/videos/ABC-001", context["url"])
}

func TestEmitOrganizeEvent(t *testing.T) {
	t.Parallel()
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)

	err := emitter.EmitOrganizeEvent("file_move", "Moved ABC-001 to new location", models.SeverityInfo, map[string]interface{}{
		"movie_id": "ABC-001",
		"batch_id": "batch-001",
		"from":     "/original/path",
		"to":       "/new/path",
	})
	require.NoError(t, err)

	events, err := repo.FindByType(models.EventCategoryOrganize, 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)

	event := events[0]
	assert.Equal(t, models.EventCategoryOrganize, event.EventType)
	assert.Equal(t, models.SeverityInfo, event.Severity)
	assert.Equal(t, "file_move", event.Source)

	// Verify context contains batch_id
	var context map[string]interface{}
	err = json.Unmarshal([]byte(event.Context), &context)
	require.NoError(t, err)
	assert.Equal(t, "batch-001", context["batch_id"])
}

func TestEmitSystemEvent(t *testing.T) {
	t.Parallel()
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)

	err := emitter.EmitSystemEvent("server", "Server started on port 8080", models.SeverityInfo, map[string]interface{}{
		"port": 8080,
	})
	require.NoError(t, err)

	events, err := repo.FindByType(models.EventCategorySystem, 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)

	event := events[0]
	assert.Equal(t, models.EventCategorySystem, event.EventType)
	assert.Equal(t, models.SeverityInfo, event.Severity)
	assert.Equal(t, "server", event.Source)
	assert.Equal(t, "Server started on port 8080", event.Message)

	var context map[string]interface{}
	err = json.Unmarshal([]byte(event.Context), &context)
	require.NoError(t, err)
	assert.Equal(t, float64(8080), context["port"]) // JSON numbers decode as float64
}

func TestEmitter_SetsSourceField(t *testing.T) {
	t.Parallel()
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)

	require.NoError(t, emitter.EmitScraperEvent("dmm", "test", models.SeverityDebug, nil))
	require.NoError(t, emitter.EmitOrganizeEvent("nfo_gen", "test", models.SeverityInfo, nil))
	require.NoError(t, emitter.EmitSystemEvent("config", "test", models.SeverityWarn, nil))

	scraperEvents, err := repo.FindByType(models.EventCategoryScraper, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, "dmm", scraperEvents[0].Source)

	organizeEvents, err := repo.FindByType(models.EventCategoryOrganize, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, "nfo_gen", organizeEvents[0].Source)

	systemEvents, err := repo.FindByType(models.EventCategorySystem, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, "config", systemEvents[0].Source)
}

func TestEmitter_ContextAsJSON(t *testing.T) {
	t.Parallel()
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)

	// Test with nil context
	err := emitter.EmitScraperEvent("test", "nil context test", models.SeverityInfo, nil)
	require.NoError(t, err)

	events, err := repo.FindByType(models.EventCategoryScraper, 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "", events[0].Context) // nil context results in empty string
}

func TestEmitter_SeverityStoredCorrectly(t *testing.T) {
	t.Parallel()
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)

	severities := []string{models.SeverityDebug, models.SeverityInfo, models.SeverityWarn, models.SeverityError}
	for _, sev := range severities {
		err := emitter.EmitSystemEvent("test", "severity test", sev, nil)
		require.NoError(t, err)
	}

	events, err := repo.FindByType(models.EventCategorySystem, 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 4)

	gotSeverities := make(map[string]bool)
	for _, e := range events {
		gotSeverities[e.Severity] = true
	}
	for _, expected := range severities {
		assert.True(t, gotSeverities[expected], "expected severity %q to be present", expected)
	}
}

func TestEmitter_DoesNotFailCallerOnPersistenceError(t *testing.T) {
	// EventEmitter is fire-and-forget: a failure to emit should NOT block the caller.
	// The Emit methods return error for logging purposes, but callers should not
	// treat it as fatal.
	// Test that the error is returned (so callers can log it) but the pattern is clear.
	emitter := NewEmitter(nil) // nil repo will cause errors

	err := emitter.EmitScraperEvent("test", "should fail", models.SeverityError, nil)
	// The error is returned, but the caller decides what to do with it.
	// This test documents the fire-and-forget pattern: error is not fatal.
	assert.Error(t, err) // Error IS returned, but callers should not panic/fail
}

func TestEmitter_ContextMapPreserved(t *testing.T) {
	t.Parallel()
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)

	context := map[string]interface{}{
		"movie_id": "XYZ-123",
		"source":   "javbus",
		"batch_id": "batch-456",
		"error":    "timeout",
	}

	err := emitter.EmitScraperEvent("javbus", "Scrape timeout for XYZ-123", models.SeverityError, context)
	require.NoError(t, err)

	events, err := repo.FindByType(models.EventCategoryScraper, 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)

	var storedContext map[string]interface{}
	err = json.Unmarshal([]byte(events[0].Context), &storedContext)
	require.NoError(t, err)
	assert.Equal(t, "XYZ-123", storedContext["movie_id"])
	assert.Equal(t, "javbus", storedContext["source"])
	assert.Equal(t, "batch-456", storedContext["batch_id"])
	assert.Equal(t, "timeout", storedContext["error"])
}
