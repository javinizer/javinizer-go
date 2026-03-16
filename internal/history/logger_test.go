package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) (*database.DB, func()) {
	t.Helper()

	// Create temp directory for test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err, "Failed to create test database")

	err = db.AutoMigrate()
	require.NoError(t, err, "Failed to run migrations")

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func TestNewLogger(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)
	require.NotNil(t, logger, "NewLogger returned nil")
	require.NotNil(t, logger.repo, "Logger repository is nil")
}

func TestLogOrganize_Success(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	err := logger.LogOrganize("IPX-535", "/old/path.mp4", "/new/path.mp4", false, nil)
	require.NoError(t, err, "LogOrganize failed")

	// Verify the record was created
	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "IPX-535", record.MovieID)
	assert.Equal(t, "organize", record.Operation)
	assert.Equal(t, "/old/path.mp4", record.OriginalPath)
	assert.Equal(t, "/new/path.mp4", record.NewPath)
	assert.Equal(t, "success", record.Status)
	assert.False(t, record.DryRun)
	assert.Empty(t, record.ErrorMessage)
}

func TestLogOrganize_Failed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	testErr := &os.PathError{Op: "move", Path: "/test/path", Err: os.ErrNotExist}
	err := logger.LogOrganize("IPX-535", "/old/path.mp4", "/new/path.mp4", false, testErr)
	require.NoError(t, err, "LogOrganize failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "failed", record.Status)
	assert.NotEmpty(t, record.ErrorMessage, "Expected error message to be set")
	assert.Contains(t, record.ErrorMessage, "file does not exist")
}

