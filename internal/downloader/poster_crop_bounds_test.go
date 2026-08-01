package downloader

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// failOverwriteRenameFs reproduces Windows rename semantics: os.Rename refuses
// to replace an existing destination.
type failOverwriteRenameFs struct {
	afero.Fs
}

func (f *failOverwriteRenameFs) Rename(oldPath, newPath string) error {
	if _, err := f.Fs.Stat(newPath); err == nil {
		return fmt.Errorf("rename %s %s: destination exists (windows semantics)", oldPath, newPath)
	}
	return f.Fs.Rename(oldPath, newPath)
}

func TestDownloadPoster_StaleBoundsKeepWholeReplacesExistingPoster(t *testing.T) {
	srv := twoToneCoverServer(t)

	fs := &failOverwriteRenameFs{Fs: afero.NewMemMapFs()}
	tmpDir := "/out"
	require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

	// An organize/update run whose resolved poster already exists must still
	// get the keep-whole replacement — not a rename failure.
	existing := filepath.Join(tmpDir, "IPX-535-poster.jpg")
	require.NoError(t, afero.WriteFile(fs, existing, []byte("old poster"), 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 9000, Y: 0, Width: 400, Height: 600}

	d := NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err, "keep-whole fallback must replace an existing destination portably")
	require.True(t, result.Downloaded)

	got, readErr := afero.ReadFile(fs, existing)
	require.NoError(t, readErr)
	assert.NotEqual(t, "old poster", string(got), "old poster content must be replaced")
}

func TestDownloadPoster_ManualCropConcurrentSameDestination(t *testing.T) {
	// Multipart movies with a part-less poster template resolve one shared
	// destPath; every part enters the crop branch concurrently. Both the
	// shared <dest>.full.tmp staging file and the final write must be
	// serialized — otherwise one worker deletes/renames the temp another is
	// cropping.
	for iter := 0; iter < 5; iter++ {
		srv := twoToneCoverServer(t)
		fs := afero.NewMemMapFs()
		tmpDir := "/out"
		require.NoError(t, fs.MkdirAll(tmpDir, 0o755))

		movie := func() *models.Movie {
			m := createTestMovie()
			m.Poster.PosterURL = srv.URL + "/cover.jpg"
			m.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600}
			return m
		}

		d := NewDownloader(http.DefaultClient, fs, &Config{
			DownloadPoster:    true,
			MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
		}, nil)

		const workers = 8
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := d.downloadPoster(context.Background(), movie(), tmpDir, nil)
				if err != nil {
					errs <- err
					return
				}
				if !res.Downloaded {
					errs <- fmt.Errorf("expected Downloaded=true")
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("concurrent manual crop failed: %v", err)
		}

		img := decodePosterImageFs(t, fs, filepath.Join(tmpDir, "IPX-535-poster.jpg"))
		b := img.Bounds()
		require.Equal(t, 400, b.Dx())
		require.Equal(t, 600, b.Dy())
		srv.Close()
	}
}

func decodePosterImageFs(t *testing.T, fs afero.Fs, path string) image.Image {
	t.Helper()
	f, err := fs.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	require.NoError(t, err)
	return img
}

func TestDownloadPoster_BoundsCarryMaxPosterHeight(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	// The crop preview honored an explicit 300px max height; Organize must
	// produce the same output height instead of the config default (0 = uncapped).
	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false
	movie.Poster.CropBounds = &models.CropBounds{X: 0, Y: 0, Width: 400, Height: 600, MaxPosterHeight: 300}

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img := decodePosterImage(t, result.LocalPath)
	b := img.Bounds()
	assert.Equal(t, 300, b.Dy(), "the stored max poster height must be honored at apply")
	assert.Equal(t, 200, b.Dx(), "aspect 400:600 scales to 200:300")
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

func TestProbeDecodableImage_OpenError(t *testing.T) {
	// A missing file must surface the open error (the caller then fails the
	// poster step instead of renaming a phantom file into place).
	err := probeDecodableImage(afero.NewMemMapFs(), "/does/not/exist.jpg")
	require.Error(t, err)
}

func TestDownloadPoster_ManualCropOverwritesExistingPoster(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	// A poster already on disk from a previous organize/update must be replaced
	// by the user's explicit crop, not silently kept.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "IPX-535-poster.jpg"), []byte("old poster"), 0o644))

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
	assert.Equal(t, 400, b.Dx(), "existing poster must be replaced by the manual crop")
	assert.Equal(t, 600, b.Dy())
	r, _, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	assert.Greater(t, r, bl)
}

func TestDownloadPoster_ExistingPosterWithoutBoundsStillSkipped(t *testing.T) {
	srv := twoToneCoverServer(t)
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "IPX-535-poster.jpg"), []byte("old poster"), 0o644))

	movie := createTestMovie()
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = false

	d := newPosterTestDownloader(&Config{DownloadPoster: true})
	result, err := d.downloadPoster(context.Background(), movie, tmpDir, nil)
	require.NoError(t, err)
	require.False(t, result.Downloaded, "existing poster without manual bounds keeps the skip-existing behavior")

	content, err := os.ReadFile(result.LocalPath)
	require.NoError(t, err)
	assert.Equal(t, "old poster", string(content))
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
