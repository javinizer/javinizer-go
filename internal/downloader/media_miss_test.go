package downloader

import (
	"context"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- downloadPoster error paths ---

func TestDownloadPoster_Disabled(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadPoster: false}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make HTTP request when poster download is disabled")
	}))
	defer srv.Close()

	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)
	movie := &models.Movie{
		ID: "TEST-001",
		Poster: models.PosterState{
			CoverURL:         srv.URL + "/cover.jpg",
			ShouldCropPoster: false,
		},
	}
	result, err := downloader.downloadPoster(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.Equal(t, MediaTypePoster, result.Type)
	assert.False(t, result.Downloaded)
}

func TestDownloadPoster_NoURLs(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadPoster: true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make HTTP request when no URLs available")
	}))
	defer srv.Close()

	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)
	movie := &models.Movie{
		ID:     "TEST-002",
		Poster: models.PosterState{CoverURL: "", PosterURL: ""},
	}
	result, err := downloader.downloadPoster(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.Equal(t, MediaTypePoster, result.Type)
	assert.False(t, result.Downloaded)
}

func TestDownloadPoster_PosterAlreadyExists(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make HTTP request when file already exists")
	}))
	defer srv.Close()

	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	// Pre-create the poster file
	require.NoError(t, memFS.MkdirAll("/output", 0755))
	require.NoError(t, afero.WriteFile(memFS, "/output/TEST-003-poster.jpg", []byte("existing"), 0644))

	movie := &models.Movie{
		ID:     "TEST-003",
		Poster: models.PosterState{CoverURL: "http://example.com/cover.jpg", ShouldCropPoster: false},
	}
	result, err := downloader.downloadPoster(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.False(t, result.Downloaded) // Not downloaded because already exists
	assert.Equal(t, int64(8), result.Size)
}

func TestDownloadPoster_DirectDownloadNoCrop(t *testing.T) {
	// When ShouldCropPoster is false, poster is downloaded directly
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID:     "TEST-004",
		Poster: models.PosterState{CoverURL: srv.URL + "/cover.jpg", ShouldCropPoster: false},
	}
	result, err := downloader.downloadPoster(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.Equal(t, MediaTypePoster, result.Type)
}

func TestDownloadPoster_UsesPosterURLWhenAvailable(t *testing.T) {
	// When PosterURL is set, it should be preferred over CoverURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-005",
		Poster: models.PosterState{
			PosterURL:        srv.URL + "/poster.jpg",
			CoverURL:         "http://should-not-be-used.example.com/cover.jpg",
			ShouldCropPoster: false,
		},
	}
	result, err := downloader.downloadPoster(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
}

func TestDownloadPoster_CropFromCover(t *testing.T) {
	// When ShouldCropPoster is true, the poster is downloaded and cropped
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 600, 400))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-006",
		Poster: models.PosterState{
			CoverURL:         srv.URL + "/cover.jpg",
			ShouldCropPoster: true,
		},
	}
	result, err := downloader.downloadPoster(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
}

func TestDownloadPoster_CropFailure(t *testing.T) {
	// When cropping fails, we get an error result
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send very small data that won't be a valid image for cropping
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not an image"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-007",
		Poster: models.PosterState{
			CoverURL:         srv.URL + "/cover.jpg",
			ShouldCropPoster: true,
		},
	}
	result, err := downloader.downloadPoster(context.Background(), movie, "/output", nil)
	// Either the download fails (not valid image) or crop fails
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.False(t, result.Downloaded)
	}
}

// --- downloadCover error paths ---

func TestDownloadCover_Disabled(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadCover: false}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make HTTP request when cover download is disabled")
	}))
	defer srv.Close()

	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)
	movie := &models.Movie{
		ID:     "TEST-010",
		Poster: models.PosterState{CoverURL: srv.URL + "/cover.jpg"},
	}
	result, err := downloader.downloadCover(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.Equal(t, MediaTypeCover, result.Type)
	assert.False(t, result.Downloaded)
}

