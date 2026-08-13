package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// pendingEvictWitnessCore: retained eviction witnesses fence further poster
// ops for the same ID — a surviving witness means canon bytes are displaced
// from the durable row until reconcile runs; edits measure at the stale image.
func pendingEvictWitnessCore(fs afero.Fs, dir, posterID string) (bool, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		if errors.Is(err, afero.ErrFileNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("eviction witness scan %s: %w", dir, err)
	}
	want := strings.TrimSpace(posterID)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, evictWitnessPrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := afero.ReadFile(fs, filepath.Join(dir, name))
		if rerr != nil {
			return false, fmt.Errorf("eviction witness scan %s: %w", name, rerr)
		}
		var w evictWitness
		if json.Unmarshal(data, &w) == nil && w.OldID != "" {
			if strings.EqualFold(strings.TrimSpace(w.OldID), want) {
				return true, nil
			}
			// legacy content-free witness: name-fold still fences sanely.
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(name, evictWitnessPrefix), ".json")
		if id, uerr := url.PathUnescape(raw); uerr == nil {
			raw = id
		}
		if strings.EqualFold(strings.TrimSpace(raw), want) {
			return true, nil
		}
	}
	return false, nil
}

// evictWitness is the durable record of a committed PATCH's stale-poster
// eviction (codex cloud P2): it lives between the commit and the final
// filesystem removals — a crash or wedged leg leaves nothing the reconciler
// can't complete on the next startup.
type evictWitness struct {
	OldID string `json:"old_id"`
	// NewSourceURL is the post-commit effective poster source (the payload's
	// resolved PosterURL|CoverURL per effectivePosterSourceOf) — startup
	// arbitration needs it to decide whether the metadata commit landed
	// (codex cloud P1 @witness arbitration).
	NewSourceURL string `json:"new_source_url,omitempty"`
	// FilePath pins the arbitration to ONE row of the envelope when set
	// (codex PR#211 crash-safety finding): a family can hold same-ID rows on
	// different sources mid-migration — only the row the witness was written
	// for may prove the commit, never a sibling.
	FilePath string `json:"file_path,omitempty"`
}

// reconcileEvictWitness completes a COMMITTED eviction: no commit ⇒ sweep the
// witness without touching canon; commit ⇒ evict then sweep (codex cloud P1
// @SJPs — a bootstrap-before-commit crash pits full witness-vs-row semantics
// eternally; the row's effective poster source is the arbiter).
func (c *TempDirCleaner) reconcileEvictWitness(ctx context.Context, dir, jobID, wpath string) int {
	data, err := afero.ReadFile(c.fs, wpath)
	var w evictWitness
	if err != nil || json.Unmarshal(data, &w) != nil || w.OldID == "" {
		logging.Warnf("evict witness %s unreadable/corrupt — left in place", wpath)
		return 0
	}
	if !witnessLegBasename(w.OldID) {
		logging.Warnf("evict witness %s carries an unsafe id — left in place", wpath)
		return 0
	}
	if c.jobRepo == nil {
		return 0 // no arbiter — keep everything
	}
	job, jerr := c.jobRepo.FindByID(ctx, jobID)
	if jerr != nil {
		logging.Warnf("evict reconcile: job %s lookup failed: %v — witness retained", jobID, jerr)
		return 0
	}
	res, ok := arbitrateResults(job)
	if !ok {
		return 0
	}
	committed := false
	if w.FilePath != "" {
		// Scoped arbitration: ONLY the witness's own row can prove its commit
		// landed — never a same-ID sibling that migrated earlier.
		if r, ok := res[w.FilePath]; ok && r != nil && r.Movie != nil {
			// codex PR#211 round 9: legacy rows whose canonical Movie.ID is
			// empty share their identity through the matcher alias — an eviction
			// targeted at the alias must accept the alias (or its canonical ID)
			// as the persisted witness's identity.
			liveID := strings.TrimSpace(r.Movie.ID)
			if liveID == "" {
				liveID = strings.TrimSpace(r.FileMatchInfo.MovieID)
			}
			if strings.EqualFold(liveID, w.OldID) &&
				effectivePosterSourceOf(r.Movie.Poster.PosterURL, r.Movie.Poster.CoverURL) == w.NewSourceURL {
				committed = true
			}
		}
	} else {
		// Legacy content-less witnesses arbitrate family-wide (pre-P3 records).
		for _, r := range res {
			if r == nil || r.Movie == nil {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(r.Movie.ID), w.OldID) &&
				effectivePosterSourceOf(r.Movie.Poster.PosterURL, r.Movie.Poster.CoverURL) == w.NewSourceURL {
				committed = true
				break
			}
		}
	}
	failed := false
	if committed {
		for _, name := range []string{w.OldID + "-full.jpg", w.OldID + ".jpg"} {
			if rmErr := c.fs.Remove(filepath.Join(dir, name)); rmErr != nil && !os.IsNotExist(rmErr) {
				failed = true
				logging.Warnf("evict reconcile removal %s: %v", name, rmErr)
			}
		}
		if failed {
			return 0
		}
	}
	// sweep the witness either way (uncommitted → nothing to do; committed → done)
	if rmErr := c.fs.Remove(wpath); rmErr != nil && !os.IsNotExist(rmErr) {
		logging.Warnf("evict witness sweep %s: %v", wpath, rmErr)
		return 0
	}
	return 1
}
