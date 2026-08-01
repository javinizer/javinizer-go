package contracts

import (
	"encoding/json"
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
			PosterURL: "https://example.com/cover.jpg",
			CropBounds: &models.CropBounds{
				X: 0, Y: 0, Width: 400, Height: 600, MaxPosterHeight: 300,
				ImageWidth: 1000, ImageHeight: 600, SourceWasCover: true,
			},
			ShouldCropPoster: false,
		},
	}

	view := MovieViewFromModel(m)
	require.NotNil(t, view.PosterCropBounds, "MovieView must expose the manual crop bounds so review-page PATCHes preserve them")
	assert.Equal(t, CropBounds{X: 0, Y: 0, Width: 400, Height: 600, MaxPosterHeight: 300, ImageWidth: 1000, ImageHeight: 600, SourceWasCover: true}, *view.PosterCropBounds)

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

func TestUpdateMovieRequest_UnmarshalPosterCropBoundsPresence(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantPresent bool
		wantErr     bool
	}{
		{name: "field absent reports absent", body: `{"movie":{"id":"X-1","poster_url":"https://example.com/p.jpg"}}`, wantPresent: false},
		{name: "explicit null reports present", body: `{"movie":{"id":"X-1","poster_crop_bounds":null}}`, wantPresent: true},
		{name: "explicit value reports present", body: `{"movie":{"id":"X-1","poster_crop_bounds":{"x":1,"y":2,"width":3,"height":4}}}`, wantPresent: true},
		{name: "null movie reports absent", body: `{"movie":null}`, wantPresent: false},
		{name: "missing movie reports absent", body: `{}`, wantPresent: false},
		{name: "undecodable movie errors", body: `{"movie":123}`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req UpdateMovieRequest
			err := json.Unmarshal([]byte(tc.body), &req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPresent, req.PosterCropBoundsPresent)
		})
	}
}
