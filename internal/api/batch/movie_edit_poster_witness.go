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

// promoteWitnessConflict reports errPromoteWitnessPending whenever a promote
// witness for posterID exists — matched by CONTENT first (payload PosterID,
// fold-cased) with a fold-cased NAME fallback for legacy contentless
// payloads. Read errors fail closed (codex cloud P1: case-fold fences).
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