func TestLogOrganize_DryRun(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	err := logger.LogOrganize("IPX-535", "/old/path.mp4", "/new/path.mp4", true, nil)
	require.NoError(t, err, "LogOrganize failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.True(t, record.DryRun, "Expected DryRun to be true")
}

func TestLogScrape_Success(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	metadata := map[string]string{
		"title":  "Test Movie",
		"studio": "Test Studio",
	}

	err := logger.LogScrape("IPX-535", "https://r18.dev/videos/vod/movies/detail/-/id=ipx00535", metadata, nil)
	require.NoError(t, err, "LogScrape failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "scrape", record.Operation)
	assert.Equal(t, "success", record.Status)
	assert.NotEmpty(t, record.Metadata, "Expected metadata to be set")
	assert.Empty(t, record.ErrorMessage)

	// Verify metadata is valid JSON
	var metadataMap map[string]string
	err = json.Unmarshal([]byte(record.Metadata), &metadataMap)
	require.NoError(t, err, "Metadata should be valid JSON")
	assert.Equal(t, "Test Movie", metadataMap["title"])
	assert.Equal(t, "Test Studio", metadataMap["studio"])
}

func TestLogScrape_Failed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	testErr := errors.New("network timeout")
	err := logger.LogScrape("IPX-535", "https://example.com", nil, testErr)
	require.NoError(t, err, "LogScrape failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "failed", record.Status)
	assert.Equal(t, "network timeout", record.ErrorMessage)
}

func TestLogScrape_NilMetadata(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	err := logger.LogScrape("IPX-535", "https://example.com", nil, nil)
	require.NoError(t, err, "LogScrape failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Empty(t, record.Metadata, "Expected empty metadata")
}

func TestLogDownload_Success(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	err := logger.LogDownload("IPX-535", "https://example.com/cover.jpg", "/local/cover.jpg", "cover", nil)
	require.NoError(t, err, "LogDownload failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "download", record.Operation)
	assert.Equal(t, "https://example.com/cover.jpg", record.OriginalPath)
	assert.Equal(t, "/local/cover.jpg", record.NewPath)
	assert.Equal(t, "success", record.Status)

	// Verify metadata contains media type
	var metadata map[string]string
	err = json.Unmarshal([]byte(record.Metadata), &metadata)
	require.NoError(t, err, "Metadata should be valid JSON")
	assert.Equal(t, "cover", metadata["media_type"])
}

func TestLogDownload_Failed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	testErr := errors.New("download failed")
	err := logger.LogDownload("IPX-535", "https://example.com/cover.jpg", "/local/cover.jpg", "cover", testErr)
	require.NoError(t, err, "LogDownload failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "failed", record.Status)
	assert.Equal(t, "download failed", record.ErrorMessage)
}

func TestLogDownload_DifferentMediaTypes(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	mediaTypes := []string{"cover", "screenshot", "trailer"}
	for _, mediaType := range mediaTypes {
		err := logger.LogDownload("IPX-535", "https://example.com/media", "/local/media", mediaType, nil)
		require.NoError(t, err, "LogDownload failed for %s", mediaType)
	}

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	assert.Len(t, records, len(mediaTypes))
}

func TestLogNFO_Success(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	err := logger.LogNFO("IPX-535", "/path/to/IPX-535.nfo", nil)
	require.NoError(t, err, "LogNFO failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "nfo", record.Operation)
	assert.Equal(t, "/path/to/IPX-535.nfo", record.NewPath)
	assert.Equal(t, "success", record.Status)
	assert.Empty(t, record.OriginalPath)
}

func TestLogNFO_Failed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	testErr := errors.New("permission denied")
	err := logger.LogNFO("IPX-535", "/path/to/IPX-535.nfo", testErr)
	require.NoError(t, err, "LogNFO failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "failed", record.Status)
	assert.Equal(t, "permission denied", record.ErrorMessage)
}

func TestLogRevert_Success(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	err := logger.LogRevert("IPX-535", "/original/path.mp4", "/organized/path.mp4", nil)
	require.NoError(t, err, "LogRevert failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "reverted", record.Status)
	assert.Equal(t, "organize", record.Operation)
	assert.Equal(t, "/organized/path.mp4", record.OriginalPath)
	assert.Equal(t, "/original/path.mp4", record.NewPath)
}

func TestLogRevert_Failed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	testErr := errors.New("revert failed")
	err := logger.LogRevert("IPX-535", "/original/path.mp4", "/organized/path.mp4", testErr)
	require.NoError(t, err, "LogRevert failed")

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	require.Len(t, records, 1, "Expected 1 record")

	record := records[0]
	assert.Equal(t, "failed", record.Status)
	assert.Equal(t, "revert failed", record.ErrorMessage)
}

func TestGetRecent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)
	repo := database.NewHistoryRepository(db)

	// Create multiple records with explicit timestamps
	baseTime := time.Now().UTC()
	for i := 0; i < 5; i++ {
		movieID := fmt.Sprintf("IPX-%d", i+1)
		record := &models.History{
			MovieID:      movieID,
			Operation:    "organize",
			OriginalPath: "/old/path.mp4",
			NewPath:      "/new/path.mp4",
			Status:       "success",
			CreatedAt:    baseTime.Add(time.Duration(i) * time.Second),
		}
		err := repo.Create(record)
		require.NoError(t, err, "Failed to create record")
	}

	records, err := logger.GetRecent(3)
	require.NoError(t, err, "GetRecent failed")
	assert.Len(t, records, 3, "Expected 3 records")

	// Verify they are in reverse chronological order
	for i := 0; i < len(records)-1; i++ {
		assert.False(t, records[i].CreatedAt.Before(records[i+1].CreatedAt),
			"Records should be in reverse chronological order")
	}
}

func TestGetRecent_EmptyDatabase(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	records, err := logger.GetRecent(10)
	require.NoError(t, err, "GetRecent failed")
	assert.Empty(t, records, "Expected no records in empty database")
}

func TestGetByMovieID_MultipleRecords(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)
	repo := database.NewHistoryRepository(db)

	// Create multiple records for same movie with explicit timestamps
	baseTime := time.Now().UTC()

	organizeRecord := &models.History{
		MovieID:      "IPX-535",
		Operation:    "organize",
		OriginalPath: "/old/path1.mp4",
		NewPath:      "/new/path1.mp4",
		Status:       "success",
		CreatedAt:    baseTime,
	}
	err := repo.Create(organizeRecord)
	require.NoError(t, err)

	scrapeRecord := &models.History{
		MovieID:      "IPX-535",
		Operation:    "scrape",
		OriginalPath: "https://example.com",
		Status:       "success",
		CreatedAt:    baseTime.Add(1 * time.Second),
	}
	err = repo.Create(scrapeRecord)
	require.NoError(t, err)

	downloadRecord := &models.History{
		MovieID:      "IPX-535",
		Operation:    "download",
		OriginalPath: "https://example.com/cover.jpg",
		NewPath:      "/local/cover.jpg",
		Status:       "success",
		Metadata:     `{"media_type":"cover"}`,
		CreatedAt:    baseTime.Add(2 * time.Second),
	}
	err = repo.Create(downloadRecord)
	require.NoError(t, err)

	records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	assert.Len(t, records, 3, "Expected 3 records")

	// Verify order (most recent first)
	assert.False(t, records[0].CreatedAt.Before(records[1].CreatedAt))
	assert.False(t, records[1].CreatedAt.Before(records[2].CreatedAt))
}

func TestGetByMovieID_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	records, err := logger.GetByMovieID("NONEXISTENT-123")
	require.NoError(t, err, "GetByMovieID should not error for non-existent movie")
	assert.Empty(t, records, "Expected no records for non-existent movie")
}

func TestGetByOperation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	// Create different operation types
	_ = logger.LogOrganize("IPX-535", "/old/path.mp4", "/new/path.mp4", false, nil)
	_ = logger.LogOrganize("IPX-536", "/old/path2.mp4", "/new/path2.mp4", false, nil)
	_ = logger.LogScrape("IPX-535", "https://example.com", nil, nil)
	_ = logger.LogDownload("IPX-535", "https://example.com/cover.jpg", "/local/cover.jpg", "cover", nil)
	_ = logger.LogNFO("IPX-535", "/path/to/nfo", nil)

	// Get only scrape operations
	scrapeRecords, err := logger.GetByOperation("scrape", 10)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, scrapeRecords, 1, "Expected 1 scrape record")
	assert.Equal(t, "scrape", scrapeRecords[0].Operation)

	// Get only organize operations
	organizeRecords, err := logger.GetByOperation("organize", 10)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, organizeRecords, 2, "Expected 2 organize records")
	for _, record := range organizeRecords {
		assert.Equal(t, "organize", record.Operation)
	}

	// Get download operations
	downloadRecords, err := logger.GetByOperation("download", 10)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, downloadRecords, 1, "Expected 1 download record")

	// Get NFO operations
	nfoRecords, err := logger.GetByOperation("nfo", 10)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, nfoRecords, 1, "Expected 1 NFO record")
}

