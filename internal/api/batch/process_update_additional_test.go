package batch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dirContainsNFO(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".nfo") {
			return true
		}
	}
	return false
}

func TestProcessUpdateMode_PerFileLegacyNFOAndHistoryFailures(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.NFO.Feature.PerFile = true
	cfg.Metadata.NFO.Format.FilenameTemplate = "<ID>-custom.nfo"
	cfg.Output.Download.DownloadCover = true
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = true
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "IPX-321-pt1.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

	// Force legacy-path discovery and parse failure.
	legacyPerFileNFO := filepath.Join(sourceDir, "IPX-321-pt1.nfo")
	require.NoError(t, os.WriteFile(legacyPerFileNFO, []byte("<movie"), 0o644))

	job := deps.JobStore.CreateJobBatch([]string{filePath})

	mediaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cover.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg"))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("missing"))
		}
	}))
	defer mediaServer.Close()

	setJobResult(job, filePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-321"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:          "IPX-321",
			Title:       "Legacy Merge Coverage",
			Poster:      models.PosterState{CoverURL: mediaServer.URL + "/cover.jpg"},
			Screenshots: []string{mediaServer.URL + "/missing.jpg"},
		},
	})

	// Force history logging branches to exercise warning paths.
	require.NoError(t, deps.CoreDeps.DB.Exec("DROP TABLE history").Error)

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	assert.Equal(t, models.JobStatusCompleted, status.Status)
	assert.Equal(t, 100.0, status.Progress)
	assert.True(t, dirContainsNFO(t, sourceDir))
}

func TestProcessUpdateMode_MetadataFallbackFilename(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Format.FilenameTemplate = "<TITLE>"

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "UNKNOWN.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: ""},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:    "",
			Title: "///",
		},
	})

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	assert.Equal(t, models.JobStatusCompleted, status.Status)
	assert.True(t, dirContainsNFO(t, sourceDir))
}

func TestProcessUpdateMode_InvalidConditionalTemplateFallsBackToMovieID(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Format.FilenameTemplate = "<IF:ID>broken"

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "IPX-654.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-654"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:    "IPX-654",
			Title: "Template Error Fallback",
		},
	})

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	assert.Equal(t, models.JobStatusCompleted, status.Status)
	assert.False(t, dirContainsNFO(t, sourceDir))
}
