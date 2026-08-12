package downloader

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func posterBranchServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 600, 400))
		for y := 0; y < 400; y++ {
			for x := 0; x < 600; x++ {
				img.Set(x, y, color.RGBA{R: 40, G: 80, B: 120, A: 255})
			}
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
}

func posterBranchDownloader(client *http.Client, fs afero.Fs) *Downloader {
	return NewDownloader(client, fs, &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}, nil)
}

func TestDownloadPoster_CroppedFilesystemBranches(t *testing.T) {
	server := posterBranchServer()
	defer server.Close()
	movie := &models.Movie{ID: "POSTER-001", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg", ShouldCropPoster: true}}

	t.Run("existing without overwrite", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		path := nativePath("/output/POSTER-001-poster.jpg")
		require.NoError(t, afero.WriteFile(fs, path, []byte("existing"), 0644))
		result, err := posterBranchDownloader(server.Client(), fs).downloadPoster(context.Background(), movie, "/output", nil)
		require.NoError(t, err)
		assert.False(t, result.Downloaded)
		assert.Equal(t, path, result.LocalPath)
	})

	t.Run("stat failure", func(t *testing.T) {
		base := afero.NewMemMapFs()
		statErr := errors.New("poster stat failed")
		fs := statErrorFS{Fs: base, path: nativePath("/output/POSTER-001-poster.jpg"), err: statErr}
		result, err := posterBranchDownloader(server.Client(), fs).downloadPoster(context.Background(), movie, "/output", nil, true)
		require.Error(t, err)
		assert.ErrorIs(t, err, statErr)
		assert.False(t, result.Downloaded)
	})

	t.Run("replace failure", func(t *testing.T) {
		base := afero.NewMemMapFs()
		path := nativePath("/output/POSTER-001-poster.jpg")
		require.NoError(t, afero.WriteFile(base, path, []byte("old"), 0644))
		// Wedge the staged byte install only; the aside/restore renames
		// (from a .dlbak sibling) must survive so the old bytes restore.
		fs := rejectStagedRenameFS{Fs: base}
		result, err := posterBranchDownloader(server.Client(), fs).downloadPoster(context.Background(), movie, "/output", nil, true, downloadLedger{opID: "test-op-branch", recorder: &armedTestLedger{}})
		require.ErrorContains(t, err, "staged install rejected")
		assert.False(t, result.Downloaded)
		assert.NotNil(t, result.Error)
		got, rerr := afero.ReadFile(base, path)
		require.NoError(t, rerr)
		require.Equal(t, "old", string(got), "failed install restores the pre-existing bytes")
	})
}