func TestGetByOperation_WithLimit(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	// Create 5 scrape operations
	for i := 0; i < 5; i++ {
		err := logger.LogScrape("IPX-"+string(rune('0'+i)), "https://example.com", nil, nil)
		require.NoError(t, err)
	}

	// Request only 3
	records, err := logger.GetByOperation("scrape", 3)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, records, 3, "Expected 3 records due to limit")
}

func TestGetByStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	// Create records with different statuses
	_ = logger.LogOrganize("IPX-535", "/old/path.mp4", "/new/path.mp4", false, nil)
	_ = logger.LogOrganize("IPX-536", "/old/path.mp4", "/new/path.mp4", false, errors.New("error"))
	_ = logger.LogRevert("IPX-537", "/original/path.mp4", "/organized/path.mp4", nil)
	_ = logger.LogOrganize("IPX-538", "/old/path.mp4", "/new/path.mp4", false, nil)

	// Get only successful operations
	successRecords, err := logger.GetByStatus("success", 10)
	require.NoError(t, err, "GetByStatus failed")
	assert.Len(t, successRecords, 2, "Expected 2 success records")

	// Get only failed operations
	failedRecords, err := logger.GetByStatus("failed", 10)
	require.NoError(t, err, "GetByStatus failed")
	assert.Len(t, failedRecords, 1, "Expected 1 failed record")

	// Get only reverted operations
	revertedRecords, err := logger.GetByStatus("reverted", 10)
	require.NoError(t, err, "GetByStatus failed")
	assert.Len(t, revertedRecords, 1, "Expected 1 reverted record")
}

func TestGetStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	// Create various records
	_ = logger.LogOrganize("IPX-535", "/old/path.mp4", "/new/path.mp4", false, nil)
	_ = logger.LogOrganize("IPX-536", "/old/path.mp4", "/new/path.mp4", false, errors.New("error"))
	_ = logger.LogScrape("IPX-535", "https://example.com", nil, nil)
	_ = logger.LogDownload("IPX-535", "https://example.com/cover.jpg", "/local/cover.jpg", "cover", nil)
	_ = logger.LogNFO("IPX-535", "/path/to/nfo", nil)
	_ = logger.LogRevert("IPX-535", "/original/path.mp4", "/organized/path.mp4", nil)

	stats, err := logger.GetStats()
	require.NoError(t, err, "GetStats failed")

	assert.Equal(t, int64(6), stats.Total)
	assert.Equal(t, int64(4), stats.Success)
	assert.Equal(t, int64(1), stats.Failed)
	assert.Equal(t, int64(1), stats.Reverted)
	assert.Equal(t, int64(1), stats.Scrape)
	assert.Equal(t, int64(3), stats.Organize, "Expected 3 organize (2 organize + 1 revert)")
	assert.Equal(t, int64(1), stats.Download)
	assert.Equal(t, int64(1), stats.NFO)
}

func TestGetStats_EmptyDatabase(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	stats, err := logger.GetStats()
	require.NoError(t, err, "GetStats failed")

	assert.Equal(t, int64(0), stats.Total)
	assert.Equal(t, int64(0), stats.Success)
	assert.Equal(t, int64(0), stats.Failed)
	assert.Equal(t, int64(0), stats.Reverted)
	assert.Equal(t, int64(0), stats.Scrape)
	assert.Equal(t, int64(0), stats.Organize)
	assert.Equal(t, int64(0), stats.Download)
	assert.Equal(t, int64(0), stats.NFO)
}

func TestCleanupOldRecords(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)
	repo := database.NewHistoryRepository(db)

	// Create old record (60 days ago)
	oldRecord := &models.History{
		MovieID:      "IPX-535",
		Operation:    "organize",
		OriginalPath: "/old/path.mp4",
		NewPath:      "/new/path.mp4",
		Status:       "success",
		CreatedAt:    time.Now().UTC().Add(-60 * 24 * time.Hour),
	}
	err := repo.Create(oldRecord)
	require.NoError(t, err)

	// Create recent record
	err = logger.LogOrganize("IPX-536", "/old/path.mp4", "/new/path.mp4", false, nil)
	require.NoError(t, err)

	// Verify 2 records exist
	allRecords, err := logger.GetRecent(10)
	require.NoError(t, err)
	assert.Len(t, allRecords, 2, "Expected 2 records before cleanup")

	// Cleanup records older than 30 days
	err = logger.CleanupOldRecords(30 * 24 * time.Hour)
	require.NoError(t, err, "CleanupOldRecords failed")

	// Verify only 1 record remains
	remainingRecords, err := logger.GetRecent(10)
	require.NoError(t, err)
	assert.Len(t, remainingRecords, 1, "Expected 1 record after cleanup")
	assert.Equal(t, "IPX-536", remainingRecords[0].MovieID)
}

func TestCleanupOldRecords_NoRecordsToDelete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	// Create recent records only
	err := logger.LogOrganize("IPX-535", "/old/path.mp4", "/new/path.mp4", false, nil)
	require.NoError(t, err)

	// Cleanup (should not delete anything)
	err = logger.CleanupOldRecords(30 * 24 * time.Hour)
	require.NoError(t, err, "CleanupOldRecords failed")

	// Verify record still exists
	records, err := logger.GetRecent(10)
	require.NoError(t, err)
	assert.Len(t, records, 1, "Expected record to still exist")
}

