package downloader

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two-tone 1000x600 source: left half black, right half white. The scraper's
// auto-crop (CropPosterFromCover) takes the right ~47%% of a landscape cover,
// so a manual LEFT crop producing black pixels proves the persisted geometry
// beat the scraper default.
func serveTwoToneSource(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			if x < 500 {
				img.Set(x, y, color.Black)
			} else {
				img.Set(x, y, color.White)
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

func newGeometryDownloader(fs afero.Fs) *Downloader {
	return NewDownloader(http.DefaultClient, fs, &Config{
		DownloadPoster:  true,
		MaxPosterHeight: 0,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}, nil)
}

func decodeResultPoster(t *testing.T, fs afero.Fs, path string) (image.Image, int, int) {
	t.Helper()
	f, err := fs.Open(path)
	require.NoError(t, err)
	defer f.Close()
	img, err := jpeg.Decode(f)
	require.NoError(t, err, "poster at %s must decode as jpeg", path)
	return img, img.Bounds().Dx(), img.Bounds().Dy()
}

// sampleLuma reads the luma at a fractional position, far from edges to
// avoid JPEG edge-blend artifacts on the two-tone boundary.
func sampleLuma(img image.Image, fx, fy float64) float64 {
	b := img.Bounds()
	x := b.Min.X + int(fx*float64(b.Dx()))
	y := b.Min.Y + int(fy*float64(b.Dy()))
	r, g, bl, _ := img.At(x, y).RGBA()
	return 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
}

func geometryMovie(url string, bounds *models.CropBounds, autoCrop bool) *models.Movie {
	m := &models.Movie{ID: "IPX-535", ContentID: "ipx00535", Title: "Crop"}
	m.Poster.PosterURL = url
	m.Poster.ShouldCropPoster = autoCrop
	if bounds != nil {
		m.Poster.PosterCropBounds = bounds
		m.Poster.PosterCropSourceFull = true
	}
	return m
}

// The persisted manual crop geometry is applied to the downloaded source
// instead of the scraper's default right-side auto-crop.
// renameFailFs blocks only the final promote rename (dst = the poster
// destination); the downloader's internal atomic ".tmp" staging is let
// through — the promote step is the one under test.
type renameFailFs struct {
	afero.Fs
}

func (f renameFailFs) Rename(src, dst string) error {
	if strings.HasSuffix(dst, "-poster.jpg") {
		return errTestRenameFail
	}
	return f.Fs.Rename(src, dst)
}

var errTestRenameFail = errors.New("rename blocked")

// Download failure with otherwise-applyable geometry: the error propagates
// and nothing is left behind. Exactly one request is made.
func TestDownloadPoster_GeometryDownloadFailurePropagates(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	fs := afero.NewMemMapFs()
	movie := geometryMovie(srv.URL+"/cover.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1.0, SourceAspect: 1000.0 / 600.0,
	}, true)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.Error(t, err)
	require.False(t, result.Downloaded)
	assert.Equal(t, int32(1), hits.Load())
	leftover, _ := afero.ReadDir(fs, "/dest")
	assert.Empty(t, leftover, "no temp or dest file may survive a failed download")
}

// Dimensions decode fine (header intact) but pixel data is truncated: the
// crop itself fails, and with scraper auto-crop intent the auto-crop attempt
// fails identically — pre-change behavior for a broken source image.
func TestDownloadPoster_GeometryCropFailureFallsToAutoCropError(t *testing.T) {
	srv := serveTwoToneSource(t)
	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	full, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	// Header survives DecodeConfig, pixels are gone.
	trunc := full[:len(full)/3]
	var hits atomic.Int32
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(trunc)
	}))
	t.Cleanup(ts2.Close)

	fs := afero.NewMemMapFs()
	movie := geometryMovie(ts2.URL+"/cover.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1.0, SourceAspect: 1000.0 / 600.0,
	}, true)
	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	assert.Error(t, err, "broken source fails exactly like the pre-change auto-crop path")
	require.False(t, result.Downloaded)
	assert.Equal(t, int32(1), hits.Load())
}