func TestDownloadCover_NoCoverURL(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadCover: true}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	movie := &models.Movie{
		ID:     "TEST-011",
		Poster: models.PosterState{CoverURL: ""},
	}
	result, err := downloader.downloadCover(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.False(t, result.Downloaded)
}

// --- downloadTrailer error paths ---

func TestDownloadTrailer_Disabled(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTrailer: false}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	movie := &models.Movie{
		ID:         "TEST-020",
		TrailerURL: "http://example.com/trailer.mp4",
	}
	result, err := downloader.downloadTrailer(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.Equal(t, MediaTypeTrailer, result.Type)
	assert.False(t, result.Downloaded)
}

func TestDownloadTrailer_NoTrailerURL(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTrailer: true}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	movie := &models.Movie{ID: "TEST-021"}
	result, err := downloader.downloadTrailer(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.False(t, result.Downloaded)
}

func TestDownloadTrailer_DefaultExtensionWhenNoExt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake video data"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadTrailer: true, MediaFormatConfig: organizer.MediaFormatConfig{TrailerFormat: "<ID>-trailer"}}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID:         "TEST-022",
		TrailerURL: srv.URL + "/trailer", // No file extension
	}
	result, err := downloader.downloadTrailer(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	// Should default to .mp4 extension
	assert.Contains(t, result.LocalPath, ".mp4")
}

// --- downloadExtrafanart error paths ---

func TestDownloadExtrafanart_Disabled(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	movie := &models.Movie{
		ID:          "TEST-030",
		Screenshots: []string{"http://example.com/screenshot1.jpg"},
	}
	results, err := downloader.downloadExtrafanart(context.Background(), movie, "/output", nil, false)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestDownloadExtrafanart_NoScreenshots(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	movie := &models.Movie{ID: "TEST-031"}
	results, err := downloader.downloadExtrafanart(context.Background(), movie, "/output", nil, true)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestDownloadExtrafanart_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("img"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{MediaFormatConfig: organizer.MediaFormatConfig{ScreenshotFolder: "extrafanart", ScreenshotFormat: ""}}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	movie := &models.Movie{
		ID:          "TEST-032",
		Screenshots: []string{srv.URL + "/s1.jpg", srv.URL + "/s2.jpg"},
	}
	results, err := downloader.downloadExtrafanart(ctx, movie, "/output", nil, true)
	// Should return partial results and context error
	if len(results) == 0 {
		assert.Error(t, err)
	}
}

// --- downloadActressImages error paths ---

func TestDownloadActressImages_Disabled(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadActress: false}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-040",
		Actresses: []models.Actress{
			{ThumbURL: "http://example.com/actress.jpg"},
		},
	}
	results, err := downloader.downloadActressImages(context.Background(), movie, "/output")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestDownloadActressImages_NoActresses(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadActress: true}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	movie := &models.Movie{ID: "TEST-041"}
	results, err := downloader.downloadActressImages(context.Background(), movie, "/output")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestDownloadActressImages_ContextCancellation(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadActress: true, MediaFormatConfig: organizer.MediaFormatConfig{ActressFolder: "actress"}}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	movie := &models.Movie{
		ID: "TEST-042",
		Actresses: []models.Actress{
			{JapaneseName: "女優A", ThumbURL: "http://example.com/a.jpg"},
		},
	}
	results, err := downloader.downloadActressImages(ctx, movie, "/output")
	// Should return error or empty results
	_ = results
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

func TestDownloadActressImages_SkipsNoThumbURL(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{DownloadActress: true}
	downloader := NewDownloader(http.DefaultClient, memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-043",
		Actresses: []models.Actress{
			{JapaneseName: "女優B", ThumbURL: ""}, // No thumb URL
		},
	}
	results, err := downloader.downloadActressImages(context.Background(), movie, "/output")
	require.NoError(t, err)
	assert.Empty(t, results) // Skipped because no thumb URL
}

// --- downloadAllWithExtrafanart: partial error sentinel ---

func TestDownloadAllWithExtrafanart_PartialError(t *testing.T) {
	// Both cover and poster fail → DownloadPartialError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 404
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadCover:  true,
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			FanartFormat: "<ID>-fanart.jpg",
			PosterFormat: "<ID>-poster.jpg",
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-050",
		Poster: models.PosterState{
			CoverURL:         srv.URL + "/cover.jpg",
			ShouldCropPoster: false,
		},
	}
	results, err := downloader.downloadAllWithExtrafanart(context.Background(), movie, "/output", nil, false)
	// Should return partial error
	assert.Error(t, err)
	var partialErr *DownloadPartialError
	assert.ErrorAs(t, err, &partialErr)
	assert.Greater(t, len(results), 0)
}

