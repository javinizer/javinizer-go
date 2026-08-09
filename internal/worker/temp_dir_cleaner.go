package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
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
	PosterID     string            `json:"poster_id"`
	URL          string            `json:"url"`
	ResultID     string            `json:"result_id"`
	PrevRevision uint64            `json:"prev_revision"`
	OldSHA       map[string]string `json:"old_sha,omitempty"`
}

// cropWitness is the crash-recovery record for the staged manual-crop flow
// (codex r51 P2/durability): staged crop bytes never touch canonical until
// the state commit lands.
const cropWitnessPrefix = ".crop-"

type cropWitness struct {
	PosterID     string `json:"poster_id"`
	ResultID     string `json:"result_id"`
	StageID      string `json:"stage_id"`
	CroppedURL   string `json:"cropped_url"`
	PrevRevision uint64 `json:"prev_revision"`
}

type rekeyWitness struct {
	OldID        string `json:"old_id"`
	NewID        string `json:"new_id"`
	PrevRevision uint64 `json:"prev_revision"`
	// ResultID pins the transitioning result so arbitration can scope the
	// OLD-ID presence gate to THIS family (audit F-R7-1): a sibling family
	// legitimately sharing the canonical Movie.ID must not flip "committed"
	// detection to false forever. Empty ⇒ legacy global scan.
	ResultID string `json:"result_id,omitempty"`
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
			isCrop := strings.HasPrefix(name, cropWitnessPrefix) && strings.HasSuffix(name, ".json")
			if !isRekey && !isPromote && !isCrop {
				continue
			}
			if isPromote {
				reversed += c.reconcilePromoteWitness(ctx, dir, jobID, filepath.Join(dir, name))
				continue
			}
			if isCrop {
				reversed += c.reconcileCropWitness(ctx, dir, jobID, filepath.Join(dir, name))
				continue
			}
			wpath := filepath.Join(dir, name)
			data, rdErr := afero.ReadFile(c.fs, wpath)
			var w rekeyWitness
			if rdErr != nil || json.Unmarshal(data, &w) != nil || w.OldID == "" || w.NewID == "" {
				logging.Warnf("rekey witness %s unreadable/corrupt — left in place", wpath)
				continue
			}
			if !witnessLegBasename(w.OldID) || !witnessLegBasename(w.NewID) {
				logging.Warnf("rekey witness %s carries unsafe id fields — left in place", wpath)
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
			oldIDPresent := false
			newIDCommitted := false
			var results map[string]*resultstore.MovieResult
			if job != nil {
				if res, ok := arbitrateResults(job); ok {
					results = res
				} else {
					continue
				}
				for _, r := range results {
					// codex r52 P2: TWO-GATE committed detection. (1) No result
					// still carries the OLD spelling — the rekey transitions the
					// whole family, so any surviving OldID means the commit never
					// landed. (2) At least one result carries the NEW spelling
					// with a revision the rekey actually bumped — a case-folded
					// sibling already at the new spelling has its own (lower)
					// revision and must not misfire. Together these scope the match
					// to THIS rekey's transition, not any result that happens to
					// share the new ID.
					if r == nil || r.Movie == nil {
						continue
					}
					// audit F-R7-1: a pinned ResultID scopes the committed gates
					// to the TRANSITIONING result — a sibling family legitimately
					// sharing the canonical Movie.ID must not flip the OLD
					// presence gate to false forever. Legacy witnesses (empty
					// ResultID) keep the global scan.
					if w.ResultID != "" && r.ResultID != w.ResultID {
						continue
					}
					if r.Movie.ID == w.OldID {
						oldIDPresent = true
					}
					if r.Movie.ID == w.NewID && r.Revision > w.PrevRevision {
						newIDCommitted = true
					}
				}
			}
			committed := !oldIDPresent && newIDCommitted
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
		// audit F-R5-2: parked-backup re-homing runs AFTER witness arbitration —
		// never before: the sweep's canonical-present litter-drop would otherwise
		// destroy bytes and markers a pending witness still needs.
		reversed += c.reconcileParkedPosterBackups(dir)
	}
	return reversed, nil
}