func TestMultipleMovies(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	// Create records for multiple movies
	_ = logger.LogOrganize("IPX-535", "/old/path1.mp4", "/new/path1.mp4", false, nil)
	_ = logger.LogOrganize("IPX-536", "/old/path2.mp4", "/new/path2.mp4", false, nil)
	_ = logger.LogOrganize("IPX-535", "/old/path3.mp4", "/new/path3.mp4", false, nil)
	_ = logger.LogScrape("IPX-536", "https://example.com", nil, nil)

	// Get records for specific movie
	ipx535Records, err := logger.GetByMovieID("IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	assert.Len(t, ipx535Records, 2, "Expected 2 records for IPX-535")

	ipx536Records, err := logger.GetByMovieID("IPX-536")
	require.NoError(t, err, "GetByMovieID failed")
	assert.Len(t, ipx536Records, 2, "Expected 2 records for IPX-536")
}

func TestConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	// Perform concurrent writes
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer func() { done <- true }()
			movieID := "IPX-" + string(rune('0'+index))
			err := logger.LogOrganize(movieID, "/old/path.mp4", "/new/path.mp4", false, nil)
			if err != nil {
				t.Errorf("Concurrent LogOrganize failed: %v", err)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all records were created
	stats, err := logger.GetStats()
	require.NoError(t, err, "GetStats failed")
	assert.Equal(t, int64(numGoroutines), stats.Total, "Expected all concurrent operations to be recorded")
}

func TestInvalidInput(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(db)

	tests := []struct {
		name         string
		movieID      string
		originalPath string
		newPath      string
		err          error
		wantFail     bool
		verifyFn     func(t *testing.T, record models.History)
	}{
		{
			name:         "empty movie ID",
			movieID:      "",
			originalPath: "/old/path.mp4",
			newPath:      "/new/path.mp4",
			err:          nil,
			wantFail:     false,
			verifyFn: func(t *testing.T, record models.History) {
				assert.Empty(t, record.MovieID, "Expected empty movie ID to be stored")
				assert.Equal(t, "/old/path.mp4", record.OriginalPath)
				assert.Equal(t, "/new/path.mp4", record.NewPath)
				assert.Equal(t, "success", record.Status)
			},
		},
		{
			name:         "empty paths",
			movieID:      "IPX-535",
			originalPath: "",
			newPath:      "",
			err:          nil,
			wantFail:     false,
			verifyFn: func(t *testing.T, record models.History) {
				assert.Equal(t, "IPX-535", record.MovieID)
				assert.Empty(t, record.OriginalPath, "Expected empty original path to be stored")
				assert.Empty(t, record.NewPath, "Expected empty new path to be stored")
				assert.Equal(t, "success", record.Status)
			},
		},
		{
			name:         "very long error message",
			movieID:      "IPX-536",
			originalPath: "/old",
			newPath:      "/new",
			err:          errors.New(string(make([]byte, 10000))),
			wantFail:     false,
			verifyFn: func(t *testing.T, record models.History) {
				assert.Equal(t, "IPX-536", record.MovieID)
				assert.Equal(t, "failed", record.Status)
				assert.NotEmpty(t, record.ErrorMessage, "Expected long error message to be stored")
				assert.Len(t, record.ErrorMessage, 10000, "Expected full error message to be preserved")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := logger.LogOrganize(tt.movieID, tt.originalPath, tt.newPath, false, tt.err)
			if tt.wantFail {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify the record was stored correctly
			records, err := logger.GetRecent(10)
			require.NoError(t, err, "Failed to retrieve records")
			require.NotEmpty(t, records, "Expected at least one record to be stored")

			// Find our record (it should be the most recent with matching movie ID)
			var found *models.History
			for i := range records {
				if records[i].MovieID == tt.movieID &&
					records[i].OriginalPath == tt.originalPath &&
					records[i].NewPath == tt.newPath {
					found = &records[i]
					break
				}
			}
			require.NotNil(t, found, "Expected to find stored record")

			// Run custom verification
			if tt.verifyFn != nil {
				tt.verifyFn(t, *found)
			}
		})
	}
}

// Benchmark tests
func BenchmarkLogOrganize(b *testing.B) {
	// Setup
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
	}

	db, err := database.New(cfg)
	if err != nil {
		b.Fatalf("Failed to create test database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.AutoMigrate(); err != nil {
		b.Fatalf("Failed to run migrations: %v", err)
	}

	logger := NewLogger(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		movieID := fmt.Sprintf("IPX-%d", i%1000)
		err := logger.LogOrganize(movieID, "/old/path.mp4", "/new/path.mp4", false, nil)
		if err != nil {
			b.Fatalf("LogOrganize failed: %v", err)
		}
	}
}

func BenchmarkGetStats(b *testing.B) {
	// Setup
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
	}

	db, err := database.New(cfg)
	if err != nil {
		b.Fatalf("Failed to create test database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.AutoMigrate(); err != nil {
		b.Fatalf("Failed to run migrations: %v", err)
	}

	logger := NewLogger(db)

	// Populate with test data
	for i := 0; i < 1000; i++ {
		movieID := fmt.Sprintf("IPX-%d", i%100)
		_ = logger.LogOrganize(movieID, "/old/path.mp4", "/new/path.mp4", false, nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := logger.GetStats()
		if err != nil {
			b.Fatalf("GetStats failed: %v", err)
		}
	}
}
