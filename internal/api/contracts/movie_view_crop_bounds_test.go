package contracts

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovieView_CropBoundsRoundTrip(t *testing.T) {
	m := &models.Movie{
		ContentID: "TEST-001",
		ID:        "TEST-001",
		Poster: models.PosterState{
			PosterURL:        "https://example.com/cover.jpg",
			CropBounds:       &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600, MaxPosterHeight: 300},
			ShouldCropPoster: false,
		},
	}

	view := MovieViewFromModel(m)
	require.NotNil(t, view.PosterCropBounds, "MovieView must expose the manual crop bounds so review-page PATCHes preserve them")
	assert.Equal(t, CropBounds{X: 0, Y: 0, Width: 400, Height: 600, MaxPosterHeight: 300}, *view.PosterCropBounds)

	assert.NotSame(t, m.Poster.CropBounds, view.PosterCropBounds, "view must not alias model state")

	back := MovieViewToModel(view)
	require.NotNil(t, back.Poster.CropBounds)
	assert.Equal(t, *m.Poster.CropBounds, *back.Poster.CropBounds)
	assert.Equal(t, 300, back.Poster.CropBounds.MaxPosterHeight)
	assert.NotSame(t, view.PosterCropBounds, back.Poster.CropBounds, "model must not alias view state")

	nilView := MovieViewFromModel(&models.Movie{ContentID: "TEST-002"})
	assert.Nil(t, nilView.PosterCropBounds)
	assert.Nil(t, MovieViewToModel(nilView).Poster.CropBounds)
}
