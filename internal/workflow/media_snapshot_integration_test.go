package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type recordingMediaDownloader struct {
	downloader.DownloaderInterface
	cmd downloader.DownloadCmd
}

func (d *recordingMediaDownloader) Download(ctx context.Context, cmd downloader.DownloadCmd) (*downloader.DownloadOutcome, error) {
	d.cmd = cmd
	return d.DownloaderInterface.Download(ctx, cmd)
}

type rejectExistingRenameIntegrationFS struct {
	afero.Fs
}

func (f rejectExistingRenameIntegrationFS) Rename(oldPath, newPath string) error {
	if _, err := f.Stat(newPath); err == nil {
		return errors.New("replacement blocked")
	}
	return f.Fs.Rename(oldPath, newPath)
}

func TestUpdateMediaRevertPreservesReplacementsAndDeletesCreated(t *testing.T) {
	fs := afero.NewMemMapFs()
	anchorPath := "/source/TEST-016.mp4"
	replacedPath := "/source/TEST-016-fanart.jpg"
	createdPath := "/source/TEST-016-trailer.mp4"
	require.NoError(t, afero.WriteFile(fs, anchorPath, []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(fs, replacedPath, []byte("old cover"), 0644))
	require.NoError(t, afero.WriteFile(fs, createdPath, []byte("new trailer"), 0644))
	generated, err := json.Marshal(models.GeneratedFilesJSON{Delete: []string{createdPath}})
	require.NoError(t, err)

	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	op := models.BatchFileOperation{
		ID:             1600,
		BatchJobID:     "job-016",
		MovieID:        "TEST-016",
		OriginalPath:   anchorPath,
		OperationType:  models.OperationTypeUpdate,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: string(generated),
	}
	repo.On("FindByBatchJobID", mock.Anything, "job-016").Return([]models.BatchFileOperation{op}, nil)
	repo.On("FindByID", mock.Anything, uint(1600)).Return(&op, nil)
	repo.On("UpdateRevertStatus", mock.Anything, uint(1600), models.RevertStatusReverted).Return(nil)

	result, err := history.NewReverter(fs, repo).RevertScrape(context.Background(), "job-016", "TEST-016")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Succeeded)
	createdExists, statErr := afero.Exists(fs, createdPath)
	require.NoError(t, statErr)
	assert.False(t, createdExists)
	oldBytes, readErr := afero.ReadFile(fs, replacedPath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("old cover"), oldBytes)

	var requested atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested.Add(1)
		_, _ = w.Write([]byte("replacement bytes"))
	}))
	defer server.Close()
	baseFS := afero.NewMemMapFs()
	failedPath := "/output/TEST-017-fanart.jpg"
	require.NoError(t, afero.WriteFile(baseFS, failedPath, []byte("critical old"), 0644))
	d := downloader.NewDownloader(server.Client(), rejectExistingRenameIntegrationFS{Fs: baseFS}, &downloader.Config{
		DownloadCover: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			FanartFormat: "<ID>-fanart.jpg",
		},
	}, nil)
	_, downloadErr := d.Download(context.Background(), downloader.DownloadCmd{
		Movie:                  &models.Movie{ID: "TEST-017", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg"}},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
		Dedup:                  &sync.Map{},
	})
	require.Error(t, downloadErr)
	assert.Equal(t, int32(1), requested.Load())
	criticalBytes, readErr := afero.ReadFile(baseFS, failedPath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("critical old"), criticalBytes)
}

func TestApply_OverwriteExistingMediaUsesFreshMatchedActressThumbs(t *testing.T) {
	tests := []struct {
		name      string
		nfoActors string
		fresh     []models.Actress
		wantURLs  []string
		wantNames []string
		unmatched string
	}{
		{
			name:      "renamed matched actress",
			nfoActors: `<actor><name>Old Name</name><altname>桜花</altname><thumb>old-thumb</thumb></actor>`,
			fresh:     []models.Actress{{FirstName: "New", LastName: "Name", JapaneseName: "桜花", ThumbURL: "http://fresh.test/fresh-renamed.jpg"}},
			wantURLs:  []string{"/fresh-renamed.jpg"},
			wantNames: []string{"New"},
		},
		{
			name:      "reordered actresses",
			nfoActors: `<actor><name>Alpha One</name><altname>甲</altname><thumb>old-alpha</thumb></actor><actor><name>Beta Two</name><altname>乙</altname><thumb>old-beta</thumb></actor>`,
			fresh: []models.Actress{
				{FirstName: "Beta", LastName: "Two", JapaneseName: "乙", ThumbURL: "http://fresh.test/fresh-beta.jpg"},
				{FirstName: "Alpha", LastName: "One", JapaneseName: "甲", ThumbURL: "http://fresh.test/fresh-alpha.jpg"},
			},
			wantURLs:  []string{"/fresh-beta.jpg", "/fresh-alpha.jpg"},
			wantNames: []string{"Alpha", "Beta"},
		},
		{
			name:      "unmatched NFO-only actress is cleared",
			nfoActors: `<actor><name>Fresh Name</name><altname>新</altname><thumb>old-fresh</thumb></actor><actor><name>NFO Only</name><altname>旧</altname><thumb>old-only</thumb></actor>`,
			fresh:     []models.Actress{{FirstName: "Fresh", LastName: "Name", JapaneseName: "新", ThumbURL: "http://fresh.test/fresh-only.jpg"}},
			wantURLs:  []string{"/fresh-only.jpg"},
			wantNames: []string{"Fresh"},
			unmatched: "旧",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requested, filenames, _, downloadMovie := runActressOverwriteFixture(t, tc.nfoActors, tc.fresh)
			assert.ElementsMatch(t, tc.wantURLs, requested)
			for _, name := range tc.wantNames {
				found := false
				for _, filename := range filenames {
					if strings.Contains(filename, name) {
						found = true
					}
				}
				assert.True(t, found, name)
			}
			if tc.unmatched != "" {
				var unmatched *models.Actress
				for i := range downloadMovie.Actresses {
					if downloadMovie.Actresses[i].JapaneseName == tc.unmatched {
						unmatched = &downloadMovie.Actresses[i]
					}
				}
				require.NotNil(t, unmatched)
				assert.Empty(t, unmatched.ThumbURL)
			}
		})
	}
}

