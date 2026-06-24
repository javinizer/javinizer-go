package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/spf13/afero"
)

// TempDirCleaner owns the cleanup of stale temp poster directories.
// Per P-8: extracted from JobStore so that temp dir cleanup is a single
// responsibility with its own dependencies (fs, tempDir, jobRepo), rather
// than being embedded in the 591-line JobStore.
type TempDirCleaner struct {
	fs      afero.Fs
	tempDir string
	jobRepo database.JobRepositoryInterface
}

// NewTempDirCleaner creates a TempDirCleaner with the minimum required dependencies.
func NewTempDirCleaner(fs afero.Fs, tempDir string, jobRepo database.JobRepositoryInterface) *TempDirCleaner {
	return &TempDirCleaner{
		fs:      fs,
		tempDir: tempDir,
		jobRepo: jobRepo,
	}
}

// CleanupStaleTempDirs removes temp poster directories for jobs that are either:
//   - In a terminal state (Organized/Failed/Cancelled/Reverted/Completed) and have been so for >24 hours
//   - Orphaned (the job ID no longer exists in the database)
//
// Returns the count of removed directories. This prevents unbounded disk growth
// from temp poster files that are only cleaned up on explicit DeleteJob calls.
func (c *TempDirCleaner) CleanupStaleTempDirs(ctx context.Context) (int, error) {
	if c.fs == nil {
		return 0, nil
	}

	postersDir := filepath.Join(c.tempDir, "posters")

	// List subdirectories under data/temp/posters/
	entries, err := afero.ReadDir(c.fs, postersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // directory doesn't exist yet — nothing to clean
		}
		return 0, fmt.Errorf("read temp posters dir: %w", err)
	}
	if len(entries) == 0 {
		return 0, nil
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	removed := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		jobID := entry.Name()

		shouldRemove := false

		if c.jobRepo != nil {
			job, err := c.jobRepo.FindByID(ctx, jobID)
			if err != nil {
				if errors.Is(err, database.ErrNotFound) {
					// Job no longer in database — orphaned directory, safe to remove.
					shouldRemove = true
				} else {
					// Transient DB error — do NOT delete; retry on next cycle.
					logging.Warnf("CleanupStaleTempDirs: lookup failed for job %s: %v", jobID, err)
					continue
				}
			} else if job == nil {
				// Defensive: the canonical JobRepository (BaseRepository.FindByID)
				// never returns (nil, nil), but guard against alternative
				// JobRepositoryInterface implementations that might.
				shouldRemove = true
			} else if isPastActiveStatus(job.Status) {
				// Past-active state — check if it's been inactive for >24h
				terminalTime := latestInactiveTime(job)
				if terminalTime != nil && terminalTime.Before(cutoff) {
					shouldRemove = true
				}
			}
		} else {
			// No job repo available — clean up directories older than 24h as a heuristic
			if entry.ModTime().Before(cutoff) {
				shouldRemove = true
			}
		}

		if shouldRemove {
			dirPath := filepath.Join(postersDir, jobID)
			if err := fsutil.AferoRemoveAll(c.fs, dirPath); err != nil {
				logging.Warnf("CleanupStaleTempDirs: failed to remove %s: %v", dirPath, err)
			} else {
				removed++
				logging.Debugf("CleanupStaleTempDirs: removed stale temp dir for job %s", jobID)
			}
		}
	}

	return removed, nil
}

// CleanJobTempDir removes the temp poster directory for the given job ID.
// Best-effort: errors are logged but not returned. Validates the job ID
// to prevent path traversal. Per S-9: extracted from DeleteJob so that
// cleanup logic is a single responsibility on TempDirCleaner.
func (c *TempDirCleaner) CleanJobTempDir(id string) {
	if err := poster.ValidateJobID(id); err != nil {
		logging.Warnf("DeleteJob: refusing to clean temp poster dir with invalid job ID: %v", err)
		return
	}
	tempPosterDir := filepath.Join(c.tempDir, "posters", id)
	if c.fs != nil {
		if err := fsutil.AferoRemoveAll(c.fs, tempPosterDir); err != nil {
			logging.Warnf("Failed to clean up temp posters for job %s: %v", id, err)
		} else {
			logging.Debugf("[Job %s] Cleaned up temporary poster directory: %s", id, tempPosterDir)
		}
	}
}