// arbitrateResults decodes the persisted job row through the persistence
// codec (codex P1): production rows are jobpersist ENVELOPES
// ({"domain": ..., "provenance": ...}) — a raw ParseResults would "parse" the
// envelope keys into zero-valued entries, misclassifying EVERY production
// witness as uncommitted and reversing committed rekeys/promotions/crops.
// Decode handles the legacy result formats transparently.
func arbitrateResults(job *models.Job) (map[string]*resultstore.MovieResult, bool) {
	snap, errs := jobpersist.Decode(job)
	if len(errs) > 0 {
		// audit F3: an undecodable Results column must NOT arbitrate — Decode
		// returns an EMPTY map on failure, which would run every witness's
		// uncommitted/reversal arm against possibly-committed state. Keep the
		// witness and defer to manual repair.
		logging.Warnf("witness arbitration skipped for job %s: results decode failed: %v", job.ID, errs)
		return nil, false
	}
	return snap.Results, true
}

// hexLowerHexTail reports whether a marker name ends in ".<lowhex>.<lowhex>"
// — the anchored shape every in-flight marker carries (audit F-R20-2).
// witnessLegBasename gates every witness-carried ID before it enters a
// filepath.Join: a hostile or corrupt witness must never steer reconcile-time
// Rename/Remove outside the job poster dir (local codex review P1).
func witnessLegBasename(s string) bool {
	return s != "" && s != "." && s != ".." && filepath.Base(s) == s &&
		!strings.ContainsAny(s, "/\\")
}

func hexLowerHexTail(s string) bool {
	i1 := strings.LastIndexByte(s, '.')
	if i1 < 2 || i1 == len(s)-1 {
		return false
	}
	i0 := strings.LastIndexByte(s[:i1], '.')
	if i0 < 1 {
		return false
	}
	return isHexLowerRun(s[i0+1:i1]) && isHexLowerRun(s[i1+1:])
}

