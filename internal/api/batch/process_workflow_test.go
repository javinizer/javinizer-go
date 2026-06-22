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
	fs := afero.NewOsFs()
	gen := nfo.NewGenerator(fs, &nfo.Config{})
	if err := gen.WriteNFO(&nfo.Movie{ID: id, Title: title}, path); err != nil {
		t.Fatalf("writeWorkflowNFO() error = %v", err)
	}
}

func TestProcessUpdateMode_GeneratesMergedNFO(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Format.FilenameTemplate = "<ID>.nfo"

	deps := createTestDeps(t, cfg, "")
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "ABC-123.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeWorkflowNFO(t, filepath.Join(tempDir, "ABC-123.nfo"), "ABC-123", "Existing Title")

	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "ABC-123"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:        "ABC-123",
			ContentID: "abc123",
			Title:     "Scraped Title",
			Maker:     "Maker",
		},
	})

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	if status.Status != models.JobStatusCompleted {
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

func TestProcessUpdateMode_TemplatedTitleNotDoubleApplied(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Format.FilenameTemplate = "<ID>.nfo"
	cfg.Metadata.NFO.Format.DisplayTitle = "[<ID>] <TITLE>"

	deps := createTestDeps(t, cfg, "")
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "ABC-123.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeWorkflowNFO(t, filepath.Join(tempDir, "ABC-123.nfo"), "ABC-123", "[ABC-123] Existing Templated Title")

	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "ABC-123"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:        "ABC-123",
			ContentID: "abc123",
			Title:     "Scraped Title",
		},
	})

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	if status.Status != models.JobStatusCompleted {
		t.Fatalf("job status = %q, want completed", status.Status)
	}

	parseResult, err := nfo.ParseNFO(afero.NewOsFs(), filepath.Join(tempDir, "ABC-123.nfo"))
	if err != nil {
		t.Fatalf("ParseNFO() error = %v", err)
	}
	if parseResult.Movie.Title != "[ABC-123] Scraped Title" {
		t.Fatalf("merged NFO title = %q, want %q", parseResult.Movie.Title, "[ABC-123] Scraped Title")
	}
}

func TestProcessUpdateMode_CancelledContextMarksJobCancelled(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false

	deps := createTestDeps(t, cfg, "")
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "XYZ-999.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "XYZ-999"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:        "XYZ-999",
			ContentID: "xyz999",
			Title:     "Cancelable",
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	testStartUpdateApply(ctx, job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	if status := job.GetStatus(); status.Status != models.JobStatusCancelled {
		t.Fatalf("job status = %q, want cancelled", status.Status)
	}
}

func TestProcessOrganizeJob_CopiesFileAndGeneratesNFO(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = []string{} // Disable subfolder for this test
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Format.FilenameTemplate = "<ID>.nfo"

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	destDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "IPX-777.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-777", Extension: ".mp4"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:        "IPX-777",
			ContentID: "ipx777",
			Title:     "Organized Movie",
			Maker:     "IdeaPocket",
		},
	})

	testStartOrganizeApply(context.Background(), job, deps.JobStore, destDir, true, "", false, false, deps.CoreDeps.DB, cfg, deps.CoreDeps.ScraperRegistry, nil)

	status := job.GetStatus()
	if status.Status != models.JobStatusOrganized {
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

func TestProcessOrganizeJob_CancelledContext(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Feature.Enabled = false

	deps := createTestDeps(t, cfg, "")
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "IPX-999.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-999"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:        "IPX-999",
			ContentID: "ipx999",
			Title:     "Test Movie",
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	testStartOrganizeApply(ctx, job, deps.JobStore, t.TempDir(), false, "", false, false, deps.CoreDeps.DB, cfg, deps.CoreDeps.ScraperRegistry, nil)

	status := job.GetStatus()
	if status.Status != models.JobStatusCancelled {
		t.Fatalf("job status = %q, want cancelled", status.Status)
	}
}
