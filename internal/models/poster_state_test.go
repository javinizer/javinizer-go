package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPosterState_Clone_CropBounds(t *testing.T) {
	orig := PosterState{
		PosterURL:  "p.jpg",
		CropBounds: &CropBounds{X: 10, Y: 20, Width: 300, Height: 450},
	}
	cloned := orig.Clone()
	require.NotNil(t, cloned.CropBounds)
	assert.Equal(t, *orig.CropBounds, *cloned.CropBounds)

	cloned.CropBounds.X = 999
	assert.Equal(t, 10, orig.CropBounds.X, "Clone must deep-copy CropBounds")

	nilClone := PosterState{}.Clone()
	assert.Nil(t, nilClone.CropBounds)
}

func TestMovie_JSONRoundTrip_CropBounds(t *testing.T) {
	orig := &Movie{
		ContentID: "TEST-001",
		ID:        "TEST-001",
		Poster: PosterState{
			PosterURL:        "https://example.com/cover.jpg",
			ShouldCropPoster: true,
			CropBounds:       &CropBounds{X: 0, Y: 0, Width: 400, Height: 600},
		},
	}

	data, err := json.Marshal(orig)
	require.NoError(t, err)
	require.Contains(t, string(data), "poster_crop_bounds")

	var decoded Movie
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.Poster.CropBounds)
	assert.Equal(t, CropBounds{X: 0, Y: 0, Width: 400, Height: 600}, *decoded.Poster.CropBounds)

	plain, err := json.Marshal(&Movie{ContentID: "TEST-002"})
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(plain), "poster_crop_bounds"),
		"nil CropBounds must be omitted from the wire format")
}

// TestPosterState_SyncCropIntentWithSource pins the crop-intent re-derivation
// source-changing edits rely on: the class of the NEW effective source
// (PosterURL ?? CoverURL) dictates ShouldCropPoster, so a cover-backed movie
// that gains a poster-grade source no longer records SourceWasCover=true on
// later manual crops (which would degrade the apply-time geometry fallback to
// the default cover crop), and a cleared poster falls back to cover-backed
// default-crop semantics.
func TestPosterState_SyncCropIntentWithSource(t *testing.T) {
	t.Run("poster URL set marks the source poster-grade", func(t *testing.T) {
		p := PosterState{PosterURL: "https://x/p.jpg", CoverURL: "https://x/c.jpg", ShouldCropPoster: true}
		p.SyncCropIntentWithSource()
		assert.False(t, p.ShouldCropPoster, "a poster-grade source must never be default-cropped or recorded SourceWasCover")
	})

	t.Run("poster URL cleared with a cover remaining restores cover intent", func(t *testing.T) {
		p := PosterState{CoverURL: "https://x/c.jpg", ShouldCropPoster: false}
		p.SyncCropIntentWithSource()
		assert.True(t, p.ShouldCropPoster, "the cover feeds the poster pipeline: default cover-crop semantics return")
	})

	t.Run("both URLs empty leaves the intent untouched", func(t *testing.T) {
		for _, initial := range []bool{true, false} {
			p := PosterState{ShouldCropPoster: initial}
			p.SyncCropIntentWithSource()
			assert.Equal(t, initial, p.ShouldCropPoster, "nothing will be downloaded from a cleared source; the flag must not churn")
		}
	})
}
