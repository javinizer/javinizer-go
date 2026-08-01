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
