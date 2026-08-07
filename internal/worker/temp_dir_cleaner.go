package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// rekeyWitness is the recovery record for a whole-movie rekey's poster-pair
// relocation (POSTER-WRITE-HARDENING codex r40 P2): the rekey path renames
// the pair BEFORE the state commit (a failed relocation must leave no DB
// mutation, and a failed commit rolls the renames back), so a crash in that
// window leaves files at the NEW identity while the durable row still
// references the old one. The witness names both identities.
const rekeyWitnessPrefix = ".rekey-"

// promoteWitness is the recovery record for a staged poster promotion that
// crashed post-promote/pre-commit (codex r48 P2) — canonical holds
// uncommitted NEW bytes, the pre-promotion pair is parked as <name>.bak,
// and the durable row still holds the OLD poster source.
//
// Wire contract with internal/api/batch (movie_edit_poster_pair.go); the
// filename encodes nothing trusted — PosterID comes from the content.
const promoteWitnessPrefix = ".promote-"

type promoteWitness struct {
	PosterID string `json:"poster_id"`
	URL      string `json:"url"`
}

type rekeyWitness struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
}

// ReconcileRekeyWitnesses repairs relocation witnesses left behind by a crash
// (or a partially-failed rollback). For each witness the DURABLE job row is
// the arbiter: when the committed results already reference the new ID the
// commit landed and only the leftover witness is swept; otherwise the pair
// files still at the NEW name are renamed back to the OLD identity so the
// stored poster URLs resolve again.
func (c *TempDirCleaner) ReconcileRekeyWitnesses(ctx context.Context) (int, error) {
	if c.fs == nil || c.jobRepo == nil {
		return 0, nil
	}
	postersDir := filepath.Join(c.tempDir, "posters")
	jobDirs, err := afero.ReadDir(c.fs, postersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read posters dir for rekey reconcile: %w", err)
	}
	reversed := 0
	for _, je := range jobDirs {
		if !je.IsDir() {
			continue
		}
		jobID := je.Name()
		dir := filepath.Join(postersDir, jobID)
		entries, rerr := afero.ReadDir(c.fs, dir)
		if rerr != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			isRekey := strings.HasPrefix(name, rekeyWitnessPrefix) && strings.HasSuffix(name, ".json")
			isPromote := strings.HasPrefix(name, promoteWitnessPrefix) && strings.HasSuffix(name, ".json")
			if !isRekey && !isPromote {
				continue
			}
			if isPromote {
				reversed += c.reconcilePromoteWitness(ctx, dir, jobID, filepath.Join(dir, name))
				continue
			}
			wpath := filepath.Join(dir, name)
			data, rdErr := afero.ReadFile(c.fs, wpath)
			var w rekeyWitness
			if rdErr != nil || json.Unmarshal(data, &w) != nil || w.OldID == "" || w.NewID == "" {
				logging.Warnf("rekey witness %s unreadable/corrupt — left in place", wpath)
				continue
			}
			job, jerr := c.jobRepo.FindByID(ctx, jobID)
			switch {
			case errors.Is(jerr, database.ErrNotFound):
				// Orphaned directory — the staleness sweep owns whole-dir removal.
				continue
			case jerr != nil:
				logging.Warnf("rekey reconcile: job %s lookup failed: %v", jobID, jerr)
				continue
			}
			committed := false
			var results map[string]*resultstore.MovieResult
			if job != nil && job.ParseResults(&results) == nil {
				for _, r := range results {
					// codex r44 P2: EXACT compare — a case-only rekey crash on a
					// case-sensitive fs leaves the row at the OLD spelling; fold-
					// equal matching would misread that as "committed" and sweep
					// the witness without reversing.
					if r != nil && r.Movie != nil && r.Movie.ID == w.NewID {
						committed = true
						break
					}
				}
			}
			if !committed {
				// codex r41 P2b: the witness is the ONLY recovery marker for a
				// mid-relocation crash — sweep it only after every required
				// reverse rename SUCCEEDED; a transient Stat/Rename failure
				// keeps it for the next startup.
				reversalClean := true
				for _, sfx := range []string{"-full.jpg", ".jpg"} {
					newPath := filepath.Join(dir, w.NewID+sfx)
					oldPath := filepath.Join(dir, w.OldID+sfx)
					if _, err := c.fs.Stat(newPath); err != nil {
						if !os.IsNotExist(err) {
							reversalClean = false
							logging.Warnf("rekey reconcile stat %s: %v", newPath, err)
						}
						continue
					}
					if _, err := c.fs.Stat(oldPath); err == nil {
						continue // old bytes still there — nothing to reverse
					} else if !os.IsNotExist(err) {
						reversalClean = false
						logging.Warnf("rekey reconcile stat %s: %v", oldPath, err)
						continue
					}
					if rnErr := c.fs.Rename(newPath, oldPath); rnErr != nil {
						reversalClean = false
						logging.Warnf("rekey reconcile rename back %s→%s: %v", newPath, oldPath, rnErr)
						continue
					}
					reversed++
				}
				if !reversalClean {
					continue // witness preserved for the next startup retry
				}
			}
			if rmErr := c.fs.Remove(wpath); rmErr != nil && !os.IsNotExist(rmErr) {
				logging.Warnf("rekey witness sweep %s: %v", wpath, rmErr)
			}
		}
	}
	return reversed, nil
}

