package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/models"
)

// mergeLiveReviewEdits: mid-apply edits win review-editable fields; untouched
// fields keep the frozen (phase-side) value — codex P6-B acceptance.
func TestMergeLiveReviewEdits(t *testing.T) {
	frozen := &models.Movie{
		ID:    "A-1",
		Title: "phase title",
		Maker: "phase maker",
		Poster: models.PosterState{
			PosterURL:        "phase.jpg",
			CroppedPosterURL: "phase-crop.jpg",
		},
	}
	t.Run("no drift keeps frozen", func(t *testing.T) {
		out := mergeLiveReviewEdits(frozen, frozen, frozen.Clone())
		assert.Equal(t, "phase title", out.Title)
		assert.Equal(t, "phase-crop.jpg", out.Poster.CroppedPosterURL)
	})
	t.Run("edited title wins over frozen", func(t *testing.T) {
		live := frozen.Clone()
		live.Title = "user edit"
		out := mergeLiveReviewEdits(frozen, frozen, live)
		assert.Equal(t, "user edit", out.Title)
		assert.Equal(t, "phase maker", out.Maker)
	})
	t.Run("poster block drift wins wholesale", func(t *testing.T) {
		live := frozen.Clone()
		live.Poster.CroppedPosterURL = "user-crop.jpg"
		out := mergeLiveReviewEdits(frozen, frozen, live)
		assert.Equal(t, "user-crop.jpg", out.Poster.CroppedPosterURL)
		assert.Equal(t, "phase.jpg", out.Poster.PosterURL)
	})
	t.Run("nil live passes frozen", func(t *testing.T) {
		out := mergeLiveReviewEdits(frozen, frozen, nil)
		assert.Equal(t, "phase title", out.Title)
	})
}
