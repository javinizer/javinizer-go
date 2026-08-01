package downloader

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoToneCoverServer(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			if x < 500 {
				img.Set(x, y, color.RGBA{R: 220, G: 30, B: 30, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 30, G: 30, B: 220, A: 255})
			}
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 95})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func decodePosterImage(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	require.NoError(t, err)
	return img
}

func newPosterTestDownloader(cfg *Config) *Downloader {
	if cfg.MediaFormatConfig.PosterFormat == "" {
		cfg.MediaFormatConfig = organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"}
	}
	return NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, nil)
}

func TestDownloadPoster_AppliesManualCropBounds(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 400, b.Dx(), "poster must be cropped to the user's manual bounds")
	assert.Equal(t, 600, b.Dy())
	r, _, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	assert.Greater(t, r, bl, "poster pixels must come from the manual (left/red) crop region")
}

func TestDownloadPoster_UndecodableDownloadDoesNotShipAsPoster(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>scraper error page</html>"))
	}))
	t.Cleanup(srv.Close)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/oops"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "an undecodable download must fail the poster step, not be renamed into place")
	require.False(t, result.Downloaded)
	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.False(t, strings.HasSuffix(e.Name(), "-poster.jpg"), "no poster file should be written: %s", e.Name())
	}
}

func TestDownloadPoster_TruncatedDownloadDoesNotShipAsPoster(t *testing.T) {
	coverBytes := func() []byte {
		img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 1000; x++ {
				img.Set(x, y, color.RGBA{R: 220, G: 30, B: 30, A: 255})
			}
		}
		var buf bytes.Buffer
		require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}))
		return buf.Bytes()
	}()
	truncated := coverBytes[:len(coverBytes)/2]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(truncated)
	}))
	t.Cleanup(srv.Close)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.Error(t, err, "a header-valid but body-truncated image must not be renamed into place")
	require.False(t, result.Downloaded)
}

func TestDownloadPoster_StaleCropBoundsOnPosterGradeSourceSaveWhole(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false // poster-grade scraper source, then user-cropped
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 1000, b.Dx(), "stale bounds on a poster-grade source must not butcher the image with an auto-crop")
	assert.Equal(t, 600, b.Dy())
}

func TestDownloadPoster_StaleCropBoundsFallBackToDefaultCrop(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	scraperSaidCover := true
	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false // set by the manual crop itself
	movie.Poster.OriginalShouldCropPoster = &scraperSaidCover
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err, "stale bounds must not fail the poster download")
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Less(t, b.Dx(), b.Dy(), "cover-shaped source with stale bounds degrades to the default portrait crop")
	r, _, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	assert.Greater(t, bl, r, "fallback crop is the default right-side auto-crop")
}