func isHexLowerRun(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// markerAnchored is true iff name carries the inflight sentinel's anchored
// shape: ".inflight-" + escaped ID + "." + <lowhex>.<lowhex> nonce.
func markerAnchored(name string) bool {
	return strings.HasPrefix(name, ".inflight-") && hexLowerHexTail(name)
}

// reconcileParkedPosterBackups re-homes .rsbak.*/.dlbak legs a crash or
// panic stranded (audit F-R4-5): parked bytes are the ONLY copy of the
// committed pair in that window. Runs AFTER witness arbitration (F-R5-2) and
// skips any poster with an unresolved witness — the arbitrators own those
// bytes. Parses are anchored: dotted ids containing ".dlbak"/".rsbak." are
// never reclassified (F-R5-3), nor are witness FILES ever.
func (c *TempDirCleaner) reconcileParkedPosterBackups(dir string) int {
	entries, err := afero.ReadDir(c.fs, dir)
	if err != nil {
		return 0
	}
	// Witness belt (F-R5-2): unresolved witness ⇒ arbitrators own the poster.
	witnessed := map[string]struct{}{}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, promoteWitnessPrefix) && strings.HasSuffix(name, ".json"):
			base := strings.TrimSuffix(strings.TrimPrefix(name, promoteWitnessPrefix), ".json")
			if id, uerr := url.PathUnescape(base); uerr == nil {
				witnessed[id] = struct{}{}
			} else {
				witnessed[base] = struct{}{}
			}
		case strings.HasPrefix(name, rekeyWitnessPrefix) && strings.HasSuffix(name, ".json"):
			// audit F-R6-1: rekey witnesses are CONTENT-matched at BOTH legs —
			// a filename-OLD-only belt would let a parked NEW-side leg be
			// swept/restored under an unresolved relocation. Corrupt content
			// falls back to the filename-derived OLD id (legacy parity).
			rawBase := strings.TrimSuffix(strings.TrimPrefix(name, rekeyWitnessPrefix), ".json")
			witnessed[rawBase] = struct{}{}
			if data, rerr := afero.ReadFile(c.fs, filepath.Join(dir, name)); rerr == nil {
				var w rekeyWitness
				if json.Unmarshal(data, &w) == nil {
					if w.OldID != "" {
						witnessed[w.OldID] = struct{}{}
					}
					if w.NewID != "" {
						witnessed[w.NewID] = struct{}{}
					}
				}
			}
		case strings.HasPrefix(name, cropWitnessPrefix) && strings.HasSuffix(name, ".json"):
			data, rerr := afero.ReadFile(c.fs, filepath.Join(dir, name))
			if rerr != nil {
				continue
			}
			var cw cropWitness
			if json.Unmarshal(data, &cw) == nil && cw.PosterID != "" {
				witnessed[cw.PosterID] = struct{}{}
			}
		}
	}
	healed := 0
	for _, e := range entries {
		name := e.Name()
		var canon string
		// audit F-R20-2 + F-R21-1: the marker branch runs FIRST — its
		// anchor therefore requires that the name does NOT ALSO parse as a
		// parked leg (whose ".rsbak."+nonce tail is subshape-compatible).
		// Otherwise a leading-dot ID's parked bytes get eaten as "markers".
		// audit F-R21-1 + F-R22-1: the marker branch's exclusion must parse,
		// not substring: a name whose .rsbak. tail is hex.hex is a parked leg
		// (re-home, never sweep); a marker whose ID CONTAINS ".rsbak." has a
		// 3+-segment tail → parked-parse rejects → marker branch fires.
		parkedParse := false
		if ridx := strings.LastIndex(name, ".rsbak."); ridx >= 0 {
			parkedParse = isBackupNonce(name[ridx+len(".rsbak."):])
		}
		if markerAnchored(name) && !parkedParse {
			// audit F-R19-1 aftermath: stranded in-flight markers (crash) — a
			// restarted process has no live generation windows; delete.
			if rmErr := c.fs.Remove(filepath.Join(dir, name)); rmErr != nil {
				logging.Warnf("in-flight marker sweep %s: %v", name, rmErr)
				continue
			}
			healed++
			continue
		}
		switch {
		case strings.HasPrefix(name, promoteWitnessPrefix) || strings.HasPrefix(name, rekeyWitnessPrefix) || strings.HasPrefix(name, cropWitnessPrefix):
			continue // witness files are never parked backups (F-R5-3 belt)
		case strings.HasSuffix(name, ".dlbak"):
			canon = strings.TrimSuffix(name, ".dlbak") // legacy + manager park
		default:
			// nonce-anchored rescrape park: <id>.jpg.rsbak.<hex>.<hex> — the
			// LAST hit with a hex.hex tail, so ids containing ".rsbak." don't
			// misparse (F-R5-3).
			idx := strings.LastIndex(name, ".rsbak.")
			if idx < 0 || !isBackupNonce(name[idx+len(".rsbak."):]) {
				continue
			}
			canon = name[:idx]
		}
		base := canon
		switch {
		case strings.HasSuffix(base, "-full.jpg"):
			base = strings.TrimSuffix(base, "-full.jpg")
		default:
			base = strings.TrimSuffix(base, ".jpg")
		}
		if _, fenced := witnessed[base]; fenced {
			continue // arbitrators own this poster (F-R5-2)
		}
		parked := filepath.Join(dir, name)
		canonPath := filepath.Join(dir, canon)
		if _, statErr := c.fs.Stat(canonPath); statErr == nil {
			if rmErr := c.fs.Remove(parked); rmErr != nil {
				logging.Warnf("parked backup sweep %s: %v", parked, rmErr)
				continue
			}
			healed++
			continue
		} else if !errors.Is(statErr, afero.ErrFileNotFound) {
			// codex P2: a transient canonical stat error must NOT fall through
			// to the rename — the possibly-existing newer bytes would be
			// replaced by the stale parked copy.
			logging.Warnf("parked backup sweep %s: canonical indeterminate (%v) — kept both", parked, statErr)
			continue
		}
		if rnErr := c.fs.Rename(parked, canonPath); rnErr != nil {
			logging.Warnf("parked backup restore %s→%s: %v", parked, canonPath, rnErr)
			continue
		}
		healed++
	}
	return healed
}

// isBackupNonce matches the "<hex>.<hex>" park-path tail, preventing dotted
// poster ids from misclassifying as backups (audit F-R5-3).
func isBackupNonce(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, p := range parts {
		for _, ch := range p {
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
				return false
			}
		}
	}
	return true
}

