package batch

import (
	"context"
	"io/fs"
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

func TestProcessOrganizeJob_HandlesExcludedInvalidAndUnmatchedFiles(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	destDir := t.TempDir()

	excludedPath := filepath.Join(sourceDir, "IPX-111.mp4")
	invalidTypePath := filepath.Join(sourceDir, "IPX-222.mp4")
	unmatchedPath := filepath.Join(sourceDir, "mystery_file.mp4")
	requireWriteFile(t, excludedPath)
	requireWriteFile(t, invalidTypePath)
	requireWriteFile(t, unmatchedPath)

	job := deps.JobQueue.CreateJob([]string{excludedPath, invalidTypePath, unmatchedPath})
	job.UpdateFileResult(excludedPath, &worker.FileResult{
		FilePath: excludedPath,
		MovieID:  "IPX-111",
		Status:   worker.JobStatusCompleted,
		Data:     &models.Movie{ID: "IPX-111", Title: "Excluded"},
	})
	job.UpdateFileResult(invalidTypePath, &worker.FileResult{
		FilePath: invalidTypePath,
		MovieID:  "IPX-222",
		Status:   worker.JobStatusCompleted,
		Data:     struct{}{},
	})
	job.UpdateFileResult(unmatchedPath, &worker.FileResult{
		FilePath: unmatchedPath,
		MovieID:  "UNKNOWN",
		Status:   worker.JobStatusCompleted,
		Data:     &models.Movie{ID: "UNKNOWN", Title: "No Match"},
	})
	job.ExcludeFile(excludedPath)

	processOrganizeJob(job, deps.JobQueue, destDir, true, "", deps.DB, cfg, deps.Registry)

	status := job.GetStatus()
	if status.Status != worker.JobStatusOrganized {
		t.Fatalf("job status = %q, want organized", status.Status)
	}
	if status.Completed != 3 {
		t.Fatalf("completed count = %d, want 3", status.Completed)
	}
}

func TestProcessUpdateMode_SkipsExcludedAndInvalidResults(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	validPath := filepath.Join(sourceDir, "IPX-333.mp4")
	excludedPath := filepath.Join(sourceDir, "IPX-444.mp4")
	invalidTypePath := filepath.Join(sourceDir, "IPX-555.mp4")
	requireWriteFile(t, validPath)
	requireWriteFile(t, excludedPath)
	requireWriteFile(t, invalidTypePath)

	job := deps.JobQueue.CreateJob([]string{validPath, excludedPath, invalidTypePath})
	job.UpdateFileResult(validPath, &worker.FileResult{
		FilePath: validPath,
		MovieID:  "IPX-333",
		Status:   worker.JobStatusCompleted,
		Data:     &models.Movie{ID: "IPX-333", Title: "Valid Update"},
	})
	job.UpdateFileResult(excludedPath, &worker.FileResult{
		FilePath: excludedPath,
		MovieID:  "IPX-444",
		Status:   worker.JobStatusCompleted,
		Data:     &models.Movie{ID: "IPX-444", Title: "Excluded Update"},
	})
	job.UpdateFileResult(invalidTypePath, &worker.FileResult{
		FilePath: invalidTypePath,
		MovieID:  "IPX-555",
		Status:   worker.JobStatusCompleted,
		Data:     123,
	})
	job.ExcludeFile(excludedPath)

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background())

	status := job.GetStatus()
	if status.Status != worker.JobStatusCompleted {
		t.Fatalf("job status = %q, want completed", status.Status)
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "IPX-333.nfo")); err != nil {
		t.Fatalf("expected NFO for valid file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "IPX-444.nfo")); err == nil {
		t.Fatal("excluded file should not generate an NFO")
	}
}

func TestProcessUpdateMode_RespectsNFOEnabledConfig(t *testing.T) {
	initTestWebSocket(t)

	// Test with NFO disabled
	cfg := config.DefaultConfig()
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false
	cfg.Metadata.NFO.Enabled = false // Explicitly disable NFO generation

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	validPath := filepath.Join(sourceDir, "IPX-666.mp4")
	requireWriteFile(t, validPath)

	job := deps.JobQueue.CreateJob([]string{validPath})
	job.UpdateFileResult(validPath, &worker.FileResult{
		FilePath: validPath,
		MovieID:  "IPX-666",
		Status:   worker.JobStatusCompleted,
		Data:     &models.Movie{ID: "IPX-666", Title: "NFO Disabled Test"},
	})

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background())

	status := job.GetStatus()
	if status.Status != worker.JobStatusCompleted {
		t.Fatalf("job status = %q, want completed", status.Status)
	}

	// Verify NFO was NOT generated because NFO is disabled
	nfoPath := filepath.Join(sourceDir, "IPX-666.nfo")
	if _, err := os.Stat(nfoPath); err == nil {
		t.Fatalf("NFO should NOT be generated when cfg.Metadata.NFO.Enabled = false")
	}
}

func TestProcessOrganizeJob_SkipsNFOWhenDisabled(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false
	cfg.Metadata.NFO.Enabled = false // Explicitly disable NFO generation

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	destDir := t.TempDir()
	videoPath := filepath.Join(sourceDir, "IPX-777.mp4")
	requireWriteFile(t, videoPath)

	job := deps.JobQueue.CreateJob([]string{videoPath})
	job.UpdateFileResult(videoPath, &worker.FileResult{
		FilePath: videoPath,
		MovieID:  "IPX-777",
		Status:   worker.JobStatusCompleted,
		Data:     &models.Movie{ID: "IPX-777", Title: "Organize NFO Disabled"},
	})

	// copyOnly=true copies files to destDir without moving original
	// NFO generation should be skipped since cfg.Metadata.NFO.Enabled = false
	processOrganizeJob(job, deps.JobQueue, destDir, true, "", deps.DB, cfg, deps.Registry)

	status := job.GetStatus()
	if status.Status != worker.JobStatusOrganized {
		t.Fatalf("job status = %q, want organized", status.Status)
	}

	// Verify no NFO files were generated since NFO is disabled
	// Walk the destination directory to check for any .nfo files
	var nfoFiles []string
	err := filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".nfo") {
			nfoFiles = append(nfoFiles, path)
		}
		return nil
	})
	require.NoError(t, err, "failed to walk destination directory")
	assert.Empty(t, nfoFiles, "no NFO files should exist when metadata.nfo.enabled is false")
}

func requireWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
