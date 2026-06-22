package history

import (
	"context"
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

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err, "Failed to create test database")

	err = db.RunMigrationsOnStartup(context.Background())
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

	logger := NewLogger(database.NewHistoryRepository(db))
	require.NotNil(t, logger, "NewLogger returned nil")
	require.NotNil(t, logger.repo, "Logger repository is nil")
}

func TestGetRecent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	// Create multiple records with explicit timestamps
	baseTime := time.Now().UTC()
	for i := 0; i < 5; i++ {
		movieID := fmt.Sprintf("IPX-%d", i+1)
		record := &models.History{
			MovieID:      movieID,
			Operation:    models.HistoryOpOrganize,
			OriginalPath: "/old/path.mp4",
			NewPath:      "/new/path.mp4",
			Status:       models.HistoryStatusSuccess,
			CreatedAt:    baseTime.Add(time.Duration(i) * time.Second),
		}
		err := repo.Create(context.Background(), record)
		require.NoError(t, err, "Failed to create record")
	}

	records, err := logger.GetRecent(context.Background(), 3)
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

	logger := NewLogger(database.NewHistoryRepository(db))

	records, err := logger.GetRecent(context.Background(), 10)
	require.NoError(t, err, "GetRecent failed")
	assert.Empty(t, records, "Expected no records in empty database")
}

func TestGetByMovieID_MultipleRecords(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	// Create multiple records for same movie with explicit timestamps
	baseTime := time.Now().UTC()

	organizeRecord := &models.History{
		MovieID:      "IPX-535",
		Operation:    models.HistoryOpOrganize,
		OriginalPath: "/old/path1.mp4",
		NewPath:      "/new/path1.mp4",
		Status:       models.HistoryStatusSuccess,
		CreatedAt:    baseTime,
	}
	err := repo.Create(context.Background(), organizeRecord)
	require.NoError(t, err)

	scrapeRecord := &models.History{
		MovieID:      "IPX-535",
		Operation:    models.HistoryOpScrape,
		OriginalPath: "https://example.com",
		Status:       models.HistoryStatusSuccess,
		CreatedAt:    baseTime.Add(1 * time.Second),
	}
	err = repo.Create(context.Background(), scrapeRecord)
	require.NoError(t, err)

	downloadRecord := &models.History{
		MovieID:      "IPX-535",
		Operation:    models.HistoryOpDownload,
		OriginalPath: "https://example.com/cover.jpg",
		NewPath:      "/local/cover.jpg",
		Status:       models.HistoryStatusSuccess,
		Metadata:     `{"media_type":"cover"}`,
		CreatedAt:    baseTime.Add(2 * time.Second),
	}
	err = repo.Create(context.Background(), downloadRecord)
	require.NoError(t, err)

	records, err := logger.GetByMovieID(context.Background(), "IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	assert.Len(t, records, 3, "Expected 3 records")

	// Verify order (most recent first)
	assert.False(t, records[0].CreatedAt.Before(records[1].CreatedAt))
	assert.False(t, records[1].CreatedAt.Before(records[2].CreatedAt))
}

func TestGetByMovieID_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))

	records, err := logger.GetByMovieID(context.Background(), "NONEXISTENT-123")
	require.NoError(t, err, "GetByMovieID should not error for non-existent movie")
	assert.Empty(t, records, "Expected no records for non-existent movie")
}

func TestGetByOperation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	baseTime := time.Now().UTC()

	// Create different operation types directly
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4", NewPath: "/new/path.mp4",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime,
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-536", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path2.mp4", NewPath: "/new/path2.mp4",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime.Add(1 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpScrape,
		OriginalPath: "https://example.com",
		Status:       models.HistoryStatusSuccess, CreatedAt: baseTime.Add(2 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpDownload,
		OriginalPath: "https://example.com/cover.jpg", NewPath: "/local/cover.jpg",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime.Add(3 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpNFO,
		NewPath: "/path/to/nfo",
		Status:  models.HistoryStatusSuccess, CreatedAt: baseTime.Add(4 * time.Second),
	})

	// Get only scrape operations
	scrapeRecords, err := logger.GetByOperation(context.Background(), models.HistoryOpScrape, 10)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, scrapeRecords, 1, "Expected 1 scrape record")
	assert.Equal(t, models.HistoryOpScrape, scrapeRecords[0].Operation)

	// Get only organize operations
	organizeRecords, err := logger.GetByOperation(context.Background(), models.HistoryOpOrganize, 10)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, organizeRecords, 2, "Expected 2 organize records")
	for _, record := range organizeRecords {
		assert.Equal(t, models.HistoryOpOrganize, record.Operation)
	}

	// Get download operations
	downloadRecords, err := logger.GetByOperation(context.Background(), models.HistoryOpDownload, 10)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, downloadRecords, 1, "Expected 1 download record")

	// Get NFO operations
	nfoRecords, err := logger.GetByOperation(context.Background(), models.HistoryOpNFO, 10)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, nfoRecords, 1, "Expected 1 NFO record")
}