// reconcilePromoteWitness arbitrates one .promote- witness against the
// durable job row: the commit landed when any stored result's poster source
// URL equals the witnessed URL (only the witness is swept then). Otherwise
// the promote was post-commit-crash: drop the uncommitted canonical bytes
// and restore the parked .bak pair. The witness survives unless every needed
// reversal succeeded (r48 mirror of the rekey rule).
func (c *TempDirCleaner) reconcilePromoteWitness(ctx context.Context, dir, jobID, wpath string) int {
	data, err := afero.ReadFile(c.fs, wpath)
	var w promoteWitness
	if err != nil || json.Unmarshal(data, &w) != nil || w.PosterID == "" || w.URL == "" {
		logging.Warnf("promote witness %s unreadable/corrupt — left in place", wpath)
		return 0
	}
	job, jerr := c.jobRepo.FindByID(ctx, jobID)
	switch {
	case errors.Is(jerr, database.ErrNotFound):
		return 0 // orphaned dir: the staleness sweep owns removal
	case jerr != nil:
		logging.Warnf("promote reconcile: job %s lookup failed: %v", jobID, jerr)
		return 0
	}
	committed := false
	var results map[string]*resultstore.MovieResult
	if job != nil && job.ParseResults(&results) == nil {
		for _, r := range results {
			if r != nil && r.Movie != nil && r.Movie.Poster.PosterURL == w.URL {
				committed = true
				break
			}
		}
	}
	reversed := 0
	if !committed {
		clean := true
		for _, sfx := range []string{"-full.jpg", ".jpg"} {
			canon := filepath.Join(dir, w.PosterID+sfx)
			bak := canon + ".bak"
			if _, statErr := c.fs.Stat(canon); statErr == nil {
				if rmErr := c.fs.Remove(canon); rmErr != nil {
					clean = false
					logging.Warnf("promote reconcile: drop uncommitted %s: %v", canon, rmErr)
					continue
				}
			} else if !os.IsNotExist(statErr) {
				clean = false
				logging.Warnf("promote reconcile: stat %s: %v", canon, statErr)
				continue
			}
			if _, statErr := c.fs.Stat(bak); statErr == nil {
				if rnErr := c.fs.Rename(bak, canon); rnErr != nil {
					clean = false
					logging.Warnf("promote reconcile: restore %s→%s: %v", bak, canon, rnErr)
				} else {
					reversed++
				}
			} else if !os.IsNotExist(statErr) {
				clean = false
				logging.Warnf("promote reconcile: stat %s: %v", bak, statErr)
			}
		}
		if !clean {
			return reversed // witness survives for the next startup retry
		}
	}
	if rmErr := c.fs.Remove(wpath); rmErr != nil && !os.IsNotExist(rmErr) {
		logging.Warnf("promote witness sweep %s: %v", wpath, rmErr)
	}
	return reversed
}

// StartStaleTempCleanup starts a background goroutine that periodically cleans
// up stale temp poster directories. Returns a stop channel that should be closed
// on shutdown to stop the cleanup loop.
func (c *TempDirCleaner) StartStaleTempCleanup() chan struct{} {
	stop := make(chan struct{})
	go func() {
		// Run immediately on startup: rekey-witness reconciliation FIRST (a
		// crash-mid-relocation must be reversed BEFORE the staleness sweep
		// could consider the directory), then the stale sweep itself.
		if n, err := c.ReconcileRekeyWitnesses(context.Background()); err != nil {
			logging.Warnf("Rekey witness reconciliation failed on startup: %v", err)
		} else if n > 0 {
			logging.Infof("Reversed %d orphaned poster rekey relocation(s)", n)
		}
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
