package worker

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type multipartDownloadWorkflow struct {
	stubApplyWorkflow
	dl      *downloader.Downloader
	mu      sync.Mutex
	results []*workflow.ApplyResult
}

func (w *multipartDownloadWorkflow) Apply(ctx context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	outcome, err := w.dl.Download(ctx, downloader.DownloadCmd{
		Movie:                  cmd.Movie,
		DestDir:                cmd.DestPath,
		Multipart:              &downloader.MultipartInfo{IsMultiPart: cmd.Match.IsMultiPart, PartNumber: cmd.Match.PartNumber, PartSuffix: cmd.Match.PartSuffix},
		OverwriteExistingMedia: cmd.OverwriteExistingMedia,
		Dedup:                  cmd.Dedup,
	})
	if err != nil && outcome == nil {
		return nil, err
	}
	result := &workflow.ApplyResult{Movie: cmd.Movie}
	if outcome != nil {
		result.DownloadPaths = outcome.CreatedPaths
	}
	w.mu.Lock()
	w.results = append(w.results, result)
	w.mu.Unlock()
	return result, err
}

func (w *multipartDownloadWorkflow) applyResults() []*workflow.ApplyResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]*workflow.ApplyResult(nil), w.results...)
}

func TestApplyPhase_MultipartSharedMediaDownloadsOnce(t *testing.T) {
	validJPEG := func() []byte {
		var buf bytes.Buffer
		require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 600, 400)), &jpeg.Options{Quality: 90}))
		return buf.Bytes()
	}()
	tests := []struct {
		name      string
		configure func(*downloader.Config)
		movie     func(string) *models.Movie
		path      string
		body      []byte
	}{
		{
			name: "shared trailer",
			configure: func(cfg *downloader.Config) {
				cfg.DownloadTrailer = true
				cfg.TrailerFormat = "<ID>-trailer.mp4"
			},
			movie: func(url string) *models.Movie {
				return &models.Movie{ID: "TEST-015", TrailerURL: url}
			},
			path: "/output/TEST-015-trailer.mp4",
			body: []byte("trailer bytes"),
		},
		{
			name: "shared cover",
			configure: func(cfg *downloader.Config) {
				cfg.DownloadCover = true
				cfg.FanartFormat = "<ID>-fanart.jpg"
			},
			movie: func(url string) *models.Movie {
				return &models.Movie{ID: "TEST-015", Poster: models.PosterState{CoverURL: url}}
			},
			path: "/output/TEST-015-fanart.jpg",
			body: []byte("cover bytes"),
		},
		{
			name: "shared direct poster",
			configure: func(cfg *downloader.Config) {
				cfg.DownloadPoster = true
				cfg.PosterFormat = "<ID>-poster.jpg"
			},
			movie: func(url string) *models.Movie {
				return &models.Movie{ID: "TEST-015", Poster: models.PosterState{PosterURL: url}}
			},
			path: "/output/TEST-015-poster.jpg",
			body: []byte("poster bytes"),
		},
		{
			name: "shared cropped poster",
			configure: func(cfg *downloader.Config) {
				cfg.DownloadPoster = true
				cfg.PosterFormat = "<ID>-poster.jpg"
			},
			movie: func(url string) *models.Movie {
				return &models.Movie{ID: "TEST-015", Poster: models.PosterState{CoverURL: url, ShouldCropPoster: true}}
			},
			path: "/output/TEST-015-poster.jpg",
			body: validJPEG,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(tc.body)
			}))
			defer server.Close()

			fs := afero.NewMemMapFs()
			cfg := &downloader.Config{MediaFormatConfig: organizer.MediaFormatConfig{ScreenshotFolder: "extrafanart", ActressFolder: "actors"}}
			tc.configure(cfg)
			dl := downloader.NewDownloader(server.Client(), fs, cfg, nil)
			wf := &multipartDownloadWorkflow{dl: dl}
			inputs := makeApplyInputs(wf)
			inputs.Concurrency.MaxWorkers = 2
			inputs.Results = map[string]*resultstore.MovieResult{
				"/source/TEST-015-pt1.mp4": {
					FileMatchInfo: models.FileMatchInfo{Path: "/source/TEST-015-pt1.mp4", MovieID: "TEST-015", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
					Status:        models.JobStatusCompleted,
					Movie:         tc.movie(server.URL + "/media"),
				},
				"/source/TEST-015-pt2.mp4": {
					FileMatchInfo: models.FileMatchInfo{Path: "/source/TEST-015-pt2.mp4", MovieID: "TEST-015", IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"},
					Status:        models.JobStatusCompleted,
					Movie:         tc.movie(server.URL + "/media"),
				},
			}

			NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
				Destination:            "/output",
				Download:               true,
				OverwriteExistingMedia: true,
				OrganizeOptions:        workflow.OrganizeOptions{Skip: true},
			})

			results := wf.applyResults()
			require.Len(t, results, 2)
			createdCount := 0
			for _, result := range results {
				createdCount += len(result.DownloadPaths)
			}
			assert.Equal(t, 1, createdCount)
			assert.Equal(t, int32(1), requests.Load())
			got, err := afero.ReadFile(fs, tc.path)
			require.NoError(t, err)
			if tc.name == "shared cropped poster" {
				assert.NotEmpty(t, got)
			} else {
				assert.Equal(t, tc.body, got)
			}
		})
	}
}