func runActressOverwriteFixture(t *testing.T, actorXML string, fresh []models.Actress) ([]string, []string, *ApplyResult, *models.Movie) {
	t.Helper()
	requested := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/fresh-") {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("fresh actress bytes"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	f := newFixture(t)
	f.httpClient = server.Client()
	f.cfg.Output.Download.DownloadCover = false
	f.cfg.Output.Download.DownloadPoster = false
	f.cfg.Output.Download.DownloadExtrafanart = false
	f.cfg.Output.Download.DownloadTrailer = false
	f.cfg.Output.Download.DownloadActress = true
	f.cfg.Output.MediaFormat.ActressFolder = "actors"
	f.cfg.Output.MediaFormat.ActressFormat = "<ACTORNAME>.jpg"
	f.withSourceFile("/source/TEST-014.mp4")
	nfoContent := `<?xml version="1.0"?><movie><id>TEST-014</id>` + actorXML + `</movie>`
	require.NoError(t, afero.WriteFile(f.fs, "/source/TEST-014.nfo", []byte(nfoContent), 0644))
	f.withDownloader()
	capture := &recordingMediaDownloader{DownloaderInterface: f.dl}
	f.dl = capture
	f.withOrganizer().withNFOGenerator()
	workflow := f.build()
	freshForDownload := append([]models.Actress(nil), fresh...)
	for i := range freshForDownload {
		freshForDownload[i].ThumbURL = strings.ReplaceAll(freshForDownload[i].ThumbURL, "http://fresh.test", server.URL)
	}
	movie := &models.Movie{ID: "TEST-014", Actresses: freshForDownload}
	result, err := workflow.Apply(context.Background(), ApplyCmd{
		Movie:                  movie,
		Match:                  models.FileMatchInfo{Path: "/source/TEST-014.mp4", MovieID: "TEST-014"},
		DestPath:               "/source",
		Organize:               OrganizeOptions{Skip: true},
		Download:               true,
		Merge:                  MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true},
		OverwriteExistingMedia: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	entries, err := afero.ReadDir(f.fs, filepath.Join("/source", "actors"))
	require.NoError(t, err)
	filenames := make([]string, 0, len(entries))
	for _, entry := range entries {
		filenames = append(filenames, entry.Name())
	}
	return requested, filenames, result, capture.cmd.Movie
}

func TestApply_OverwriteExistingMediaUsesFreshScrapedCoverAfterNFOMerge(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		if r.URL.Path == "/fresh-cover.jpg" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("fresh cover bytes"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	f := newFixture(t)
	f.httpClient = server.Client()
	f.cfg.Output.Download.DownloadCover = true
	f.cfg.Output.Download.DownloadPoster = false
	f.cfg.Output.Download.DownloadExtrafanart = false
	f.cfg.Output.Download.DownloadTrailer = false
	f.cfg.Output.Download.DownloadActress = false
	f.cfg.Output.MediaFormat.FanartFormat = "<ID>-fanart.jpg"
	f.withSourceFile("/source/TEST-013.mp4")
	require.NoError(t, afero.WriteFile(f.fs, "/source/TEST-013.nfo", []byte(`<?xml version="1.0"?><movie><title>Existing</title><id>TEST-013</id><thumb>old-cover</thumb></movie>`), 0644))
	f.withDownloader().withOrganizer().withNFOGenerator()
	workflow := f.build()

	result, err := workflow.Apply(context.Background(), ApplyCmd{
		Movie: &models.Movie{
			ID:     "TEST-013",
			Title:  "Fresh title",
			Poster: models.PosterState{CoverURL: server.URL + "/fresh-cover.jpg"},
		},
		Match:                  models.FileMatchInfo{Path: "/source/TEST-013.mp4", MovieID: "TEST-013"},
		DestPath:               "/source",
		Organize:               OrganizeOptions{Skip: true},
		Download:               true,
		Merge:                  MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true},
		OverwriteExistingMedia: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Merged)
	assert.Equal(t, []string{"/fresh-cover.jpg"}, requested)
	got, readErr := afero.ReadFile(f.fs, "/source/TEST-013-fanart.jpg")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("fresh cover bytes"), got)
}
