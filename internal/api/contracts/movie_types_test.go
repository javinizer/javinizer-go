package contracts

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The presence flag distinguishes an omitted poster_crop_bounds key
// (preserve stored geometry) from an explicit null (clear it).
func TestUpdateMovieRequestUnmarshal_PosterCropBoundsPresence(t *testing.T) {
	t.Parallel()

	t.Run("key present as null", func(t *testing.T) {
		var r UpdateMovieRequest
		require.NoError(t, json.Unmarshal([]byte(`{"movie":{"code":"ABC-1","poster_crop_bounds":null}}`), &r))
		require.NotNil(t, r.Movie)
		assert.True(t, r.PosterCropBoundsFieldPresent)
		assert.Nil(t, r.Movie.PosterCropBounds)
	})

	t.Run("key present with value", func(t *testing.T) {
		var r UpdateMovieRequest
		require.NoError(t, json.Unmarshal([]byte(`{"movie":{"poster_crop_bounds":{"x":0.1,"y":0.1,"width":0.4,"height":0.9}}}`), &r))
		assert.True(t, r.PosterCropBoundsFieldPresent)
		require.NotNil(t, r.Movie.PosterCropBounds)
		assert.InDelta(t, 0.4, r.Movie.PosterCropBounds.Width, 1e-9)
	})

	t.Run("key omitted", func(t *testing.T) {
		var r UpdateMovieRequest
		require.NoError(t, json.Unmarshal([]byte(`{"movie":{"code":"ABC-1"}}`), &r))
		require.NotNil(t, r.Movie)
		assert.False(t, r.PosterCropBoundsFieldPresent)
	})

	t.Run("movie null leaves Movie nil for binding validation", func(t *testing.T) {
		var r UpdateMovieRequest
		require.NoError(t, json.Unmarshal([]byte(`{"movie":null}`), &r))
		assert.Nil(t, r.Movie)
		assert.False(t, r.PosterCropBoundsFieldPresent)
	})

	t.Run("typed envelope mismatch errors", func(t *testing.T) {
		// Well-formed JSON of the wrong shape reaches our envelope decode;
		// truncated input like `{` errors inside encoding/json instead.
		var r UpdateMovieRequest
		assert.Error(t, json.Unmarshal([]byte(`[1]`), &r))
	})

	t.Run("truncated input errors", func(t *testing.T) {
		var r UpdateMovieRequest
		assert.Error(t, json.Unmarshal([]byte(`{`), &r))
	})

	t.Run("non-object movie errors", func(t *testing.T) {
		var r UpdateMovieRequest
		assert.Error(t, json.Unmarshal([]byte(`{"movie":5}`), &r))
	})
}
