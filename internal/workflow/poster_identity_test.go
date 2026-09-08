package workflow

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

func TestApplyPosterRecropMergeCannotEraseIntent(t *testing.T) {
	var source bytes.Buffer
	require.NoError(t, jpeg.Encode(&source, image.NewRGBA(image.Rect(0, 0, 100, 60)), nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(source.Bytes())
	}))
	defer server.Close()
	for _, fingerprint := range []string{"", assetidentity.FromBytes([]byte("different review bytes")).Fingerprint, assetidentity.FromBytes(source.Bytes()).Fingerprint} {
		t.Run(fingerprint, func(t *testing.T) {
			root := t.TempDir()
			fs := afero.NewMemMapFs()
			bounds := &models.CropBounds{Width: .4, Height: 1, SourceFingerprint: fingerprint}
			movie := &models.Movie{ID: "CROP-1", Poster: models.PosterState{PosterURL: "https://review.test/a.jpg", PosterCropBounds: bounds, PosterCropSourceFull: true}}
			merged := &models.Movie{ID: movie.ID, Poster: models.PosterState{PosterURL: server.URL + "/merged.jpg", ShouldCropPoster: true}}
			d := downloader.NewDownloader(server.Client(), fs, &downloader.Config{DownloadPoster: true, MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"}}, nil)
			impl := &applyOrchImpl{fs: fs, downloader: d, nfo: &stubNFOFileMerger{result: nfo.MergeWithExistingResult{Movie: merged, Merged: true}}, revertLog: noOpRevertLog{}}
			result, err := impl.Execute(context.Background(), ApplyCmd{Movie: movie, Match: models.FileMatchInfo{Path: filepath.Join(root, "movie.mp4"), MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{Skip: true}, Download: true})
			if fingerprint == assetidentity.FromBytes(source.Bytes()).Fingerprint {
				require.NoError(t, err)
				require.True(t, result.Steps.Downloaded)
			} else {
				require.ErrorIs(t, err, downloader.ErrPosterRecropRequired)
				require.Empty(t, result.DownloadPaths)
				_, statErr := fs.Stat(filepath.Join(root, "CROP-1-poster.jpg"))
				require.True(t, errors.Is(statErr, afero.ErrFileNotFound))
			}
			require.Equal(t, bounds, result.Movie.Poster.PosterCropBounds)
		})
	}
}

func TestApplyPosterRecropPreservesCover(t *testing.T) {
	var source bytes.Buffer
	require.NoError(t, jpeg.Encode(&source, image.NewRGBA(image.Rect(0, 0, 100, 60)), nil))
	for _, refusal := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary HTTP error", true: "identity refusal"}[refusal], func(t *testing.T) {
			root := t.TempDir()
			fs := afero.NewMemMapFs()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/poster.jpg" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(source.Bytes())
			}))
			defer server.Close()
			movie := &models.Movie{ID: "CROP-1", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg", PosterURL: server.URL + "/poster.jpg"}}
			if refusal {
				movie.Poster.PosterCropBounds = &models.CropBounds{Width: .4, Height: 1}
				movie.Poster.PosterCropSourceFull = true
			}
			d := downloader.NewDownloader(server.Client(), fs, &downloader.Config{DownloadCover: true, DownloadPoster: true, MediaFormatConfig: organizer.MediaFormatConfig{FanartFormat: "<ID>-fanart.jpg", PosterFormat: "<ID>-poster.jpg"}}, nil)
			impl := &applyOrchImpl{fs: fs, downloader: d, nfo: &applyStubNFO{}, revertLog: noOpRevertLog{}}
			result, err := impl.Execute(context.Background(), ApplyCmd{Movie: movie, Match: models.FileMatchInfo{Path: filepath.Join(root, "movie.mp4"), MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{Skip: true}, Download: true})
			if refusal {
				require.ErrorIs(t, err, downloader.ErrPosterRecropRequired)
				var typed *downloader.PosterRecropRequiredError
				require.ErrorAs(t, err, &typed)
				require.Equal(t, downloader.SourceFingerprintMissing, typed.Reason)
				require.False(t, result.Steps.Downloaded)
				require.False(t, result.Steps.NFOGenerated)
			} else {
				require.NoError(t, err)
				require.False(t, errors.Is(err, downloader.ErrPosterRecropRequired))
			}
			cover := filepath.Join(root, "CROP-1-fanart.jpg")
			require.Equal(t, []string{cover}, result.DownloadPaths)
			got, err := afero.ReadFile(fs, cover)
			require.NoError(t, err)
			require.Equal(t, source.Bytes(), got)
		})
	}
}
