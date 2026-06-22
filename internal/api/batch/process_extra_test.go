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

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	destDir := t.TempDir()

	excludedPath := filepath.Join(sourceDir, "IPX-111.mp4")
	invalidTypePath := filepath.Join(sourceDir, "IPX-222.mp4")
	unmatchedPath := filepath.Join(sourceDir, "mystery_file.mp4")
	requireWriteFile(t, excludedPath)
	requireWriteFile(t, invalidTypePath)
	requireWriteFile(t, unmatchedPath)

	job := deps.JobStore.CreateJobBatch([]string{excludedPath, invalidTypePath, unmatchedPath})
	setJobResult(job, excludedPath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: excludedPath, MovieID: "IPX-111"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-111", Title: "Excluded"},
	})
	// Movie: nil means "no movie data available" — skipped gracefully by organize (not failed)
	setJobResult(job, invalidTypePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: invalidTypePath, MovieID: "IPX-222"},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})
	// A Movie with UNKNOWN ID still has a valid Movie struct, so organize will attempt it.
	// With copyOnly=true, the file is copied successfully → organized.
	setJobResult(job, unmatchedPath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: unmatchedPath, MovieID: "UNKNOWN"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "UNKNOWN", Title: "No Match"},
	})
	excludeFile(job, excludedPath)

	testStartOrganizeApply(context.Background(), job, deps.JobStore, destDir, true, "", false, false, deps.CoreDeps.DB, cfg, deps.CoreDeps.ScraperRegistry, nil)

	status := job.GetStatus()
	// With typed Movie field: nil Movie is skipped (not failed), valid Movie structs get organized.
	// unmatchedPath organizes successfully (copyOnly=true), excludedPath is skipped, invalidTypePath is skipped.
	// Result: organized=1, failed=0 → MarkOrganized()
	if status.Status != models.JobStatusOrganized {
		t.Fatalf("job status = %q, want organized (1 file organized, 0 failed)", status.Status)
	}
}

func TestProcessUpdateMode_SkipsExcludedAndInvalidResults(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	validPath := filepath.Join(sourceDir, "IPX-333.mp4")
	excludedPath := filepath.Join(sourceDir, "IPX-444.mp4")
	invalidTypePath := filepath.Join(sourceDir, "IPX-555.mp4")
	requireWriteFile(t, validPath)
	requireWriteFile(t, excludedPath)
	requireWriteFile(t, invalidTypePath)

	job := deps.JobStore.CreateJobBatch([]string{validPath, excludedPath, invalidTypePath})
	setJobResult(job, validPath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: validPath, MovieID: "IPX-333"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-333", Title: "Valid Update"},
	})
	setJobResult(job, excludedPath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: excludedPath, MovieID: "IPX-444"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-444", Title: "Excluded Update"},
	})
	setJobResult(job, invalidTypePath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: invalidTypePath, MovieID: "IPX-555"},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})
	excludeFile(job, excludedPath)

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	if status.Status != models.JobStatusCompleted {
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
	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Feature.Enabled = false // Explicitly disable NFO generation

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	validPath := filepath.Join(sourceDir, "IPX-666.mp4")
	requireWriteFile(t, validPath)

	job := deps.JobStore.CreateJobBatch([]string{validPath})
	setJobResult(job, validPath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: validPath, MovieID: "IPX-666"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-666", Title: "NFO Disabled Test"},
	})

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	if status.Status != models.JobStatusCompleted {
		t.Fatalf("job status = %q, want completed", status.Status)
	}

	// Verify NFO was NOT generated because NFO is disabled
	nfoPath := filepath.Join(sourceDir, "IPX-666.nfo")
	if _, err := os.Stat(nfoPath); err == nil {
		t.Fatalf("NFO should NOT be generated when cfg.Metadata.NFO.Feature.Enabled = false")
	}
}

func TestProcessOrganizeJob_SkipsNFOWhenDisabled(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	cfg.Metadata.NFO.Feature.Enabled = false // Explicitly disable NFO generation

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	destDir := t.TempDir()
	videoPath := filepath.Join(sourceDir, "IPX-777.mp4")
	requireWriteFile(t, videoPath)

	job := deps.JobStore.CreateJobBatch([]string{videoPath})
	setJobResult(job, videoPath, &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: videoPath, MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777", Title: "Organize NFO Disabled"},
	})

	// copyOnly=true copies files to destDir without moving original
	// NFO generation should be skipped since cfg.Metadata.NFO.Feature.Enabled = false
	testStartOrganizeApply(context.Background(), job, deps.JobStore, destDir, true, "", false, false, deps.CoreDeps.DB, cfg, deps.CoreDeps.ScraperRegistry, nil)

	status := job.GetStatus()
	if status.Status != models.JobStatusOrganized {
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
