package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryRepository(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := NewHistoryRepository(db)

	t.Run("Create history record", func(t *testing.T) {
		history := &models.History{
			MovieID:      "IPX-001",
			Operation:    models.HistoryOpScrape,
			Status:       models.HistoryStatusSuccess,
			OriginalPath: "/path/to/original.mp4",
			NewPath:      "/path/to/new.mp4",
		}

		err := repo.Create(context.TODO(), history)
		require.NoError(t, err)
		assert.NotZero(t, history.ID)
	})

	t.Run("FindByID", func(t *testing.T) {
		history := &models.History{
			MovieID:   "IPX-002",
			Operation: models.HistoryOpOrganize,
			Status:    models.HistoryStatusSuccess,
		}

		err := repo.Create(context.TODO(), history)
		require.NoError(t, err)

		found, err := repo.FindByID(context.TODO(), history.ID)
		require.NoError(t, err)
		assert.Equal(t, "IPX-002", found.MovieID)
		assert.Equal(t, models.HistoryOpOrganize, found.Operation)
	})

	t.Run("FindByID not found", func(t *testing.T) {
		_, err := repo.FindByID(context.TODO(), 99999)
		assert.Error(t, err)
	})

	t.Run("FindByMovieID", func(t *testing.T) {
		// Create multiple history records for same movie
		histories := []*models.History{
			{MovieID: "IPX-100", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess},
			{MovieID: "IPX-100", Operation: models.HistoryOpDownload, Status: models.HistoryStatusSuccess},
			{MovieID: "IPX-100", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess},
		}

		for _, h := range histories {
			err := repo.Create(context.TODO(), h)
			require.NoError(t, err)
		}

		// Find all history for this movie
		results, err := repo.FindByMovieID(context.TODO(), "IPX-100")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 3)

		// Verify operations
		operations := make(map[string]bool)
		for _, r := range results {
			if r.MovieID == "IPX-100" {
				operations[string(r.Operation)] = true
			}
		}
		assert.True(t, operations[string(models.HistoryOpScrape)])
		assert.True(t, operations[string(models.HistoryOpDownload)])
		assert.True(t, operations[string(models.HistoryOpOrganize)])
	})

	t.Run("FindByOperation", func(t *testing.T) {
		// Create history records with different operations
		histories := []*models.History{
			{MovieID: "IPX-200", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess},
			{MovieID: "IPX-201", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess},
			{MovieID: "IPX-202", Operation: models.HistoryOpDownload, Status: models.HistoryStatusSuccess},
		}

		for _, h := range histories {
			err := repo.Create(context.TODO(), h)
			require.NoError(t, err)
		}

		// Find all scrape operations
		results, err := repo.FindByOperation(context.TODO(), models.HistoryOpScrape, 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 2)

		// Verify all are scrape operations
		for _, r := range results {
			if r.MovieID == "IPX-200" || r.MovieID == "IPX-201" {
				assert.Equal(t, models.HistoryOpScrape, r.Operation)
			}
		}
	})

	t.Run("FindByOperation with limit", func(t *testing.T) {
		// Create many records
		for i := 0; i < 20; i++ {
			history := &models.History{
				MovieID:   "IPX-300",
				Operation: models.HistoryOperation("test_op"),
				Status:    models.HistoryStatusSuccess,
			}
			err := repo.Create(context.TODO(), history)
			require.NoError(t, err)
		}

		// Find with limit
		results, err := repo.FindByOperation(context.TODO(), models.HistoryOperation("test_op"), 5)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(results), 5)
	})

	t.Run("FindByStatus", func(t *testing.T) {
		// Create records with different statuses
		histories := []*models.History{
			{MovieID: "IPX-400", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess},
			{MovieID: "IPX-401", Operation: models.HistoryOpScrape, Status: models.HistoryStatusFailed},
			{MovieID: "IPX-402", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess},
		}

		for _, h := range histories {
			err := repo.Create(context.TODO(), h)
			require.NoError(t, err)
		}

		// Find all successful operations
		results, err := repo.FindByStatus(context.TODO(), models.HistoryStatusSuccess, 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 2)

		// Verify all have success status
		for _, r := range results {
			if r.MovieID == "IPX-400" || r.MovieID == "IPX-402" {
				assert.Equal(t, models.HistoryStatusSuccess, r.Status)
			}
		}
	})

	t.Run("FindRecent", func(t *testing.T) {
		// Create multiple records
		for i := 0; i < 10; i++ {
			history := &models.History{
				MovieID:   "IPX-500",
				Operation: models.HistoryOperation("recent_test"),
				Status:    models.HistoryStatusSuccess,
			}
			err := repo.Create(context.TODO(), history)
			require.NoError(t, err)
			time.Sleep(1 * time.Millisecond) // Ensure different timestamps
		}

		// Get recent records
		results, err := repo.FindRecent(context.TODO(), 5)
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
			Operation: models.HistoryOperation("date_test"),
			Status:    models.HistoryStatusSuccess,
		}
		err := repo.Create(context.TODO(), history)
		require.NoError(t, err)

		// Use actual creation time for date range
		now := history.CreatedAt
		start := now.Add(-1 * time.Second)
		end := now.Add(1 * time.Second)

		// Find by date range
		results, err := repo.FindByDateRange(context.TODO(), start, end)
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
		initialCount, err := repo.Count(context.TODO())
		require.NoError(t, err)

		// Create new records
		for i := 0; i < 3; i++ {
			history := &models.History{
				MovieID:   "IPX-700",
				Operation: models.HistoryOperation("count_test"),
				Status:    models.HistoryStatusSuccess,
			}
			err := repo.Create(context.TODO(), history)
			require.NoError(t, err)
		}

		// Get new count
		newCount, err := repo.Count(context.TODO())
		require.NoError(t, err)
		assert.Equal(t, initialCount+3, newCount)
	})

	t.Run("CountByStatus", func(t *testing.T) {
		// Create records with specific status
		for i := 0; i < 5; i++ {
			history := &models.History{
				MovieID:   "IPX-800",
				Operation: models.HistoryOperation("status_count_test"),
				Status:    models.HistoryStatus("pending"),
			}
			err := repo.Create(context.TODO(), history)
			require.NoError(t, err)
		}

		count, err := repo.CountByStatus(context.TODO(), models.HistoryStatus("pending"))
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(5))
	})

	t.Run("CountByOperation", func(t *testing.T) {
		// Create records with specific operation
		for i := 0; i < 7; i++ {
			history := &models.History{
				MovieID:   "IPX-900",
				Operation: models.HistoryOperation("op_count_test"),
				Status:    models.HistoryStatusSuccess,
			}
			err := repo.Create(context.TODO(), history)
			require.NoError(t, err)
		}

		count, err := repo.CountByOperation(context.TODO(), models.HistoryOperation("op_count_test"))
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(7))
	})

	t.Run("Delete", func(t *testing.T) {
		history := &models.History{
			MovieID:   "IPX-1000",
			Operation: models.HistoryOperation("delete_test"),
			Status:    models.HistoryStatusSuccess,
		}

		err := repo.Create(context.TODO(), history)
		require.NoError(t, err)

		// Delete
		err = repo.Delete(context.TODO(), history.ID)
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByID(context.TODO(), history.ID)
		assert.Error(t, err)
	})

	t.Run("DeleteByMovieID", func(t *testing.T) {
		// Create multiple history records for same movie
		for i := 0; i < 3; i++ {
			history := &models.History{
				MovieID:   "IPX-1100",
				Operation: models.HistoryOperation("bulk_delete_test"),
				Status:    models.HistoryStatusSuccess,
			}
			err := repo.Create(context.TODO(), history)
			require.NoError(t, err)
		}

		// Delete all history for this movie
		err := repo.DeleteByMovieID(context.TODO(), "IPX-1100")
		require.NoError(t, err)

		// Verify deletion
		results, err := repo.FindByMovieID(context.TODO(), "IPX-1100")
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("DeleteOlderThan", func(t *testing.T) {
		// Create an old record (simulate by creating and then deleting recent ones)
		oldHistory := &models.History{
			MovieID:   "IPX-1200",
			Operation: models.HistoryOperation("old_record"),
			Status:    models.HistoryStatusSuccess,
		}
		err := repo.Create(context.TODO(), oldHistory)
		require.NoError(t, err)

		// Delete records older than 1 hour from now (should delete the old one)
		futureTime := time.Now().Add(1 * time.Hour)
		err = repo.DeleteOlderThan(context.TODO(), futureTime)
		require.NoError(t, err)

		// Verify old record is deleted
		_, err = repo.FindByID(context.TODO(), oldHistory.ID)
		assert.Error(t, err)
	})

	t.Run("List with pagination", func(t *testing.T) {
		// Create multiple records
		for i := 0; i < 15; i++ {
			history := &models.History{
				MovieID:   "IPX-1300",
				Operation: models.HistoryOperation("list_test"),
				Status:    models.HistoryStatusSuccess,
			}
			err := repo.Create(context.TODO(), history)
			require.NoError(t, err)
		}

		// Get first page
		page1, err := repo.List(context.TODO(), 5, 0)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(page1), 5)

		// Get second page
		page2, err := repo.List(context.TODO(), 5, 5)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(page2), 5)

		// Verify different records (by ID)
		if len(page1) > 0 && len(page2) > 0 {
			assert.NotEqual(t, page1[0].ID, page2[0].ID)
		}
	})

	t.Run("Create with all fields populated", func(t *testing.T) {
		history := &models.History{
			MovieID:      "IPX-HIST-001",
			Operation:    models.HistoryOperation("complete_test"),
			Status:       models.HistoryStatusSuccess,
			OriginalPath: "/original/path/file.mp4",
			NewPath:      "/new/path/file.mp4",
			ErrorMessage: "Test error message",
			Metadata:     `{"key":"value"}`,
			DryRun:       false,
		}

		err := repo.Create(context.TODO(), history)
		require.NoError(t, err)
		assert.NotZero(t, history.ID)

		found, err := repo.FindByID(context.TODO(), history.ID)
		require.NoError(t, err)
		assert.Equal(t, "/original/path/file.mp4", found.OriginalPath)
		assert.Equal(t, "/new/path/file.mp4", found.NewPath)
		assert.Equal(t, "Test error message", found.ErrorMessage)
		assert.Equal(t, `{"key":"value"}`, found.Metadata)
		assert.Equal(t, false, found.DryRun)
	})

	t.Run("FindByOperation with empty operation", func(t *testing.T) {
		// Create some records first
		history := &models.History{
			MovieID:   "IPX-EMPTY-OP",
			Operation: models.HistoryOperation("test"),
			Status:    models.HistoryStatusSuccess,
		}
		err := repo.Create(context.TODO(), history)
		require.NoError(t, err)

		// Query with empty string (should return no results)
		results, err := repo.FindByOperation(context.TODO(), models.HistoryOperation(""), 10)
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("FindByStatus with empty status", func(t *testing.T) {
		results, err := repo.FindByStatus(context.TODO(), models.HistoryStatus(""), 10)
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("DeleteAll non-existent movie", func(t *testing.T) {
		err := repo.DeleteByMovieID(context.TODO(), "NONEXISTENT-MOVIE-999")
		assert.NoError(t, err, "Deleting non-existent movie history should not error")
	})

	t.Run("Count with empty database", func(t *testing.T) {
		cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

		db2, err := New(cfg)
		require.NoError(t, err)
		defer func() { _ = db2.Close() }()

		require.NoError(t, db2.RunMigrationsOnStartup(context.Background()))
		repo2 := NewHistoryRepository(db2)

		count, err := repo2.Count(context.TODO())
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	})

	t.Run("FindByDateRange with equal start and end", func(t *testing.T) {
		history := &models.History{
			MovieID:   "IPX-EQUAL-RANGE",
			Operation: models.HistoryOperation("range_test"),
			Status:    models.HistoryStatusSuccess,
		}
		err := repo.Create(context.TODO(), history)
		require.NoError(t, err)

		now := history.CreatedAt
		start := now
		end := now

		results, err := repo.FindByDateRange(context.TODO(), start, end)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 1)
	})

	t.Run("FindByDateRange with end before start", func(t *testing.T) {
		start := time.Now().Add(1 * time.Hour)
		end := time.Now()

		results, err := repo.FindByDateRange(context.TODO(), start, end)
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("List with zero limit", func(t *testing.T) {
		results, err := repo.List(context.TODO(), 0, 0)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 1,
			"List(0,0) should return all records when limit is 0")
	})

	t.Run("List with negative offset", func(t *testing.T) {
		results, err := repo.List(context.TODO(), 10, -5)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 0)
	})

	t.Run("FindByOperation with negative limit", func(t *testing.T) {
		results, err := repo.FindByOperation(context.TODO(), models.HistoryOperation("test_op"), -1)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 0)
	})

	t.Run("FindByStatus with negative limit", func(t *testing.T) {
		results, err := repo.FindByStatus(context.TODO(), models.HistoryStatusSuccess, -1)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 0)
	})

	t.Run("FindRecent with negative limit", func(t *testing.T) {
		results, err := repo.FindRecent(context.TODO(), -1)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 0)
	})
}