// Geometry that rounds to an empty pixel rect falls back cleanly to the
// scraper auto-crop.
func TestDownloadPoster_DegenerateRectFallsBack(t *testing.T) {
	server := serveTwoToneSource(t)
	fs := afero.NewMemMapFs()
	movie := geometryMovie(server.URL+"/cover.jpg", &models.CropBounds{
		X: 0.5, Y: 0.5, Width: 0.0001, Height: 0.0001, SourceAspect: 1000.0 / 600.0,
	}, true)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)
	img, w, _ := decodeResultPoster(t, fs, result.LocalPath)
	assert.InDelta(t, 472, w, 3, "degenerate geometry must fall back to auto-crop")
	assert.Greater(t, sampleLuma(img, 0.5, 0.5), 215.0)
}

// Rename failure on the direct-promote fallback: clean error, no dangling
// temp path in the result.
func TestDownloadPoster_PromoteFailureIsCleanError(t *testing.T) {
	fs := renameFailFs{afero.NewMemMapFs()}
	// Undecodable payload: the geometry path bails to the direct-promote fallback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Binary-typed so the content guard lets it through: an undecodable
		// payload is exactly what pushes the geometry path to the
		// direct-promote fallback under test here. (A declared text/* type
		// would be refused earlier as provably-not-media.)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("definitely not jpeg"))
	}))
	t.Cleanup(srv.Close)
	movie := geometryMovie(srv.URL+"/poster.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1, SourceAspect: 1000.0 / 600.0,
	}, false)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.Error(t, err)
	assert.False(t, result.Downloaded)
	assert.Empty(t, result.LocalPath, "removed temp path must never leak into the result")
	assert.Zero(t, result.Size)
}

// Existing destination + pending manual crop geometry: existing artwork is
// still never replaced — replacement would break revert (downloaded paths are
// deleted on revert, and the pre-existing poster would be gone). In-place
// artwork refresh is follow-up work requiring overwrite tracking.
func TestDownloadPoster_ExistingDestKeptWithGeometry(t *testing.T) {
	server := serveTwoToneSource(t)
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/dest/IPX-535-poster.jpg", []byte("old artwork"), 0o644))
	movie := geometryMovie(server.URL+"/cover.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1.0, SourceAspect: 1000.0 / 600.0,
	}, true)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	assert.False(t, result.Downloaded, "existing poster must be kept even with pending geometry")
	content, err := afero.ReadFile(fs, "/dest/IPX-535-poster.jpg")
	require.NoError(t, err)
	assert.Equal(t, []byte("old artwork"), content)
}

// Existing destination without pending geometry keeps pre-change behavior:
// the existing file is left untouched.
func TestDownloadPoster_ExistingDestKeptWithoutGeometry(t *testing.T) {
	server := serveTwoToneSource(t)
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/dest/IPX-535-poster.jpg", []byte("old artwork"), 0o644))
	movie := geometryMovie(server.URL+"/cover.jpg", nil, true)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	assert.False(t, result.Downloaded, "no geometry: existing poster must be kept")
	content, err := afero.ReadFile(fs, "/dest/IPX-535-poster.jpg")
	require.NoError(t, err)
	assert.Equal(t, []byte("old artwork"), content)
}

// finalizePosterResult clears the location fields when the promoted file
// cannot be stat'd, and points at the file with its size when it can.
func TestFinalizePosterResult_StatFailClearsLocation(t *testing.T) {
	d := newGeometryDownloader(afero.NewMemMapFs())
	result := &DownloadResult{Downloaded: true, LocalPath: "/dest/X-poster.jpg.full.tmp", Size: 99}
	d.finalizePosterResult(result, "/dest/X-poster.jpg")
	assert.Empty(t, result.LocalPath, "missing destination must clear the temp path")
	assert.Zero(t, result.Size)
}

func TestFinalizePosterResult_StatSuccessSetsLocation(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/dest/X-poster.jpg", []byte("img"), 0o644))
	d := newGeometryDownloader(fs)
	result := &DownloadResult{Downloaded: true}
	d.finalizePosterResult(result, "/dest/X-poster.jpg")
	assert.Equal(t, "/dest/X-poster.jpg", result.LocalPath)
	assert.Equal(t, int64(3), result.Size)
}
func TestDownloadPoster_ManualGeometryApplied(t *testing.T) {
	server := serveTwoToneSource(t)
	fs := afero.NewMemMapFs()
	movie := geometryMovie(server.URL+"/cover.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1.0, SourceAspect: 1000.0 / 600.0,
	}, true)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img, w, h := decodeResultPoster(t, fs, result.LocalPath)
	assert.InDelta(t, 400, w, 2, "manual crop width (0.4 of 1000px)")
	assert.Equal(t, 600, h)
	assert.Less(t, sampleLuma(img, 0.5, 0.5), 40.0,
		"manual LEFT crop must keep the black half (auto-crop would have kept white)")
}

