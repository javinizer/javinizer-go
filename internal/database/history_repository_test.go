package database

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryRepository(t *testing.T) {
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
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewHistoryRepository(db)

	t.Run("Create history record", func(t *testing.T) {
		history := &models.History{
			MovieID:      "IPX-001",
			Operation:    "scrape",
			Status:       "success",
			OriginalPath: "/path/to/original.mp4",
			NewPath:      "/path/to/new.mp4",
		}

		err := repo.Create(history)
		require.NoError(t, err)
		assert.NotZero(t, history.ID)
	})

	t.Run("FindByID", func(t *testing.T) {
		history := &models.History{
			MovieID:   "IPX-002",
			Operation: "organize",
			Status:    "success",
		}

		err := repo.Create(history)
		require.NoError(t, err)

		found, err := repo.FindByID(history.ID)
		require.NoError(t, err)
		assert.Equal(t, "IPX-002", found.MovieID)
		assert.Equal(t, "organize", found.Operation)
	})

	t.Run("FindByID not found", func(t *testing.T) {
		_, err := repo.FindByID(99999)
		assert.Error(t, err)
	})

	t.Run("FindByMovieID", func(t *testing.T) {
		// Create multiple history records for same movie
		histories := []*models.History{
			{MovieID: "IPX-100", Operation: "scrape", Status: "success"},
			{MovieID: "IPX-100", Operation: "download", Status: "success"},
			{MovieID: "IPX-100", Operation: "organize", Status: "success"},
		}

		for _, h := range histories {
			err := repo.Create(h)
			require.NoError(t, err)
		}

		// Find all history for this movie
		results, err := repo.FindByMovieID("IPX-100")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 3)

		// Verify operations
		operations := make(map[string]bool)
		for _, r := range results {
			if r.MovieID == "IPX-100" {
				operations[r.Operation] = true
			}
		}
		assert.True(t, operations["scrape"])
		assert.True(t, operations["download"])
		assert.True(t, operations["organize"])
	})

	t.Run("FindByOperation", func(t *testing.T) {
		// Create history records with different operations
		histories := []*models.History{
			{MovieID: "IPX-200", Operation: "scrape", Status: "success"},
			{MovieID: "IPX-201", Operation: "scrape", Status: "success"},
			{MovieID: "IPX-202", Operation: "download", Status: "success"},
		}

		for _, h := range histories {
			err := repo.Create(h)
			require.NoError(t, err)
		}

		// Find all scrape operations
		results, err := repo.FindByOperation("scrape", 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 2)

		// Verify all are scrape operations
		for _, r := range results {
			if r.MovieID == "IPX-200" || r.MovieID == "IPX-201" {
				assert.Equal(t, "scrape", r.Operation)
			}
		}
	})

	t.Run("FindByOperation with limit", func(t *testing.T) {
		// Create many records
		for i := 0; i < 20; i++ {
			history := &models.History{
				MovieID:   "IPX-300",
				Operation: "test_op",
				Status:    "success",
			}
			err := repo.Create(history)
			require.NoError(t, err)
		}

		// Find with limit
		results, err := repo.FindByOperation("test_op", 5)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(results), 5)
	})

	t.Run("FindByStatus", func(t *testing.T) {
		// Create records with different statuses
		histories := []*models.History{
			{MovieID: "IPX-400", Operation: "scrape", Status: "success"},
			{MovieID: "IPX-401", Operation: "scrape", Status: "failed"},
			{MovieID: "IPX-402", Operation: "scrape", Status: "success"},
		}

		for _, h := range histories {
			err := repo.Create(h)
			require.NoError(t, err)
		}

		// Find all successful operations
		results, err := repo.FindByStatus("success", 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 2)

		// Verify all have success status
		for _, r := range results {
			if r.MovieID == "IPX-400" || r.MovieID == "IPX-402" {
				assert.Equal(t, "success", r.Status)
			}
		}
	})

	t.Run("FindRecent", func(t *testing.T) {
		// Create multiple records
		for i := 0; i < 10; i++ {
			history := &models.History{
				MovieID:   "IPX-500",
				Operation: "recent_test",
				Status:    "success",
			}
			err := repo.Create(history)
			require.NoError(t, err)
			time.Sleep(1 * time.Millisecond) // Ensure different timestamps
		}

		// Get recent records
		results, err := repo.FindRecent(5)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(results), 5)

		// Verify they are sorted by created_at DESC
		if len(results) >= 2 {
			for i := 0; i < len(results)-1; i++ {
				assert.True(t, results[i].CreatedAt.After(results[i+1].CreatedAt) ||
					results[i].CreatedAt.Equal(results[i+1].CreatedAt))
			}
		}
	})

	t.Run("FindByDateRange", func(t *testing.T) {
		// Create a record first
		history := &models.History{
			MovieID:   "IPX-600",
			Operation: "date_test",
			Status:    "success",
		}
		err := repo.Create(history)
		require.NoError(t, err)

		// Use actual creation time for date range
		now := history.CreatedAt
		start := now.Add(-1 * time.Second)
		end := now.Add(1 * time.Second)

		// Find by date range
		results, err := repo.FindByDateRange(start, end)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 1, "Should find at least one record")

		// Verify record is in the results
		found := false
		for _, r := range results {
			if r.MovieID == "IPX-600" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find the created record")
	})

	t.Run("Count", func(t *testing.T) {
		// Get initial count
		initialCount, err := repo.Count()
		require.NoError(t, err)

		// Create new records
		for i := 0; i < 3; i++ {
			history := &models.History{
				MovieID:   "IPX-700",
				Operation: "count_test",
				Status:    "success",
			}
			err := repo.Create(history)
			require.NoError(t, err)
		}

		// Get new count
		newCount, err := repo.Count()
		require.NoError(t, err)
		assert.Equal(t, initialCount+3, newCount)
	})

	t.Run("CountByStatus", func(t *testing.T) {
		// Create records with specific status
		for i := 0; i < 5; i++ {
			history := &models.History{
				MovieID:   "IPX-800",
				Operation: "status_count_test",
				Status:    "pending",
			}
			err := repo.Create(history)
			require.NoError(t, err)
		}

		count, err := repo.CountByStatus("pending")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(5))
	})

	t.Run("CountByOperation", func(t *testing.T) {
		// Create records with specific operation
		for i := 0; i < 7; i++ {
			history := &models.History{
				MovieID:   "IPX-900",
				Operation: "op_count_test",
				Status:    "success",
			}
			err := repo.Create(history)
			require.NoError(t, err)
		}

		count, err := repo.CountByOperation("op_count_test")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(7))
	})

	t.Run("Delete", func(t *testing.T) {
		history := &models.History{
			MovieID:   "IPX-1000",
			Operation: "delete_test",
			Status:    "success",
		}

		err := repo.Create(history)
		require.NoError(t, err)

		// Delete
		err = repo.Delete(history.ID)
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByID(history.ID)
		assert.Error(t, err)
	})

	t.Run("DeleteByMovieID", func(t *testing.T) {
		// Create multiple history records for same movie
		for i := 0; i < 3; i++ {
			history := &models.History{
				MovieID:   "IPX-1100",
				Operation: "bulk_delete_test",
				Status:    "success",
			}
			err := repo.Create(history)
			require.NoError(t, err)
		}

		// Delete all history for this movie
		err := repo.DeleteByMovieID("IPX-1100")
		require.NoError(t, err)

		// Verify deletion
		results, err := repo.FindByMovieID("IPX-1100")
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("DeleteOlderThan", func(t *testing.T) {
		// Create an old record (simulate by creating and then deleting recent ones)
		oldHistory := &models.History{
			MovieID:   "IPX-1200",
			Operation: "old_record",
			Status:    "success",
		}
		err := repo.Create(oldHistory)
		require.NoError(t, err)

		// Delete records older than 1 hour from now (should delete the old one)
		futureTime := time.Now().Add(1 * time.Hour)
		err = repo.DeleteOlderThan(futureTime)
		require.NoError(t, err)

		// Verify old record is deleted
		_, err = repo.FindByID(oldHistory.ID)
		assert.Error(t, err)
	})

	t.Run("List with pagination", func(t *testing.T) {
		// Create multiple records
		for i := 0; i < 15; i++ {
			history := &models.History{
				MovieID:   "IPX-1300",
				Operation: "list_test",
				Status:    "success",
			}
			err := repo.Create(history)
			require.NoError(t, err)
		}

		// Get first page
		page1, err := repo.List(5, 0)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(page1), 5)

		// Get second page
		page2, err := repo.List(5, 5)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(page2), 5)

		// Verify different records (by ID)
		if len(page1) > 0 && len(page2) > 0 {
			assert.NotEqual(t, page1[0].ID, page2[0].ID)
		}
	})
}