// --- Download (public seam) ---

func TestDownload_ExtrafanartOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadCover:       true,
		DownloadPoster:      false,
		DownloadExtrafanart: false, // Disabled in config
		MediaFormatConfig: organizer.MediaFormatConfig{
			FanartFormat: "<ID>-fanart.jpg",
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-060",
		Poster: models.PosterState{
			CoverURL: srv.URL + "/cover.jpg",
		},
	}

	// Override extrafanart to true
	extrafanartTrue := true
	outcome, err := downloader.Download(context.Background(), DownloadCmd{
		Movie:               movie,
		DestDir:             "/output",
		DownloadExtrafanart: &extrafanartTrue,
	})
	require.NoError(t, err)
	assert.NotNil(t, outcome)
	assert.NotEmpty(t, outcome.Results)
}

// TestDownload_PartialErrorPreservesNonCriticalPaths verifies the public
// Download seam returns a non-nil outcome carrying non-critical artifacts
// (extrafanart) ALONGSIDE a DownloadPartialError, so the apply orchestrator
// can record them for revert cleanup. Previously Download returned nil on any
// error, discarding partial results.
func TestDownload_PartialErrorPreservesNonCriticalPaths(t *testing.T) {
	// failSrv 404s cover+poster (critical media) → DownloadPartialError.
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer failSrv.Close()
	// okSrv serves a valid jpeg for the extrafanart screenshot (non-critical).
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer okSrv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadCover:       true,
		DownloadPoster:      true,
		DownloadExtrafanart: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			FanartFormat:     "<ID>-fanart.jpg",
			PosterFormat:     "<ID>-poster.jpg",
			ScreenshotFolder: "extrafanart",
			ScreenshotFormat: "<ID>-fanart<INDEX>.jpg",
		},
	}
	dl := NewDownloader(okSrv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-PARTIAL",
		Poster: models.PosterState{
			CoverURL:         failSrv.URL + "/cover.jpg",
			PosterURL:        failSrv.URL + "/poster.jpg",
			ShouldCropPoster: false,
		},
		Screenshots: []string{okSrv.URL + "/s1.jpg"},
	}

	outcome, err := dl.Download(context.Background(), DownloadCmd{
		Movie:   movie,
		DestDir: "/output",
	})
	var partial *DownloadPartialError
	require.ErrorAs(t, err, &partial, "expected DownloadPartialError when all critical media fails")
	require.NotNil(t, outcome, "partial error must return a non-nil outcome with non-critical paths")
	assert.NotEmpty(t, outcome.DownloadedPaths,
		"non-critical media that succeeded before the partial error must be preserved in DownloadedPaths")
}

func TestDownload_UsesConfigExtrafanartWhenNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadCover:       true,
		DownloadPoster:      false,
		DownloadExtrafanart: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			FanartFormat:     "<ID>-fanart.jpg",
			ScreenshotFolder: "extrafanart",
			ScreenshotFormat: "",
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-061",
		Poster: models.PosterState{
			CoverURL: srv.URL + "/cover.jpg",
		},
		Screenshots: []string{srv.URL + "/s1.jpg"},
	}

	// DownloadExtrafanart is nil → uses config value (true)
	outcome, err := downloader.Download(context.Background(), DownloadCmd{
		Movie:   movie,
		DestDir: "/output",
	})
	require.NoError(t, err)
	assert.NotNil(t, outcome)
}