// Invalid geometry never fails organize: with should_crop_poster=true the
// scraper auto-crop still runs.
func TestDownloadPoster_InvalidGeometryFallsBackToAutoCrop(t *testing.T) {
	server := serveTwoToneSource(t)
	fs := afero.NewMemMapFs()
	movie := geometryMovie(server.URL+"/cover.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 1.5, Height: 1.0, SourceAspect: 1000.0 / 600.0,
	}, true)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img, w, _ := decodeResultPoster(t, fs, result.LocalPath)
	assert.InDelta(t, 472, w, 3, "auto-crop keeps the right ~47.2%%")
	assert.Greater(t, sampleLuma(img, 0.5, 0.5), 215.0, "auto-crop must keep the white half")
}

// With should_crop_poster=false the pre-geometry direct-download success path
// is preserved even when geometry is present but invalid.
func TestDownloadPoster_InvalidGeometryKeepsDirectDownloadSuccess(t *testing.T) {
	server := serveTwoToneSource(t)
	fs := afero.NewMemMapFs()
	movie := geometryMovie(server.URL+"/cover.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0, Height: 0, SourceAspect: 1000.0 / 600.0,
	}, false)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	_, w, h := decodeResultPoster(t, fs, result.LocalPath)
	assert.Equal(t, 1000, w)
	assert.Equal(t, 600, h, "uncropped source must land unchanged")
}

// Geometry whose recorded aspect no longer matches the downloaded image is
// stale (different image behind the same URL): refuse it and fall back —
// reusing the one download already made (single-use URLs must not be re-GET).
func TestDownloadPoster_AspectMismatchFallsBack(t *testing.T) {
	var hits atomic.Int32
	img := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.White)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 95})
	}))
	t.Cleanup(srv.Close)
	fs := afero.NewMemMapFs()
	movie := geometryMovie(srv.URL+"/cover.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1.0, SourceAspect: 378.0 / 529.0,
	}, true)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	got, _, _ := decodeResultPoster(t, fs, result.LocalPath)
	assert.Greater(t, sampleLuma(got, 0.5, 0.5), 215.0, "stale aspect must fall back to white auto-crop")
	assert.Equal(t, int32(1), hits.Load(), "auto-crop fallback must reuse the downloaded temp, not re-GET")
}

// An undecodable download with should_crop_poster=false keeps today's
// direct-download success — a previously successful organize must not newly fail.
func TestDownloadPoster_UndecodableKeepsDirectDownloadSuccess(t *testing.T) {
	raw := []byte("not an image at all")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Binary-typed: undeclared bytes sniff to text/plain, which the media
		// guard now refuses by design; this test asserts the lenient fallback
		// for undecodable-but-binary payloads.
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	fs := afero.NewMemMapFs()
	movie := geometryMovie(srv.URL+"/poster.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1.0, SourceAspect: 0.5,
	}, false)

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	content, err := afero.ReadFile(fs, result.LocalPath)
	require.NoError(t, err)
	assert.Equal(t, raw, content, "direct download must deliver raw bytes unchanged")
	assert.Equal(t, int32(1), hits.Load(), "direct fallback must reuse the downloaded temp, not re-GET")
}

// Geometry flagged as measured against the legacy already-cropped preview is
// never applied — pre-geometry behavior exactly.
func TestDownloadPoster_NonFullSourceGeometryIgnored(t *testing.T) {
	server := serveTwoToneSource(t)
	fs := afero.NewMemMapFs()
	movie := geometryMovie(server.URL+"/cover.jpg", &models.CropBounds{
		X: 0, Y: 0, Width: 0.4, Height: 1.0, SourceAspect: 1000.0 / 600.0,
	}, true)
	movie.Poster.PosterCropSourceFull = false

	result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, "/dest", nil)
	require.NoError(t, err)
	require.True(t, result.Downloaded)

	img, _, _ := decodeResultPoster(t, fs, result.LocalPath)
	assert.Greater(t, sampleLuma(img, 0.5, 0.5), 215.0, "legacy geometry must be ignored (auto-crop white)")
}
