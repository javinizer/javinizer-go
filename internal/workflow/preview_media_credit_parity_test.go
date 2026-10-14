package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPreviewMediaPathsMatchDownloaderCreditRendering(t *testing.T) {
	cases := []struct {
		name        string
		credited    bool
		firstCredit string
		wantNames   string
	}{
		{name: "credited names disabled", firstCredit: "Stage One", wantNames: "Alpha One + Beta Two"},
		{name: "credited fallback to canonical", credited: true, wantNames: "Alpha One + Stage Two"},
		{name: "multiple credited names", credited: true, firstCredit: "Stage One", wantNames: "Stage One + Stage Two"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			image := pr260PosterBytes(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(image)
			}))
			defer server.Close()

			first := models.Actress{ID: 1, FirstName: "Alpha", LastName: "One", Verified: true}
			second := models.Actress{ID: 2, FirstName: "Beta", LastName: "Two", Verified: true}
			movie := &models.Movie{
				ID:          "PATH-001",
				Poster:      models.PosterState{CoverURL: server.URL + "/cover.jpg", PosterURL: server.URL + "/poster.jpg"},
				TrailerURL:  server.URL + "/trailer.mp4",
				Screenshots: []string{server.URL + "/shot.jpg"},
				Actresses:   []models.Actress{first, second},
				Credits: []models.MovieCredit{
					{ActressID: first.ID, Actress: &first, CreditedName: tc.firstCredit},
					{ActressID: second.ID, Actress: &second, CreditedName: "Stage Two"},
				},
			}

			app := config.DefaultConfig(nil, nil)
			app.Metadata.NFO.Feature.UseCreditedName = tc.credited
			app.Metadata.NFO.Format.FirstNameOrder = true
			app.Output.Template.ActressDelimiter = " + "
			app.Output.MediaFormat.PosterFormat = "poster-<ACTRESSES>.jpg"
			app.Output.MediaFormat.FanartFormat = "fanart-<ACTRESSES>.jpg"
			app.Output.MediaFormat.TrailerFormat = "trailer-<ACTRESSES>.mp4"
			app.Output.MediaFormat.ScreenshotFormat = "shot-<ACTRESSES>-<INDEX>.jpg"
			app.Output.MediaFormat.ScreenshotFolder = "shots"
			app.Output.Download.DownloadPoster = true
			app.Output.Download.DownloadCover = true
			app.Output.Download.DownloadTrailer = true
			app.Output.Download.DownloadExtrafanart = true

			engine := template.NewEngine()
			fs := afero.NewMemMapFs()
			dcs := extractDomainConfigs(app)
			previewCfg, _ := buildOrchestratorConfigs(app, dcs, fs, nil, engine)
			preview := newPreviewOrchestrator(fs, nil, previewCfg, dcs.nfoNameCfg, engine, nil, nil).(*previewOrchImpl)
			dest := filepath.Join("/library", tc.name)
			paths := preview.resolveMediaPaths(movie, nil, &organizer.OrganizePlan{}, organizer.EncodedPaths{TargetDir: dest}, nil, operationmode.OperationModeOrganize, true, false)

			dl := downloader.NewDownloader(server.Client(), fs, dcs.downloadCfg, engine)
			outcome, err := dl.Download(context.Background(), downloader.DownloadCmd{Movie: movie, DestDir: dest})
			require.NoError(t, err)
			require.NotNil(t, outcome)

			actual := make(map[downloader.MediaType][]string)
			for _, result := range outcome.Results {
				if result.Downloaded {
					actual[result.Type] = append(actual[result.Type], result.LocalPath)
				}
			}
			require.Equal(t, []string{paths.PosterPath}, actual[downloader.MediaTypePoster])
			require.Equal(t, []string{paths.FanartPath}, actual[downloader.MediaTypeCover])
			require.Equal(t, []string{paths.TrailerPath}, actual[downloader.MediaTypeTrailer])
			require.Equal(t, []string{filepath.Join(paths.ExtrafanartPath, paths.Screenshots[0])}, actual[downloader.MediaTypeExtrafanart])
			for _, path := range outcome.DownloadedPaths {
				_, err := fs.Stat(path)
				require.NoError(t, err)
				require.Contains(t, filepath.Base(path), tc.wantNames)
			}
		})
	}
}
