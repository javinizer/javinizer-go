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

// codex cloud P2: every PATCH-editable field participates in the apply
// closeout merge — a concurrent review edit on any of these overrides the
// phase's frozen value, never the reverse.
func TestMergeLiveReviewEditsNewFields(t *testing.T) {
	frozen := &models.Movie{ID: "A-1", Title: "phase"}
	live := frozen.Clone()
	live.OriginalFileName = "renamed.mkv"
	live.RatingWarning = "R18"
	live.SourceName = "dmm"
	live.SourceURL = "https://dmm/x"
	live.Translations = []models.MovieTranslation{{Language: "en", Title: "English name"}}
	out := mergeLiveReviewEdits(frozen, frozen, live)
	assert.Equal(t, "renamed.mkv", out.OriginalFileName)
	assert.Equal(t, "R18", out.RatingWarning)
	assert.Equal(t, "dmm", out.SourceName)
	assert.Equal(t, "https://dmm/x", out.SourceURL)
	assert.Len(t, out.Translations, 1)
	assert.Equal(t, "English name", out.Translations[0].Title)

	// no-drift control: untouched fields keep the phase value.
	frozen2 := &models.Movie{ID: "A-2", SourceName: "freeze"}
	live2 := frozen2.Clone()
	live2.Title = "user edit"
	out2 := mergeLiveReviewEdits(frozen2, frozen2, live2)
	assert.Equal(t, "user edit", out2.Title)
	assert.Equal(t, "freeze", out2.SourceName, "unchanged field follows the phase")
}
