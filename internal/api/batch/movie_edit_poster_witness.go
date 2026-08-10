package batch

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// removeWithRetry mirrors the worker's bounded sweep retry: a transient
// removal wedge must never poison family fences till a restart.
const witnessSweepRetries = 3

func removeWithRetry(fs afero.Fs, path string) error {
	var err error
	for i := 0; i < witnessSweepRetries; i++ {
		if err = fs.Remove(path); err == nil || os.IsNotExist(err) {
			return nil
		}
	}
	return err
}

// evictWitnessConflict mirrors pendingEvictWitnessCore (worker): any dispute
// witne?? placeholder)' escape first — use exact matching below via python compatibly

// witness for posterID exists — matched by CONTENT first (payload PosterID,
// fold-cased) with a fold-cased NAME fallback for legacy contentless
// payloads. Read errors fail closed (codex cloud P1: case-fold fences).
// pendingEvictFromDir reports whether any .evict- witness names this poster
// (fold-cased OldID) — retained eviction leftovers gate further poster edits
// the same way pending crop/promote records do (codex cloud P2 @snFs).
func pendingEvictFromDir(fs afero.Fs, dir, posterID string) (bool, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("eviction witness scan %s: %w", dir, err)
	}
	want := strings.TrimSpace(posterID)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".evict-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := afero.ReadFile(fs, filepath.Join(dir, name))
		if rerr != nil {
			return false, fmt.Errorf("eviction witness scan %s: %w", name, rerr)
		}
		var w struct {
			OldID string `json:"old_id"`
		}
		if json.Unmarshal(data, &w) == nil && w.OldID != "" {
			if strings.EqualFold(strings.TrimSpace(w.OldID), want) {
				return true, nil
			}
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(name, ".evict-"), ".json")
		if id, uerr := url.PathUnescape(raw); uerr == nil {
			raw = id
		}
		if strings.EqualFold(strings.TrimSpace(raw), want) {
			return true, nil
		}
	}
	return false, nil
}

func promoteWitnessConflict(fs afero.Fs, dir, posterID string) error {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("promote witness scan %s: %w", dir, err)
	}
	want := strings.TrimSpace(posterID)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, promoteWitnessPrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, rerr := afero.ReadFile(fs, filepath.Join(dir, name))
		if rerr != nil {
			return fmt.Errorf("promote witness scan %s: %w", name, rerr)
		}
		var w struct {
			PosterID string `json:"poster_id"`
		}
		if json.Unmarshal(data, &w) == nil && w.PosterID != "" {
			if strings.EqualFold(strings.TrimSpace(w.PosterID), want) {
				return fmt.Errorf("%w for %s", errPromoteWitnessPending, posterID)
			}
			continue
		}
		// Legacy contentless witness: fold-compare by filename instead.
		raw := strings.TrimSuffix(strings.TrimPrefix(name, promoteWitnessPrefix), ".json")
		if id, uerr := url.PathUnescape(raw); uerr == nil {
			raw = id
		}
		if strings.EqualFold(strings.TrimSpace(raw), want) {
			return fmt.Errorf("%w for %s", errPromoteWitnessPending, posterID)
		}
	}
	return nil
}
