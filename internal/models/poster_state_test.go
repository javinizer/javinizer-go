package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The manual review-page crop geometry must round-trip through Movie's custom
// MarshalJSON/UnmarshalJSON — it is the only codec carrying poster fields.
func TestPosterCropGeometryJSONRoundTrip(t *testing.T) {
	t.Parallel()

	movie := &Movie{
		ContentID: "ipx00123",
		Title:     "Crop Geometry",
		Poster: PosterState{
			PosterURL:            "https://example.com/poster.jpg",
			ShouldCropPoster:     true,
			PosterCropBounds:     &CropBounds{X: 0.0, Y: 0.1, Width: 0.4, Height: 0.8},
			PosterCropSourceFull: true,
		},
	}

	data, err := json.Marshal(movie)
	require.NoError(t, err)

	var wire map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &wire))
	bounds, ok := wire["poster_crop_bounds"].(map[string]interface{})
	require.True(t, ok, "poster_crop_bounds missing from marshaled movie")
	assert.Equal(t, 0.4, bounds["width"])
	assert.Equal(t, 0.1, bounds["y"])
	assert.Equal(t, true, wire["poster_crop_source_full"])

	var decoded Movie
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.Poster.PosterCropBounds)
	assert.Equal(t, *movie.Poster.PosterCropBounds, *decoded.Poster.PosterCropBounds)
	assert.True(t, decoded.Poster.PosterCropSourceFull)
	assert.Equal(t, "https://example.com/poster.jpg", decoded.Poster.PosterURL)
	assert.True(t, decoded.Poster.ShouldCropPoster)
}

// Without pending geometry both fields must be omitted, keeping the wire
// format byte-compatible with pre-change payloads (and golden files).
func TestPosterCropGeometryJSONOmittedWhenUnset(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(&Movie{ContentID: "ipx00123"})
	require.NoError(t, err)

	var wire map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &wire))
	_, hasBounds := wire["poster_crop_bounds"]
	_, hasSourceFull := wire["poster_crop_source_full"]
	assert.False(t, hasBounds, "poster_crop_bounds must be omitted when nil")
	assert.False(t, hasSourceFull, "poster_crop_source_full must be omitted when false")
}

func TestCropBoundsValid(t *testing.T) {
	t.Parallel()
	valid := CropBounds{X: 0.1, Y: 0.05, Width: 0.4, Height: 0.9, SourceAspect: 1.667}
	assert.True(t, valid.Valid())
	assert.True(t, CropBounds{X: 0, Y: 0, Width: 1, Height: 1}.Valid(), "full-frame exact-edge crop is valid")
	assert.True(t, (&CropBounds{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5, SourceAspect: 0}).Valid(), "unset aspect allowed (guard skipped)")
	assert.False(t, (&CropBounds{Width: 0, Height: 0.5}).Valid(), "zero size")
	assert.False(t, (&CropBounds{X: 0.9, Width: 0.5, Height: 1}).Valid(), "containment violated")
	assert.False(t, (&CropBounds{Width: 0.5, Height: 0.5, SourceAspect: -1}).Valid(), "negative aspect must not silently disable the guard")
}

// Clone must deep-copy PosterCropBounds so mutating a clone cannot corrupt
// the stored job result.
func TestPosterStateCloneDeepCopiesCropBounds(t *testing.T) {
	t.Parallel()

	orig := PosterState{
		PosterCropBounds:     &CropBounds{X: 0.1, Y: 0.2, Width: 0.3, Height: 0.4},
		PosterCropSourceFull: true,
	}
	clone := orig.Clone()

	require.NotNil(t, clone.PosterCropBounds)
	assert.Equal(t, *orig.PosterCropBounds, *clone.PosterCropBounds)
	assert.NotSame(t, orig.PosterCropBounds, clone.PosterCropBounds)
	assert.True(t, clone.PosterCropSourceFull)

	clone.PosterCropBounds.Width = 0.9
	assert.Equal(t, 0.3, orig.PosterCropBounds.Width, "clone mutation leaked into original")
}
