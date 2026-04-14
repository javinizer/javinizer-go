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

	cfg := config.DefaultConfig()
	cfg.Metadata.NFO.PerFile = true
	cfg.Metadata.NFO.FilenameTemplate = "<ID>-custom.nfo"
	cfg.Output.DownloadCover = true
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = true
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "IPX-321-pt1.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

	// Force legacy-path discovery and parse failure.
	legacyPerFileNFO := filepath.Join(sourceDir, "IPX-321-pt1.nfo")
	require.NoError(t, os.WriteFile(legacyPerFileNFO, []byte("<movie"), 0o644))

	job := deps.JobQueue.CreateJob([]string{filePath})

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

	job.UpdateFileResult(filePath, &worker.FileResult{
		FilePath: filePath,
		MovieID:  "IPX-321",
		Status:   worker.JobStatusCompleted,
		Data: &models.Movie{
			ID:          "IPX-321",
			Title:       "Legacy Merge Coverage",
			CoverURL:    mediaServer.URL + "/cover.jpg",
			Screenshots: []string{mediaServer.URL + "/missing.jpg"},
		},
	})

	// Force history logging branches to exercise warning paths.
	require.NoError(t, deps.DB.Exec("DROP TABLE history").Error)

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil)

	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status)
	assert.Equal(t, 100.0, status.Progress)
	assert.True(t, dirContainsNFO(t, sourceDir))
}

func TestProcessUpdateMode_MetadataFallbackFilename(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false
	cfg.Metadata.NFO.FilenameTemplate = "<TITLE>"

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "UNKNOWN.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

	job := deps.JobQueue.CreateJob([]string{filePath})
	job.UpdateFileResult(filePath, &worker.FileResult{
		FilePath: filePath,
		MovieID:  "",
		Status:   worker.JobStatusCompleted,
		Data: &models.Movie{
			ID:    "",
			Title: "///",
		},
	})

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil)

	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status)
	assert.True(t, dirContainsNFO(t, sourceDir))
}

func TestProcessUpdateMode_InvalidConditionalTemplateFallsBackToMovieID(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false
	cfg.Metadata.NFO.FilenameTemplate = "<IF:ID>broken"

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "IPX-654.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

	job := deps.JobQueue.CreateJob([]string{filePath})
	job.UpdateFileResult(filePath, &worker.FileResult{
		FilePath: filePath,
		MovieID:  "IPX-654",
		Status:   worker.JobStatusCompleted,
		Data: &models.Movie{
			ID:    "IPX-654",
			Title: "Template Error Fallback",
		},
	})

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil)

	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status)
	assert.False(t, dirContainsNFO(t, sourceDir))
}
