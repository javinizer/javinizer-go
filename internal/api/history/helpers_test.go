package history

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestToHistoryRecord(t *testing.T) {
	ts := time.Date(2026, 4, 27, 12, 30, 0, 0, time.UTC)

	t.Run("converts all fields from models.History", func(t *testing.T) {
		h := models.History{
			ID:           42,
			MovieID:      "ABC-123",
			Operation:    models.HistoryOpScrape,
			OriginalPath: "/src/ABC-123.mp4",
			NewPath:      "/dst/ABC-123.mp4",
			Status:       models.HistoryStatusSuccess,
			ErrorMessage: "",
			Metadata:     `{"source":"jav"}`,
			DryRun:       false,
			CreatedAt:    ts,
		}

		got := toHistoryRecord(h)

		assert.Equal(t, uint(42), got.ID)
		assert.Equal(t, "ABC-123", got.MovieID)
		assert.Equal(t, models.HistoryOpScrape, got.Operation)
		assert.Equal(t, "/src/ABC-123.mp4", got.OriginalPath)
		assert.Equal(t, "/dst/ABC-123.mp4", got.NewPath)
		assert.Equal(t, models.HistoryStatusSuccess, got.Status)
		assert.Equal(t, "", got.ErrorMessage)
		assert.Equal(t, `{"source":"jav"}`, got.Metadata)
		assert.False(t, got.DryRun)
		assert.Equal(t, ts.Format(time.RFC3339), got.CreatedAt)
	})

	t.Run("formats CreatedAt as RFC3339", func(t *testing.T) {
		h := models.History{CreatedAt: ts}
		got := toHistoryRecord(h)
		assert.Equal(t, "2026-04-27T12:30:00Z", got.CreatedAt)
	})

	t.Run("preserves error message when present", func(t *testing.T) {
		h := models.History{ErrorMessage: "network timeout"}
		got := toHistoryRecord(h)
		assert.Equal(t, "network timeout", got.ErrorMessage)
	})

	t.Run("preserves dry_run flag when true", func(t *testing.T) {
		h := models.History{DryRun: true}
		got := toHistoryRecord(h)
		assert.True(t, got.DryRun)
	})
}

func TestPaginateAndConvert(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	makeHistory := func(id uint, movieID string) models.History {
		return models.History{
			ID:        id,
			MovieID:   movieID,
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
			CreatedAt: ts,
		}
	}

	all := []models.History{
		makeHistory(1, "A-001"),
		makeHistory(2, "A-002"),
		makeHistory(3, "A-003"),
		makeHistory(4, "A-004"),
		makeHistory(5, "A-005"),
	}

	t.Run("returns total count of all records", func(t *testing.T) {
		records, total := paginateAndConvert(all, 10, 0)
		assert.Equal(t, int64(5), total)
		assert.Len(t, records, 5)
	})

	t.Run("paginates with limit", func(t *testing.T) {
		records, total := paginateAndConvert(all, 2, 0)
		assert.Equal(t, int64(5), total)
		assert.Len(t, records, 2)
		assert.Equal(t, "A-001", records[0].MovieID)
		assert.Equal(t, "A-002", records[1].MovieID)
	})

	t.Run("paginates with offset", func(t *testing.T) {
		records, total := paginateAndConvert(all, 2, 2)
		assert.Equal(t, int64(5), total)
		assert.Len(t, records, 2)
		assert.Equal(t, "A-003", records[0].MovieID)
		assert.Equal(t, "A-004", records[1].MovieID)
	})

	t.Run("handles offset beyond total", func(t *testing.T) {
		records, total := paginateAndConvert(all, 10, 100)
		assert.Equal(t, int64(5), total)
		assert.Empty(t, records)
	})

	t.Run("handles limit exceeding remaining items", func(t *testing.T) {
		records, total := paginateAndConvert(all, 100, 3)
		assert.Equal(t, int64(5), total)
		assert.Len(t, records, 2)
		assert.Equal(t, "A-004", records[0].MovieID)
		assert.Equal(t, "A-005", records[1].MovieID)
	})

	t.Run("offset at boundary returns empty slice", func(t *testing.T) {
		records, total := paginateAndConvert(all, 10, 5)
		assert.Equal(t, int64(5), total)
		assert.Empty(t, records)
	})

	t.Run("empty input returns empty records and zero total", func(t *testing.T) {
		records, total := paginateAndConvert([]models.History{}, 10, 0)
		assert.Equal(t, int64(0), total)
		assert.Empty(t, records)
	})

	t.Run("converts each record via toHistoryRecord", func(t *testing.T) {
		records, _ := paginateAndConvert(all, 1, 0)
		assert.Len(t, records, 1)
		assert.Equal(t, uint(1), records[0].ID)
		assert.Equal(t, "A-001", records[0].MovieID)
		assert.Equal(t, ts.Format(time.RFC3339), records[0].CreatedAt)
	})

	t.Run("single record with full limit returns it", func(t *testing.T) {
		single := []models.History{makeHistory(1, "A-001")}
		records, total := paginateAndConvert(single, 50, 0)
		assert.Equal(t, int64(1), total)
		assert.Len(t, records, 1)
	})
}
