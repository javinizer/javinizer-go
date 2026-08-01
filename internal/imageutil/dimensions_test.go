package imageutil

import (
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageDimensionsFromFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	f, err := fs.Create("/in.jpg")
	require.NoError(t, err)
	require.NoError(t, jpeg.Encode(f, img, nil))
	require.NoError(t, f.Close())

	w, h, err := ImageDimensionsFromFile(fs, "/in.jpg")
	require.NoError(t, err)
	assert.Equal(t, 1000, w)
	assert.Equal(t, 600, h)

	_, _, err = ImageDimensionsFromFile(fs, "/missing.jpg")
	require.Error(t, err)

	require.NoError(t, afero.WriteFile(fs, "/garbage.jpg", []byte("not an image"), 0o644))
	_, _, err = ImageDimensionsFromFile(fs, "/garbage.jpg")
	require.Error(t, err, "an undecodable file must surface as an error")
}
