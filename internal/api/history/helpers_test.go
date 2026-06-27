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