// shaContentHex hashes a poster leg for witness arbitration.
func shaContentHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
	if !witnessLegBasename(w.PosterID) {
		logging.Warnf("promote witness %s carries an unsafe poster id — left in place", wpath)
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
	// r48-followup P2a: arbitration is TARGET-scoped — the match is the row
	// where result ID, canonical poster ID AND the witnessed URL all agree.
	// Global URL matching misfires when another family shares the URL, or
	// when the target re-downloads from its existing URL (row carried it
	// pre-op).
	// r49 P2a: commit detection needs a token that CANNOT be present
	// pre-commit — a same-URL refresh row (and any later-sibling edit) reads
	// identical by identity+URL. Every commit bumps the per-result Revision,
	// so committed means the TARGET row's revision moved past the captured
	// one.
	committed := false
	var results map[string]*resultstore.MovieResult
	if job != nil {
		if res, ok := arbitrateResults(job); ok {
			results = res
		} else {
			return 0
		}
		for _, r := range results {
			if r != nil && r.ResultID == w.ResultID && r.Movie != nil &&
				strings.EqualFold(r.Movie.ID, w.PosterID) && r.Movie.Poster.PosterURL == w.URL &&
				r.Revision > w.PrevRevision {
				committed = true
				break
			}
		}
	}
	reversed := 0
	if !committed {
		clean := true
		// r49 P2b: per-leg arbitration is by CONTENT HASH against the
		// witness's pre-op snapshots — an already-restored canon hashes equal
		// and survives even when its .bak was consumed by an earlier startup;
		// mismatching canon bytes are uncommitted and dropped before restoring.
		for _, leg := range []struct{ key, sfx string }{{"full", "-full.jpg"}, {"crop", ".jpg"}} {
			canon := filepath.Join(dir, w.PosterID+leg.sfx)
			bak := canon + ".bak"
			oldSHA := w.OldSHA[leg.key]
			_, canonErr := c.fs.Stat(canon)
			_, bakErr := c.fs.Stat(bak)
			switch {
			case canonErr != nil && !os.IsNotExist(canonErr):
				clean = false
				logging.Warnf("promote reconcile: stat %s: %v", canon, canonErr)
				continue
			case bakErr != nil && !os.IsNotExist(bakErr):
				clean = false
				logging.Warnf("promote reconcile: read %s: %v", bak, bakErr)
				continue
			}
			bakExists := bakErr == nil
			canonExists := canonErr == nil
			if bakExists {
				if canonExists {
					data, rdErr := afero.ReadFile(c.fs, canon)
					if rdErr != nil {
						clean = false
						logging.Warnf("promote reconcile: read %s: %v", canon, rdErr)
						continue
					}
					if shaContentHex(data) != oldSHA {
						if rmErr := c.fs.Remove(canon); rmErr != nil {
							clean = false
							logging.Warnf("promote reconcile: drop uncommitted %s: %v", canon, rmErr)
							continue
						}
					}
				}
				if rnErr := c.fs.Rename(bak, canon); rnErr != nil {
					clean = false
					logging.Warnf("promote reconcile: restore %s→%s: %v", bak, canon, rnErr)
					continue
				}
				reversed++
				continue
			}
			if !canonExists {
				continue // leg settled — nothing on either side
			}
			if oldSHA == "" {
				// No pre-op bytes existed ⇒ canon is uncommitted promoted bytes.
				if rmErr := c.fs.Remove(canon); rmErr != nil {
					clean = false
					logging.Warnf("promote reconcile: drop uncommitted %s: %v", canon, rmErr)
				}
				continue
			}
			data, rdErr := afero.ReadFile(c.fs, canon)
			if rdErr != nil {
				clean = false
				logging.Warnf("promote reconcile: read %s: %v", canon, rdErr)
				continue
			}
			if shaContentHex(data) != oldSHA {
				if rmErr := c.fs.Remove(canon); rmErr != nil {
					clean = false
					logging.Warnf("promote reconcile: drop uncommitted %s: %v", canon, rmErr)
				}
			}
			// hash-equal ⇒ already restored on an earlier startup — keep.
		}
		if !clean {
			return reversed // witness survives for the next startup retry
		}
	}
	// r53 P2: committed promotion leaves parked .bak files behind — sweep
	// them BEFORE the witness so a cleanup failure keeps the witness (and a
	// retry completes the sweep) rather than orphaning large backups.
	for _, sfx := range []string{"-full.jpg", ".jpg"} {
		bak := filepath.Join(dir, w.PosterID+sfx+".bak")
		if rmErr := c.fs.Remove(bak); rmErr != nil && !os.IsNotExist(rmErr) {
			logging.Warnf("promote reconcile: bak sweep %s: %v", bak, rmErr)
			return reversed // witness survives for retry
		}
	}
	if rmErr := c.fs.Remove(wpath); rmErr != nil && !os.IsNotExist(rmErr) {
		logging.Warnf("promote witness sweep %s: %v", wpath, rmErr)
	}
	return reversed
}

