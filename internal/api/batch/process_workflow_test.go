package batch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

func writeWorkflowNFO(t *testing.T, path, id, title string) {
	t.Helper()

	gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{})
	if err := gen.WriteNFO(&nfo.Movie{ID: id, Title: title}, path); err != nil {
		t.Fatalf("WriteNFO() error = %v", err)
	}
}

func TestProcessUpdateMode_GeneratesMergedNFO(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"

	deps := createTestDeps(t, cfg, "")
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "ABC-123.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeWorkflowNFO(t, filepath.Join(tempDir, "ABC-123.nfo"), "ABC-123", "Existing Title")

	job := deps.JobQueue.CreateJob([]string{filePath})
	job.UpdateFileResult(filePath, &worker.FileResult{
		FilePath: filePath,
		MovieID:  "ABC-123",
		Status:   worker.JobStatusCompleted,
		Data: &models.Movie{
			ID:        "ABC-123",
			ContentID: "abc123",
			Title:     "Scraped Title",
			Maker:     "Maker",
		},
	})

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background())

	status := job.GetStatus()
	if status.Status != worker.JobStatusCompleted {
		t.Fatalf("job status = %q, want completed", status.Status)
	}

	parseResult, err := nfo.ParseNFO(afero.NewOsFs(), filepath.Join(tempDir, "ABC-123.nfo"))
	if err != nil {
		t.Fatalf("ParseNFO() error = %v", err)
	}
	if parseResult.Movie.Title != "Scraped Title" {
		t.Fatalf("merged NFO title = %q, want %q", parseResult.Movie.Title, "Scraped Title")
	}
}

func TestProcessUpdateMode_CancelledContextMarksJobCancelled(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false

	deps := createTestDeps(t, cfg, "")
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "XYZ-999.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	job := deps.JobQueue.CreateJob([]string{filePath})
	job.UpdateFileResult(filePath, &worker.FileResult{
		FilePath: filePath,
		MovieID:  "XYZ-999",
		Status:   worker.JobStatusCompleted,
		Data: &models.Movie{
			ID:        "XYZ-999",
			ContentID: "xyz999",
			Title:     "Cancelable",
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	processUpdateMode(job, cfg, deps.DB, deps.Registry, ctx)

	if status := job.GetStatus(); status.Status != worker.JobStatusCancelled {
		t.Fatalf("job status = %q, want cancelled", status.Status)
	}
}

func TestProcessOrganizeJob_CopiesFileAndGeneratesNFO(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.FolderFormat = "<ID>"
	cfg.Output.FileFormat = "<ID>"
	cfg.Output.SubfolderFormat = []string{} // Disable subfolder for this test
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	destDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "IPX-777.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	job := deps.JobQueue.CreateJob([]string{filePath})
	job.UpdateFileResult(filePath, &worker.FileResult{
		FilePath: filePath,
		MovieID:  "IPX-777",
		Status:   worker.JobStatusCompleted,
		Data: &models.Movie{
			ID:        "IPX-777",
			ContentID: "ipx777",
			Title:     "Organized Movie",
			Maker:     "IdeaPocket",
		},
	})

	processOrganizeJob(job, deps.JobQueue, destDir, true, "", deps.DB, cfg, deps.Registry)

	status := job.GetStatus()
	if status.Status != worker.JobStatusOrganized {
		t.Fatalf("job status = %q, want organized", status.Status)
	}

	targetFolder := filepath.Join(destDir, "IPX-777")
	if _, err := os.Stat(filepath.Join(targetFolder, "IPX-777.mp4")); err != nil {
		t.Fatalf("organized file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetFolder, "IPX-777.nfo")); err != nil {
		t.Fatalf("generated NFO missing: %v", err)
	}
}
