package downloader

import (
	"context"
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

func TestDownloadPoster_DedupNonOwnerSkipsAfterCropOwnerFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "failed", http.StatusBadGateway)
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
	assert.True(t, second.Skipped)
	assert.False(t, second.Downloaded)
	assert.Equal(t, int32(1), requests.Load())
}