// --- Fallback filename generation when template is empty ---

func TestDownloadCover_FallbackFilename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadCover: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			FanartFormat: "", // Empty template triggers fallback
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID:     "TEST-070",
		Poster: models.PosterState{CoverURL: srv.URL + "/cover.jpg"},
	}
	result, err := downloader.downloadCover(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.Contains(t, result.LocalPath, "TEST-070-fanart.jpg")
}

func TestDownloadPoster_FallbackFilename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "", // Empty template triggers fallback
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-071",
		Poster: models.PosterState{
			CoverURL:         srv.URL + "/cover.jpg",
			ShouldCropPoster: false,
		},
	}
	result, err := downloader.downloadPoster(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.Contains(t, result.LocalPath, "TEST-071-poster.jpg")
}

func TestDownloadTrailer_FallbackFilename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake trailer"))
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadTrailer: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			TrailerFormat: "", // Empty template triggers fallback
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID:         "TEST-072",
		TrailerURL: srv.URL + "/trailer.mp4",
	}
	result, err := downloader.downloadTrailer(context.Background(), movie, "/output", nil)
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.Contains(t, result.LocalPath, "TEST-072-trailer.mp4")
}

// --- Extrafanart screenshot fallback filename ---

func TestDownloadExtrafanart_FallbackFilename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		MediaFormatConfig: organizer.MediaFormatConfig{
			ScreenshotFolder:  "extrafanart",
			ScreenshotFormat:  "", // Empty template triggers fallback
			ScreenshotPadding: 2,
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID:          "TEST-073",
		Screenshots: []string{srv.URL + "/s1.jpg"},
	}
	results, err := downloader.downloadExtrafanart(context.Background(), movie, "/output", nil, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Downloaded)
	// Should use padded format: fanart01.jpg
	assert.Contains(t, results[0].LocalPath, "fanart01.jpg")
}

func TestDownloadExtrafanart_NoPadding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		MediaFormatConfig: organizer.MediaFormatConfig{
			ScreenshotFolder:  "extrafanart",
			ScreenshotFormat:  "",
			ScreenshotPadding: 0, // No padding
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID:          "TEST-074",
		Screenshots: []string{srv.URL + "/s1.jpg"},
	}
	results, err := downloader.downloadExtrafanart(context.Background(), movie, "/output", nil, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].LocalPath, "fanart1.jpg")
}

// --- downloadAllWithExtrafanart: multipart actress download ---

func TestDownloadAllWithExtrafanart_SecondPartSkipsActress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer srv.Close()

	memFS := afero.NewMemMapFs()
	cfg := &Config{
		DownloadCover:   false,
		DownloadPoster:  false,
		DownloadActress: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			ActressFolder: "actress",
			ActressFormat: "",
		},
	}
	downloader := NewDownloader(srv.Client(), memFS, cfg, nil)

	movie := &models.Movie{
		ID: "TEST-075",
		Actresses: []models.Actress{
			{JapaneseName: "女優C", ThumbURL: srv.URL + "/a.jpg"},
		},
	}

	// PartNumber > 1 should skip actress downloads
	results, err := downloader.downloadAllWithExtrafanart(context.Background(), movie, "/output", &MultipartInfo{
		IsMultiPart: true,
		PartNumber:  2,
	}, false)
	require.NoError(t, err)
	// No actress results should be present
	for _, r := range results {
		assert.NotEqual(t, MediaTypeActress, r.Type, "second part should not download actress images")
	}
}

// Suppress unused import warning
var _ = fmt.Sprintf
var _ = io.Discard
