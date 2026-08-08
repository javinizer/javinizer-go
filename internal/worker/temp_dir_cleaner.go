package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
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
func ClearMissingTempPosters(fs afero.Fs, tempDir, jobID string, results map[string]*resultstore.MovieResult) {
	if tempDir == "" {
		return
	}
	// Collect only results with a cropped URL to check — avoids a ReadDir when
	// nothing needs checking (e.g. jobs whose movies never had a cropped poster).
	toCheck := make([]*resultstore.MovieResult, 0, len(results))
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

// CleanupStaleImageCache removes expired entries from the image cache directory.
// It walks {tempDir}/image-cache/ (shard dirs then files) and removes files whose
// mtime is older than a retention grace of ttl + max(ttl, 24h), so entries that
// crossed their freshness TTL survive the sweep and remain available for the
// stale-if-error fallback. Orphan partial downloads in
// {tempDir}/image-cache/.tmp/ are removed once older than ttl itself.
// No-op when ttl <= 0 or the dir does not exist. Per-file removal failures are
// logged and skipped; the walk continues, and an error is returned only if the
// top-level directory traversal fails.
func CleanupStaleImageCache(fs afero.Fs, tempDir string, ttl time.Duration) (int, error) {
	if ttl <= 0 || fs == nil {
		return 0, nil
	}
	root := filepath.Join(tempDir, "image-cache")
	entries, err := afero.ReadDir(fs, root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read image-cache dir: %w", err)
	}
	retentionCutoff := time.Now().Add(-(ttl + max(ttl, 24*time.Hour)))
	cutoff := time.Now().Add(-ttl)
	removed := 0
	for _, shard := range entries {
		if !shard.IsDir() || shard.Name() == ".tmp" {
			continue
		}
		shardPath := filepath.Join(root, shard.Name())
		files, ferr := afero.ReadDir(fs, shardPath)
		if ferr != nil {
			if os.IsNotExist(ferr) {
				continue
			}
			logging.Warnf("CleanupStaleImageCache: failed to read shard %s: %v", shardPath, ferr)
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if f.ModTime().Before(retentionCutoff) {
				fp := filepath.Join(shardPath, f.Name())
				info, statErr := fs.Stat(fp)
				if statErr != nil || !info.ModTime().Before(retentionCutoff) {
					continue
				}
				if rerr := fsutil.AferoRemoveAll(fs, fp); rerr != nil {
					logging.Warnf("CleanupStaleImageCache: failed to remove %s: %v", fp, rerr)
				} else {
					removed++
				}
			}
		}
		remaining, rerr := afero.ReadDir(fs, shardPath)
		if rerr == nil && len(remaining) == 0 {
			_ = fs.Remove(shardPath)
		}
	}
	tmpDir := filepath.Join(root, ".tmp")
	tmpEntries, terr := afero.ReadDir(fs, tmpDir)
	if terr == nil {
		for _, f := range tmpEntries {
			if f.IsDir() || !f.ModTime().Before(cutoff) {
				continue
			}
			fp := filepath.Join(tmpDir, f.Name())
			info, statErr := fs.Stat(fp)
			if statErr != nil || !info.ModTime().Before(cutoff) {
				continue
			}
			if rerr := fsutil.AferoRemoveAll(fs, fp); rerr != nil {
				logging.Warnf("CleanupStaleImageCache: failed to remove temp %s: %v", fp, rerr)
			} else {
				removed++
			}
		}
	}
	return removed, nil
}

// EvictImageCacheToSize removes oldest-first entries from {tempDir}/image-cache/
// until the total size of shard entries is within limitBytes. The .tmp staging
// directory is never counted or evicted (in-flight writes live there). Per-file
// failures are logged and the sweep continues. Returns the pre-eviction total and
// the number of entries removed.
func EvictImageCacheToSize(fs afero.Fs, tempDir string, limitBytes int64, keep ...string) (int64, int, error) {
	if limitBytes <= 0 || fs == nil {
		return 0, 0, nil
	}
	root := filepath.Join(tempDir, "image-cache")
	entries, err := afero.ReadDir(fs, root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("read image-cache dir: %w", err)
	}
	type artifact struct {
		path  string
		mtime time.Time
		size  int64
	}
	var artifacts []artifact
	var total int64
	for _, shard := range entries {
		if !shard.IsDir() || shard.Name() == ".tmp" {
			continue
		}
		files, ferr := afero.ReadDir(fs, filepath.Join(root, shard.Name()))
		if ferr != nil {
			if os.IsNotExist(ferr) {
				continue
			}
			logging.Warnf("EvictImageCacheToSize: failed to read shard %s: %v", shard.Name(), ferr)
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			artifacts = append(artifacts, artifact{path: filepath.Join(root, shard.Name(), f.Name()), mtime: f.ModTime(), size: f.Size()})
			total += f.Size()
		}
	}
	if total <= limitBytes {
		return total, 0, nil
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].mtime.Before(artifacts[j].mtime) })
	keepSet := make(map[string]struct{}, len(keep))
	for _, k := range keep {
		keepSet[k] = struct{}{}
	}
	over := total - limitBytes
	var freed int64
	removed := 0
	for _, a := range artifacts {
		if freed >= over {
			break
		}
		if _, protected := keepSet[a.path]; protected {
			continue
		}
		if rerr := fsutil.AferoRemoveAll(fs, a.path); rerr != nil {
			logging.Warnf("EvictImageCacheToSize: failed to remove %s: %v", a.path, rerr)
			continue
		}
		freed += a.size
		removed++
	}
	return total, removed, nil
}
