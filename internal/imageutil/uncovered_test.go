package imageutil

import (
	"image"
	"image/color"
	"image/jpeg"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCropPosterFromCover_PortraitResizeUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/test"
	require.NoError(t, fs.MkdirAll(tempDir, 0755))

	coverPath := filepath.Join(tempDir, "portrait_cover.jpg")
	posterPath := filepath.Join(tempDir, "portrait_poster.jpg")

	// Create a tall portrait image that will need resizing (height > MaxPosterHeight)
	createTestImage(t, fs, coverPath, 400, 800, color.RGBA{R: 100, G: 150, B: 200, A: 255})

	err := CropPosterFromCover(fs, coverPath, posterPath, 500)
	require.NoError(t, err)

	posterWidth, posterHeight := decodeTestImageDimensions(t, fs, posterPath)
	assert.Greater(t, posterWidth, 0)
	assert.Greater(t, posterHeight, 0)
	assert.LessOrEqual(t, posterHeight, 500, "tall portrait should be resized")
}

func TestCropPosterWithBounds_TopLeftCropUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/test"
	require.NoError(t, fs.MkdirAll(tempDir, 0755))

	coverPath := filepath.Join(tempDir, "cover.jpg")
	posterPath := filepath.Join(tempDir, "poster.jpg")

	// Build a 200x100 image with two distinct halves
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255}) // left half = red
		}
		for x := 100; x < 200; x++ {
			img.Set(x, y, color.RGBA{G: 255, A: 255}) // right half = green
		}
	}

	f, err := fs.Create(coverPath)
	require.NoError(t, err)
	require.NoError(t, jpeg.Encode(f, img, &jpeg.Options{Quality: 95}))
	require.NoError(t, f.Close())

	// Crop the left (red) half
	err = CropPosterWithBounds(fs, coverPath, posterPath, 0, 0, 100, 100, 500)
	require.NoError(t, err)

	outFile, err := fs.Open(posterPath)
	require.NoError(t, err)
	defer func() { _ = outFile.Close() }()

	outImg, _, err := image.Decode(outFile)
	require.NoError(t, err)
	bounds := outImg.Bounds()
	assert.Equal(t, 100, bounds.Dx())
	assert.Equal(t, 100, bounds.Dy())
}

func TestCropPosterFromCover_NarrowImageUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/test"
	require.NoError(t, fs.MkdirAll(tempDir, 0755))

	coverPath := filepath.Join(tempDir, "narrow.jpg")
	posterPath := filepath.Join(tempDir, "narrow_poster.jpg")

	// Create a narrow tall image (wider than 2:3 aspect)
	createTestImage(t, fs, coverPath, 300, 700, color.RGBA{R: 200, G: 100, B: 50, A: 255})

	err := CropPosterFromCover(fs, coverPath, posterPath, 500)
	require.NoError(t, err)

	_, posterHeight := decodeTestImageDimensions(t, fs, posterPath)
	assert.LessOrEqual(t, posterHeight, 500)
}

func TestCropPosterFromCover_SmallLandscapeUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/test"
	require.NoError(t, fs.MkdirAll(tempDir, 0755))

	coverPath := filepath.Join(tempDir, "small_landscape.jpg")
	posterPath := filepath.Join(tempDir, "small_landscape_poster.jpg")

	// Small landscape that shouldn't need resize
	createTestImage(t, fs, coverPath, 200, 100, color.RGBA{R: 128, G: 128, B: 128, A: 255})

	err := CropPosterFromCover(fs, coverPath, posterPath, 500)
	require.NoError(t, err)

	posterWidth, posterHeight := decodeTestImageDimensions(t, fs, posterPath)
	assert.Greater(t, posterWidth, 0)
	assert.Greater(t, posterHeight, 0)
}

func TestCropPosterWithBounds_BottomRightCropUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/test"
	require.NoError(t, fs.MkdirAll(tempDir, 0755))

	coverPath := filepath.Join(tempDir, "cover.jpg")
	posterPath := filepath.Join(tempDir, "poster.jpg")

	createTestImage(t, fs, coverPath, 200, 100, color.RGBA{R: 200, A: 255})

	// Crop bottom-right quadrant
	err := CropPosterWithBounds(fs, coverPath, posterPath, 100, 50, 200, 100, 500)
	require.NoError(t, err)

	outW, outH := decodeTestImageDimensions(t, fs, posterPath)
	assert.Equal(t, 100, outW)
	assert.Equal(t, 50, outH)
}
