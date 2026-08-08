package actresscache

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarkStaleEntriesWriteFailure(t *testing.T) {
	// Established ok entry, journal writer that errors on every append.
	entryCandidate := Candidate{Key: "one", Source: "test"}
	entry := StateEntry{Key: "one", Status: "ok", Candidate: &entryCandidate, Thumbnail: &ThumbnailValidation{}}
	store := &stateStore{entries: map[string]StateEntry{"one": entry}, encoder: json.NewEncoder(errorWriter{})}
	err := markStaleEntries(store, []string{"one"})
	assert.ErrorContains(t, err, "write stale state")
	assert.ErrorContains(t, err, "one")
	// Skip-missing keys are tolerated; a tombstoned (non-ok) entry is a no-op.
	tomb := StateEntry{Key: "gone", Status: "stale"}
	store.entries["gone"] = tomb
	if err := markStaleEntries(store, []string{"missing", "gone"}); err != nil {
		t.Fatalf("expected tolerance for non-ok keys, got %v", err)
	}
}

// JournalStale fails when the journal path cannot even be opened
// (post-publish ordering keeps this off the critical path).
func TestJournalStaleErrorsOnBadPath(t *testing.T) {
	badDir := filepath.Join(t.TempDir(), "nonexistent")
	assert.NoError(t, JournalStale("", []string{"x"}))
	assert.NoError(t, JournalStale(filepath.Join(t.TempDir(), "s.jsonl"), nil))
	// A directory as the journal path fails to read (not NotExist, not writable).
	err := JournalStale(filepath.Dir(badDir), []string{"x"})
	assert.Error(t, err)
}