// reconcileCropWitness arbitrates a staged manual-crop witness (codex r51):
// commit landed ⇒ finish the promotion (staged bytes over canonical);
// otherwise drop the staged leftovers — canonical was never touched
// pre-commit, so nothing else needs repair. Witness removed only after the
// promote completes.
func (c *TempDirCleaner) reconcileCropWitness(ctx context.Context, dir, jobID, wpath string) int {
	data, err := afero.ReadFile(c.fs, wpath)
	var w cropWitness
	if err != nil || json.Unmarshal(data, &w) != nil || w.PosterID == "" || w.ResultID == "" || w.StageID == "" || w.CroppedURL == "" {
		logging.Warnf("crop witness %s unreadable/corrupt — left in place", wpath)
		return 0
	}
	if !witnessLegBasename(w.PosterID) || !witnessLegBasename(w.StageID) {
		logging.Warnf("crop witness %s carries unsafe id fields — left in place", wpath)
		return 0
	}
	job, jerr := c.jobRepo.FindByID(ctx, jobID)
	switch {
	case errors.Is(jerr, database.ErrNotFound):
		return 0
	case jerr != nil:
		logging.Warnf("crop reconcile: job %s lookup failed: %v", jobID, jerr)
		return 0
	}
	committed := false
	var results map[string]*resultstore.MovieResult
	if job != nil {
		if res, ok := arbitrateResults(job); ok {
			results = res
		} else {
			return 0
		}
		for _, r := range results {
			if r != nil && r.ResultID == w.ResultID && r.Movie != nil &&
				strings.EqualFold(r.Movie.ID, w.PosterID) &&
				r.Movie.Poster.CroppedPosterURL == w.CroppedURL && r.Revision > w.PrevRevision {
				committed = true
				break
			}
		}
	}
	staged := filepath.Join(dir, w.StageID+".jpg")
	canon := filepath.Join(dir, w.PosterID+".jpg")
	promoted := 0
	if committed {
		if _, err := c.fs.Stat(staged); err == nil {
			if rnErr := c.fs.Rename(staged, canon); rnErr != nil {
				logging.Warnf("crop reconcile: complete promote %s→%s: %v — witness kept for retry", staged, canon, rnErr)
				return 0
			}
			promoted++
		} else if !os.IsNotExist(err) {
			// r52 P2: a transient stat error must NOT sweep the witness — the
			// staged bytes may still exist and the next startup must retry.
			logging.Warnf("crop reconcile: stat %s: %v — witness kept", staged, err)
			return 0
		}
	} else {
		if rmErr := c.fs.Remove(staged); rmErr != nil && !os.IsNotExist(rmErr) {
			logging.Warnf("crop reconcile: drop staged %s: %v", staged, rmErr)
			return 0
		}
	}
	// r52 P2: the staged full-size copy is only needed by the crop operation
	// itself, never at reconcile time — free it as soon as the crop leg is
	// settled (applied or dropped).
	if rmErr := c.fs.Remove(filepath.Join(dir, w.StageID+"-full.jpg")); rmErr != nil && !os.IsNotExist(rmErr) {
		logging.Warnf("crop reconcile: drop staged full %s: %v", w.StageID+"-full.jpg", rmErr)
	}
	if rmErr := c.fs.Remove(wpath); rmErr != nil && !os.IsNotExist(rmErr) {
		logging.Warnf("crop witness sweep %s: %v", wpath, rmErr)
	}
	return promoted
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