func TestGetByOperation_WithLimit(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	// Create 5 scrape operations
	baseTime := time.Now().UTC()
	for i := 0; i < 5; i++ {
		err := repo.Create(context.Background(), &models.History{
			MovieID:      fmt.Sprintf("IPX-%d", i),
			Operation:    models.HistoryOpScrape,
			OriginalPath: "https://example.com",
			Status:       models.HistoryStatusSuccess,
			CreatedAt:    baseTime.Add(time.Duration(i) * time.Second),
		})
		require.NoError(t, err)
	}

	// Request only 3
	records, err := logger.GetByOperation(context.Background(), models.HistoryOpScrape, 3)
	require.NoError(t, err, "GetByOperation failed")
	assert.Len(t, records, 3, "Expected 3 records due to limit")
}

func TestGetByStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	baseTime := time.Now().UTC()

	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4", NewPath: "/new/path.mp4",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime,
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-536", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4", NewPath: "/new/path.mp4",
		Status: models.HistoryStatusFailed, ErrorMessage: "error", CreatedAt: baseTime.Add(1 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-537", Operation: models.HistoryOpOrganize,
		OriginalPath: "/organized/path.mp4", NewPath: "/original/path.mp4",
		Status: models.HistoryStatusReverted, CreatedAt: baseTime.Add(2 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-538", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4", NewPath: "/new/path.mp4",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime.Add(3 * time.Second),
	})

	// Get only successful operations
	successRecords, err := logger.GetByStatus(context.Background(), models.HistoryStatusSuccess, 10)
	require.NoError(t, err, "GetByStatus failed")
	assert.Len(t, successRecords, 2, "Expected 2 success records")

	// Get only failed operations
	failedRecords, err := logger.GetByStatus(context.Background(), models.HistoryStatusFailed, 10)
	require.NoError(t, err, "GetByStatus failed")
	assert.Len(t, failedRecords, 1, "Expected 1 failed record")

	// Get only reverted operations
	revertedRecords, err := logger.GetByStatus(context.Background(), models.HistoryStatusReverted, 10)
	require.NoError(t, err, "GetByStatus failed")
	assert.Len(t, revertedRecords, 1, "Expected 1 reverted record")
}

func TestGetStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	baseTime := time.Now().UTC()

	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4", NewPath: "/new/path.mp4",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime,
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-536", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4", NewPath: "/new/path.mp4",
		Status: models.HistoryStatusFailed, ErrorMessage: "error", CreatedAt: baseTime.Add(1 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpScrape,
		OriginalPath: "https://example.com",
		Status:       models.HistoryStatusSuccess, CreatedAt: baseTime.Add(2 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpDownload,
		OriginalPath: "https://example.com/cover.jpg", NewPath: "/local/cover.jpg",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime.Add(3 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpNFO,
		NewPath: "/path/to/nfo",
		Status:  models.HistoryStatusSuccess, CreatedAt: baseTime.Add(4 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpOrganize,
		OriginalPath: "/organized/path.mp4", NewPath: "/original/path.mp4",
		Status: models.HistoryStatusReverted, CreatedAt: baseTime.Add(5 * time.Second),
	})

	stats, err := logger.GetStats(context.Background())
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

	logger := NewLogger(database.NewHistoryRepository(db))

	stats, err := logger.GetStats(context.Background())
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

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	// Create old record (60 days ago)
	oldRecord := &models.History{
		MovieID:      "IPX-535",
		Operation:    models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4",
		NewPath:      "/new/path.mp4",
		Status:       models.HistoryStatusSuccess,
		CreatedAt:    time.Now().UTC().Add(-60 * 24 * time.Hour),
	}
	err := repo.Create(context.Background(), oldRecord)
	require.NoError(t, err)

	// Create recent record
	recentRecord := &models.History{
		MovieID:      "IPX-536",
		Operation:    models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4",
		NewPath:      "/new/path.mp4",
		Status:       models.HistoryStatusSuccess,
		CreatedAt:    time.Now().UTC(),
	}
	err = repo.Create(context.Background(), recentRecord)
	require.NoError(t, err)

	// Verify 2 records exist
	allRecords, err := logger.GetRecent(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, allRecords, 2, "Expected 2 records before cleanup")

	// Cleanup records older than 30 days
	err = logger.CleanupOldRecords(context.Background(), 30*24*time.Hour)
	require.NoError(t, err, "CleanupOldRecords failed")

	// Verify only 1 record remains
	remainingRecords, err := logger.GetRecent(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, remainingRecords, 1, "Expected 1 record after cleanup")
	assert.Equal(t, "IPX-536", remainingRecords[0].MovieID)
}

func TestCleanupOldRecords_NoRecordsToDelete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	// Create recent records only
	err := repo.Create(context.Background(), &models.History{
		MovieID:      "IPX-535",
		Operation:    models.HistoryOpOrganize,
		OriginalPath: "/old/path.mp4",
		NewPath:      "/new/path.mp4",
		Status:       models.HistoryStatusSuccess,
		CreatedAt:    time.Now().UTC(),
	})
	require.NoError(t, err)

	// Cleanup (should not delete anything)
	err = logger.CleanupOldRecords(context.Background(), 30*24*time.Hour)
	require.NoError(t, err, "CleanupOldRecords failed")

	// Verify record still exists
	records, err := logger.GetRecent(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, records, 1, "Expected record to still exist")
}