// ClearMissingTempPosters clears CroppedPosterURL on each result movie whose
// cropped temp poster file no longer exists on disk.
//
// This keeps API responses consistent across the detail view (reconstructBatchJob)
// and the list view (parseAndConvertJobResults): when the local temp artifact is
// gone — e.g. after upgrading from a version whose temp dir was not preserved, or
// after manual temp-dir deletion — the stale URL is dropped so the frontend falls
// back to the remote poster_url instead of rendering a broken image. It does NOT
// delete anything; directory removal is the responsibility of CleanJobTempDir on
// explicit job deletion.
//
// No-op when tempDir is empty or no result has a cropped URL to check. A nil fs
// falls back to the real filesystem.
//
// Uses a single directory read instead of one Stat per result: the list endpoint
// may load dozens of jobs × many results per request, so batching avoids an N×M
// syscall fan-out. If the poster directory does not exist, every cropped URL is
// cleared; any other read error (permission, I/O) leaves URLs intact. Membership
// is checked by entry NAME (movieID+".jpg"), so a dangling symlink would count
// as present — acceptable because the poster generator always writes regular
// files; the only behavioral difference from the prior per-file os.IsNotExist
// check is that theoretical symlink edge case, which does not occur in practice.
func ClearMissingTempPosters(fs afero.Fs, tempDir, jobID string, results map[string]*MovieResult) {
	if tempDir == "" {
		return
	}
	// Collect only results with a cropped URL to check — avoids a ReadDir when
	// nothing needs checking (e.g. jobs whose movies never had a cropped poster).
	toCheck := make([]*MovieResult, 0, len(results))
	for _, result := range results {
		if result != nil && result.Movie != nil && result.Movie.Poster.CroppedPosterURL != "" {
			toCheck = append(toCheck, result)
		}
	}
	if len(toCheck) == 0 {
		return
	}
	if fs == nil {
		fs = afero.NewOsFs()
	}
	posterDir := filepath.Join(tempDir, "posters", jobID)

	entries, err := afero.ReadDir(fs, posterDir)
	if err != nil {
		if os.IsNotExist(err) {
			for _, result := range toCheck {
				result.Movie.Poster.CroppedPosterURL = ""
				logging.Debugf("[Job %s] Cleared missing temp poster URL for %s (no poster dir)", jobID, result.Movie.ID)
			}
		}
		return
	}

	existing := make(map[string]bool, len(entries))
	for _, e := range entries {
		existing[e.Name()] = true
	}
	for _, result := range toCheck {
		if !existing[result.Movie.ID+".jpg"] {
			result.Movie.Poster.CroppedPosterURL = ""
			logging.Debugf("[Job %s] Cleared missing temp poster URL for %s", jobID, result.Movie.ID)
		}
	}
}

// StartStaleTempCleanup starts a background goroutine that periodically cleans
// up stale temp poster directories. Returns a stop channel that should be closed
// on shutdown to stop the cleanup loop.
func (c *TempDirCleaner) StartStaleTempCleanup() chan struct{} {
	stop := make(chan struct{})
	go func() {
		// Run immediately on startup
		if removed, err := c.CleanupStaleTempDirs(context.Background()); err != nil {
			logging.Warnf("Stale temp cleanup failed on startup: %v", err)
		} else if removed > 0 {
			logging.Infof("Cleaned up %d stale temp poster director(ies) on startup", removed)
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if removed, err := c.CleanupStaleTempDirs(context.Background()); err != nil {
					logging.Warnf("Stale temp cleanup failed: %v", err)
				} else if removed > 0 {
					logging.Infof("Cleaned up %d stale temp poster director(ies)", removed)
				}
			case <-stop:
				return
			}
		}
	}()
	return stop
}
