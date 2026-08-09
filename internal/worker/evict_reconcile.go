package worker

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// evictWitness is the durable record of a committed PATCH's stale-poster
// eviction (codex cloud P2): it lives between the commit and the final
// filesystem removals — a crash or wedged leg leaves nothing the reconciler
// can't complete on the next startup.
type evictWitness struct {
	OldID string `json:"old_id"`
}

// reconcileEvictWitness completes an eviction: the committed envelope already
// names the new source, so the old canonical legs leave, then the witness
// sweeps. Transient faults retain the witness for the next startup.
func (c *TempDirCleaner) reconcileEvictWitness(dir, wpath string) int {
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
	failed := false
	for _, name := range []string{w.OldID + "-full.jpg", w.OldID + ".jpg"} {
		if rmErr := c.fs.Remove(filepath.Join(dir, name)); rmErr != nil && !os.IsNotExist(rmErr) {
			failed = true
			logging.Warnf("evict reconcile removal %s: %v", name, rmErr)
		}
	}
	if failed {
		return 0
	}
	if rmErr := c.fs.Remove(wpath); rmErr != nil && !os.IsNotExist(rmErr) {
		logging.Warnf("evict witness sweep %s: %v", wpath, rmErr)
		return 0
	}
	return 1
}
