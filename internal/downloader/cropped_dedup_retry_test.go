package downloader

import (
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

func TestDownloadPoster_DedupNonOwnerRetriesAfterCropOwnerFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "failed", http.StatusBadGateway)
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, 600, 400))
		w.Header().Set("Content-Type", "image/jpeg")
		require.NoError(t, jpeg.Encode(w, img, &jpeg.Options{Quality: 90}))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	d := NewDownloader(server.Client(), fs, &Config{DownloadPoster: true, MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"}}, nil)
	movie := &models.Movie{ID: "TEST-OWNER", Poster: models.PosterState{CoverURL: server.URL + "/cover.png", ShouldCropPoster: true}}
	dedup := &sync.Map{}

	first, firstErr := d.downloadPoster(context.Background(), movie, "/output", nil, true, dedup)
	require.Error(t, firstErr)
	assert.False(t, first.Downloaded)
	second, secondErr := d.downloadPoster(context.Background(), movie, "/output", nil, true, dedup)
	require.NoError(t, secondErr)
	assert.False(t, second.Skipped)
	assert.True(t, second.Downloaded)
	assert.Equal(t, int32(2), requests.Load())
}

func TestDownloadPoster_ReturnsReservationWaitCancellation(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := NewDownloader(http.DefaultClient, fs, &Config{DownloadPoster: true, MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"}}, nil)
	movie := &models.Movie{ID: "TEST-CANCEL", Poster: models.PosterState{CoverURL: "https://example.com/cover.png", ShouldCropPoster: true}}
	tmplCtx := d.buildTemplateContext(movie, nil)
	path := d.pathResolver.ResolvePosterPath(movie, nil, true, tmplCtx, "/output")
	dedup := &sync.Map{}
	owner, skipped, err := acquireDownloadReservation(context.Background(), dedup, path)
	require.NoError(t, err)
	assert.False(t, skipped)
	require.NotNil(t, owner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := d.downloadPoster(ctx, movie, "/output", nil, true, dedup)
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, result.Error, context.Canceled)
	finishDownloadReservation(dedup, path, owner, false)
}

func TestDownloadPoster_CroppedSkipsClaimedDestination(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := NewDownloader(http.DefaultClient, fs, &Config{DownloadPoster: true, MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"}}, nil)
	movie := &models.Movie{ID: "TEST-SKIP", Poster: models.PosterState{CoverURL: "https://example.com/cover.png", ShouldCropPoster: true}}
	tmplCtx := d.buildTemplateContext(movie, nil)
	path := d.pathResolver.ResolvePosterPath(movie, nil, true, tmplCtx, "/output")
	dedup := &sync.Map{}
	dedup.Store(path, struct{}{})

	result, err := d.downloadPoster(context.Background(), movie, "/output", nil, true, dedup)
	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.False(t, result.Downloaded)
}
