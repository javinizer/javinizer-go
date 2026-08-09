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
	"sort"
	"strconv"
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

// evictWitnessPrefix names the crash-recovery record for a committed patch's
// stale-poster eviction (codex cloud P2): complete-on-restart, no arbitration
// needed (the DURABLE row already names the new source).
const evictWitnessPrefix = ".evict-"

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
			isEvict := strings.HasPrefix(name, evictWitnessPrefix) && strings.HasSuffix(name, ".json")
			if !isRekey && !isPromote && !isCrop && !isEvict {
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
			if isEvict {
				reversed += c.reconcileEvictWitness(ctx, dir, jobID, filepath.Join(dir, name))
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
		reversed += c.reconcileParkedPosterBackups(ctx, je.Name(), dir)
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
func (c *TempDirCleaner) reconcileParkedPosterBackups(ctx context.Context, jobID, dir string) int {
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
	strandedMeta := map[string]inFlightMeta{}
	var pendingRsbak []pendingPark
	var strandedMarkers []strandedMarker
	var commitTokens []commitToken
	for _, e := range entries {
		name := e.Name()
		var canon string
		var rsbakNonce string
		// codex cloud P2 (@541) shape disambiguation: a parked LEG's ".rsbak."
		// always immediately follows the jpg suffix — any ".rsbak." deeper in
		// the name belongs to the poster ID itself (a sentinel for an ID that
		// literally ends in ".rsbak"; a parked leg for a leading-dot ID).
		if markerAnchored(name) && !isParkedBackupName(name) {
			// audit F-R19-1 aftermath: stranded in-flight markers (crash) — a
			// restarted process has no live generation windows; delete.
			// codex cloud P1: harvest the op provenance FIRST — parked backups
			// sharing this nonce arbitrate against the durable row, and this
			// marker IS their persisted record.
			// codex cloud P2: REMOVAL is deferred until every parked leg sharing
			// this nonce has settled (post-arbitration finalization) — deleting
			// the only provenance before the bytes resolve permanently strands
			// the legs (and a marker we failed to READ may carry records we
			// haven't seen).
			sm := strandedMarker{name: name}
			if i1 := strings.LastIndexByte(name, '.'); i1 >= 0 {
				if i0 := strings.LastIndexByte(name[:i1], '.'); i0 >= 0 {
					sm.nonce = name[i0+1:]
				}
			}
			if data, rdErr := afero.ReadFile(c.fs, filepath.Join(dir, name)); rdErr == nil {
				var meta inFlightMeta
				if json.Unmarshal(data, &meta) == nil && meta.PosterID != "" {
					strandedMeta[sm.nonce] = meta
					sm.hasProvenance = true
				}
			} else {
				sm.readFailed = true
			}
			strandedMarkers = append(strandedMarkers, sm)
			continue
		}
		if commitAnchored(name) {
			// codex cloud P1: commit tokens name the WINNING op — harvest before
			// any classification so stranded backups can attribute the commit
			// (and never lean on bare family revision advance again).
			if data, rdErr := afero.ReadFile(c.fs, filepath.Join(dir, name)); rdErr == nil {
				var cm commitMeta
				if json.Unmarshal(data, &cm) == nil && cm.PosterID != "" {
					if i1 := strings.LastIndexByte(name, '.'); i1 >= 0 {
						if i0 := strings.LastIndexByte(name[:i1], '.'); i0 >= 0 {
							commitTokens = append(commitTokens, commitToken{name: name, base: strings.TrimSpace(cm.PosterID), nonce: name[i0+1:], meta: cm})
						}
					}
				}
			}
			continue // tokens sweep at finalization once no base-parking legs pend
		}
		switch {
		case strings.HasPrefix(name, promoteWitnessPrefix) || strings.HasPrefix(name, rekeyWitnessPrefix) || strings.HasPrefix(name, cropWitnessPrefix):
			continue // witness files are never parked backups (F-R5-3 belt)
		case strings.HasSuffix(name, ".dlbak"):
			canon = strings.TrimSuffix(name, ".dlbak") // legacy + manager park
		default:
			// codex cloud P2: ONLY jpg-adjacent ".rsbak." names are parked
			// legs — anything with the token deeper in the name (e.g. inside a
			// dotted poster ID) is not a rescrape park at all.
			if !isParkedBackupName(name) {
				continue
			}
			idx := strings.LastIndex(name, ".rsbak.")
			canon = name[:idx]
			rsbakNonce = name[idx+len(".rsbak."):]
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
		if rsbakNonce != "" {
			// codex cloud P1: rescrape parks arbitrate AFTER the sweep —
			// canonical presence alone never again justifies a delete.
			pendingRsbak = append(pendingRsbak, pendingPark{name: name, canon: canon, nonce: rsbakNonce})
			continue
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
	if len(pendingRsbak) > 0 {
		healed += c.arbitrateParkedRescrapeBackups(ctx, jobID, dir, pendingRsbak, strandedMeta, commitTokens)
	}

	// codex cloud P2 (@644): ONE fold-aware ledger feeds both sweeps; an
	// UNDECIDABLE rescan skips ALL finalization — the sweeps would otherwise
	// delete the very provenance records a pending leg needs.
	if len(strandedMarkers) > 0 || len(commitTokens) > 0 {
		remaining := map[string]bool{}
		pendingBases := map[string]bool{}
		entries2, rdErr := afero.ReadDir(c.fs, dir)
		if rdErr != nil {
			logging.Warnf("recovery finalize: rescan %s undecidable (%v) — markers and tokens kept for the next startup", dir, rdErr)
			return healed
		}
		for _, e2 := range entries2 {
			n2 := e2.Name()
			idx := strings.LastIndex(n2, ".rsbak.")
			if idx < 0 || !isBackupNonce(n2[idx+len(".rsbak."):]) {
				continue
			}
			remaining[n2[idx+len(".rsbak."):]] = true
			cb := n2[:idx]
			if strings.HasSuffix(cb, "-full.jpg") {
				cb = strings.TrimSuffix(cb, "-full.jpg")
			} else {
				cb = strings.TrimSuffix(cb, ".jpg")
			}
			// codex cloud P2 (@685): case-fold the pending-base key — a winner's
			// token must attribute either spelling of the stranded backup's ID.
			pendingBases[strings.ToLower(strings.TrimSpace(cb))] = true
		}
		for _, m := range strandedMarkers {
			if remaining[m.nonce] && (m.hasProvenance || m.readFailed) {
				continue // marker retained: legs unresolved — provenance must persist
			}
			if rmErr := c.fs.Remove(filepath.Join(dir, m.name)); rmErr != nil {
				logging.Warnf("in-flight marker sweep %s: %v", m.name, rmErr)
				continue
			}
			healed++
		}
		// codex cloud P1 leak guard: a commit token settles when no parked leg for
		// its base pends anymore — attribution evidence only lives while bytes
		// remain contestable.
		for _, tok := range commitTokens {
			if pendingBases[strings.ToLower(strings.TrimSpace(tok.base))] {
				continue
			}
			if rmErr := c.fs.Remove(filepath.Join(dir, tok.name)); rmErr != nil {
				logging.Warnf("commit token sweep %s: %v", tok.name, rmErr)
				continue
			}
			healed++
		}
	}
	return healed
}

// strandedMarker is a crashed op's in-flight sentinel sighted at startup;
// removal waits until every parked leg sharing its nonce settles (codex
// cloud P2), and only parsed-payload or unreadable markers can carry the
// provenance legs need.
type strandedMarker struct {
	name          string
	nonce         string
	hasProvenance bool
	readFailed    bool
}

// staleCleanupInterval is a package var so tests can accelerate the periodic
// sweep without waiting real hours.
var staleCleanupInterval = 24 * time.Hour

// pendingPark is a rescrape parked backup awaiting post-sweep arbitration.
type pendingPark struct {
	name, canon, nonce string
}

// arbitrateParkedRescrapeBackups (codex cloud P1): a crash between
// GeneratePoster and CompleteRescrape leaves the canonical name holding
// UNCOMMITTED generated bytes while the .rsbak twin holds the last committed
// poster. Canonical presence alone is no evidence of a won race, so each
// backup arbitrates against the op's persisted provenance (the stranded
// sentinel payload, paired by their shared nonce) and the durable job row:
//   - durable revision advanced past the op's capture ⇒ the commit landed ⇒
//     the canonical bytes are legit ⇒ the backup is safe to drop;
//   - otherwise the commit never landed ⇒ restore the committed backup over
//     the stranded generation.
//
// Missing provenance, a foreign provenance ID, or an undecidable durable row
// keep BOTH copies — disk litter beats unrecoverable bytes. Multiple crashed
// ops unwind newest-first (their confidence chain stacks).
func (c *TempDirCleaner) arbitrateParkedRescrapeBackups(ctx context.Context, jobID, dir string, pending []pendingPark, stranded map[string]inFlightMeta, tokens []commitToken) int {
	byCanon := map[string][]pendingPark{}
	for _, p := range pending {
		// codex cloud P2: group by the case-folded canonical key — case-insensitive
		// filesystems make two spellings address the same file; split stacks would
		// unwind an older op first against real storage order.
		byCanon[strings.ToLower(p.canon)] = append(byCanon[strings.ToLower(p.canon)], p)
	}
	nonceTime, nonceSeq := func(n string) uint64 {
		p := strings.Split(n, ".")
		v, _ := strconv.ParseUint(p[0], 16, 64)
		return v
	}, func(n string) uint64 {
		p := strings.Split(n, ".")
		v, _ := strconv.ParseUint(p[1], 16, 64)
		return v
	}
	healed := 0
	for _, list := range byCanon {
		// newest-op FIRST: chained crashed ops form a stack — unwinding oldest
		// first would re-restore older backup bytes over the newer op's rewind.
		sort.Slice(list, func(i, j int) bool {
			hi, hj := nonceTime(list[i].nonce), nonceTime(list[j].nonce)
			if hi != hj {
				return hi > hj
			}
			return nonceSeq(list[i].nonce) > nonceSeq(list[j].nonce)
		})
		for _, p := range list {
			parked := filepath.Join(dir, p.name)
			// codex cloud P2 (@819): the FOLDED key exists for GROUPING only —
			// each entry's own spelling drives all fs operations on case-SENSITIVE
			// filesystems (case-insensitive ones converge them physically anyway).
			canonPath := filepath.Join(dir, p.canon)
			if _, statErr := c.fs.Stat(canonPath); statErr != nil {
				if !os.IsNotExist(statErr) {
					// (unchanged invariant) undecidable canonical state: touch neither side
					logging.Warnf("parked backup sweep %s: canonical indeterminate (%v) — kept both", parked, statErr)
					continue
				}
				// canonical ABSENT: restoring committed bytes is safe with or
				// without op provenance (nothing is being overwritten).
				if rnErr := c.fs.Rename(parked, canonPath); rnErr != nil {
					logging.Warnf("parked backup restore %s→%s: %v", parked, canonPath, rnErr)
					continue
				}
				healed++
				continue
			}
			meta, ok := stranded[p.nonce]
			if !ok {
				// zero op provenance (legacy/foreign backup): canon presence alone
				// never justifies deletion again (codex cloud P1).
				logging.Warnf("parked backup sweep %s: no op provenance — kept both", parked)
				continue
			}
			base := p.canon
			if strings.HasSuffix(base, "-full.jpg") {
				base = strings.TrimSuffix(base, "-full.jpg")
			} else {
				base = strings.TrimSuffix(base, ".jpg")
			}
			if !strings.EqualFold(strings.TrimSpace(meta.PosterID), base) {
				logging.Warnf("parked backup sweep %s: provenance id %q mismatches owner %q — kept both", parked, meta.PosterID, base)
				continue
			}
			// codex cloud P1 (@758): a family revision advance does NOT identify the
			// winning op — attribute by commit token first:
			//   own nonce token ⇒ this op committed ⇒ its backup is obsolete;
			//   foreign token whose SHA the CANON currently carries ⇒ family whole
			//     already ⇒ backup obsolete;
			//   foreign token whose SHA this BACKUP carries ⇒ canon was lost to a
			//     later uncommitted op ⇒ restoring this backup recovers committed bytes;
			//   foreign token matching NEITHER ⇒ a newer-op unwind settles this leg
			//     this run ⇒ keep both (never guess).
			if tok := tokenForNonce(tokens, p.nonce); tok != nil {
				// codex cloud P1 (deferral binding): this op's token proves its
				// IN-MEMORY commit — the DURABLE envelope lands later. Only when the
				// durable row also advanced past the captured baseline is the backup
				// truly obsolete; a token without the durable advance means the
				// envelope write never happened, so the canonical bytes must rewind.
				rev, decidable := c.posterDurableRevision(ctx, jobID, meta.PosterID)
				if !decidable {
					logging.Warnf("parked backup sweep %s: durable revision undecidable — kept both", parked)
					continue
				}
				if rev > meta.PrevRevision {
					if rmErr := c.fs.Remove(parked); rmErr != nil {
						logging.Warnf("parked backup sweep %s: %v", parked, rmErr)
						continue
					}
					healed++
					continue
				}
				if rnErr := c.fs.Rename(parked, canonPath); rnErr != nil {
					logging.Warnf("parked backup restore %s→%s: %v", parked, canonPath, rnErr)
					continue
				}
				healed++
				continue
			}
			if fToks := foreignTokensForBase(tokens, base, p.nonce); len(fToks) > 0 {
				// codex cloud P2 (@822): every same-base token is candidate evidence
				// — with overlapping commits the FIRST is the older loser, never a
				// reason alone to call content ambiguous.
				// codex cloud P1: a foreign token is evidential only if ITS winner op
				// provably committed durably — vet each candidate by its Sentinel's
				// baseline vs the current durable revision first, or an
				// in-memory-only commit's token would bless uncommitted bytes.
				trusted := make([]commitToken, 0, len(fToks))
				for _, t := range fToks {
					wm, wok := stranded[t.nonce]
					if !wok {
						continue // token without its Sentinel context stays untrusted
					}
					rev, okRev := c.posterDurableRevision(ctx, jobID, wm.PosterID)
					if okRev && rev > wm.PrevRevision {
						trusted = append(trusted, t)
					}
				}
				if len(trusted) == 0 {
					logging.Warnf("parked backup sweep %s: commit tokens present but none provably durabled — kept both", parked)
					continue
				}
				bakData, bErr := afero.ReadFile(c.fs, parked)
				canonData, cErr := afero.ReadFile(c.fs, canonPath)
				matchedCanon := false
				matchedBak := false
				if bErr == nil && cErr == nil {
					for _, t := range trusted {
						wantSHA := tokenLegSHA(t.meta, p.canon)
						if wantSHA == "" {
							continue
						}
						switch shaContentHex(canonData) == wantSHA {
						case true:
							matchedCanon = true
						}
						if shaContentHex(bakData) == wantSHA {
							matchedBak = true
						}
					}
				}
				switch {
				case matchedCanon:
					if rmErr := c.fs.Remove(parked); rmErr != nil {
						logging.Warnf("parked backup sweep %s: %v", parked, rmErr)
						continue
					}
					healed++
					continue
				case matchedBak:
					if rnErr := c.fs.Rename(parked, canonPath); rnErr != nil {
						logging.Warnf("parked backup restore %s→%s: %v", parked, canonPath, rnErr)
						continue
					}
					healed++
					continue
				default:
					logging.Warnf("parked backup sweep %s: commit present but content ambiguous — kept both", parked)
				}
				continue
			}
			rev, decidable := c.posterDurableRevision(ctx, jobID, meta.PosterID)
			if !decidable {
				logging.Warnf("parked backup sweep %s: durable revision undecidable — kept both", parked)
				continue
			}
			if rev > meta.PrevRevision {
				// codex cloud P1 bare-advance fallback: NO commit token exists for
				// this base, so the advance is unattributable — an overlapping-op
				// interleave could blame this op for another's commit. Keep both.
				logging.Warnf("parked backup sweep %s: revision advanced without op-attributed commit token — kept both", parked)
				continue
			}
			// the commit never landed: canonical holds stranded generation —
			// restore the committed backup over it.
			if rnErr := c.fs.Rename(parked, canonPath); rnErr != nil {
				logging.Warnf("parked backup restore %s→%s: %v", parked, canonPath, rnErr)
				continue
			}
			healed++
		}
	}
	return healed
}

// posterDurableRevision reports the highest durable revision among the job's// isParkedBackupName: the parked-leg shape REQUIRES ".rsbak." immediately
// after the jpg suffix (full or cropped leg) — a deeper infix occurrence just
// means a poster id containing the token (codex cloud P2 @541).
func isParkedBackupName(name string) bool {
	i := strings.LastIndex(name, ".rsbak.")
	if i < 0 || !isBackupNonce(name[i+len(".rsbak."):]) {
		return false
	}
	pre := name[:i]
	return strings.HasSuffix(pre, "-full.jpg") || strings.HasSuffix(pre, ".jpg")
} // commitToken couples an op-attributed commit payload with its filename parts.
type commitToken struct {
	name  string
	base  string
	nonce string
	meta  commitMeta
}

const commitWitnessPrefix = ".commit-"

// commitAnchored mirrors markerAnchored for the ".commit-" lane: prefix plus
// the hex.hex nonce tail — never a parked-leg interpretation.
func commitAnchored(name string) bool {
	return strings.HasPrefix(name, commitWitnessPrefix) && hexLowerHexTail(name)
}

// tokenForNonce finds the commit token produced by the SAME op (nonce pair).
func tokenForNonce(tokens []commitToken, nonce string) *commitToken {
	for i := range tokens {
		if tokens[i].nonce == nonce {
			return &tokens[i]
		}
	}
	return nil
}

// foreignTokensForBase lists every OTHER op's commit token for this base —
// stacked commits need any-match arbitration, never first-match truth
// (codex cloud P2 @822).
func foreignTokensForBase(tokens []commitToken, base, nonce string) []commitToken {
	out := []commitToken{}
	for i := range tokens {
		if tokens[i].nonce != nonce && strings.EqualFold(tokens[i].base, base) {
			out = append(out, tokens[i])
		}
	}
	return out
}

// tokenLegSHA picks the token's SHA matching the canonical leg form.// tokenLegSHA picks the token's SHA matching the canonical leg form.
func tokenLegSHA(m commitMeta, canon string) string {
	if strings.HasSuffix(canon, "-full.jpg") {
		return m.FullSHA
	}
	return m.CropSHA
}

// results carrying the poster ID (Movie.ID first, match-info fallback). Any
// incomparability (missing repo, missing job conclusively, undecodable
// results, no matching row) is undecidable — matching state decides byte fate.
func (c *TempDirCleaner) posterDurableRevision(ctx context.Context, jobID, posterID string) (uint64, bool) {
	if c.jobRepo == nil {
		return 0, false
	}
	job, jerr := c.jobRepo.FindByID(ctx, jobID)
	if jerr != nil {
		return 0, false
	}
	res, ok := arbitrateResults(job)
	if !ok {
		return 0, false
	}
	want := strings.TrimSpace(posterID)
	found := false
	var best uint64
	for _, r := range res {
		// nil rows cannot exist here: jobpersist.Decode drops null entries.
		id := ""
		if r.Movie != nil {
			id = strings.TrimSpace(r.Movie.ID)
		}
		if id == "" {
			id = strings.TrimSpace(r.FileMatchInfo.MovieID)
		}
		if !strings.EqualFold(id, want) {
			continue
		}
		if !found || r.Revision > best {
			best, found = r.Revision, true
		}
	}
	return best, found
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
	// Read the (test-overridable) interval on the CALLER's goroutine — reading
	// it inside the spawned goroutine races a test's var restore.
	interval := staleCleanupInterval
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

		ticker := time.NewTicker(interval)
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