func TestMultipleMovies(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	baseTime := time.Now().UTC()

	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path1.mp4", NewPath: "/new/path1.mp4",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime,
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-536", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path2.mp4", NewPath: "/new/path2.mp4",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime.Add(1 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-535", Operation: models.HistoryOpOrganize,
		OriginalPath: "/old/path3.mp4", NewPath: "/new/path3.mp4",
		Status: models.HistoryStatusSuccess, CreatedAt: baseTime.Add(2 * time.Second),
	})
	repo.Create(context.Background(), &models.History{
		MovieID: "IPX-536", Operation: models.HistoryOpScrape,
		OriginalPath: "https://example.com",
		Status:       models.HistoryStatusSuccess, CreatedAt: baseTime.Add(3 * time.Second),
	})

	// Get records for specific movie
	ipx535Records, err := logger.GetByMovieID(context.Background(), "IPX-535")
	require.NoError(t, err, "GetByMovieID failed")
	assert.Len(t, ipx535Records, 2, "Expected 2 records for IPX-535")

	ipx536Records, err := logger.GetByMovieID(context.Background(), "IPX-536")
	require.NoError(t, err, "GetByMovieID failed")
	assert.Len(t, ipx536Records, 2, "Expected 2 records for IPX-536")
}

func TestConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	// Perform concurrent writes
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer func() { done <- true }()
			movieID := "IPX-" + string(rune('0'+index))
			err := repo.Create(context.Background(), &models.History{
				MovieID:      movieID,
				Operation:    models.HistoryOpOrganize,
				OriginalPath: "/old/path.mp4",
				NewPath:      "/new/path.mp4",
				Status:       models.HistoryStatusSuccess,
				CreatedAt:    time.Now().UTC(),
			})
			if err != nil {
				t.Errorf("Concurrent repo.Create failed: %v", err)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all records were created
	stats, err := logger.GetStats(context.Background())
	require.NoError(t, err, "GetStats failed")
	assert.Equal(t, int64(numGoroutines), stats.Total, "Expected all concurrent operations to be recorded")
}

// Benchmark tests
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

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	if err != nil {
		b.Fatalf("Failed to create test database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		b.Fatalf("Failed to run migrations: %v", err)
	}

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	// Populate with test data
	for i := 0; i < 1000; i++ {
		movieID := fmt.Sprintf("IPX-%d", i%100)
		_ = repo.Create(context.Background(), &models.History{
			MovieID:      movieID,
			Operation:    models.HistoryOpOrganize,
			OriginalPath: "/old/path.mp4",
			NewPath:      "/new/path.mp4",
			Status:       models.HistoryStatusSuccess,
			CreatedAt:    time.Now().UTC(),
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := logger.GetStats(context.Background())
		if err != nil {
			b.Fatalf("GetStats failed: %v", err)
		}
	}
}
