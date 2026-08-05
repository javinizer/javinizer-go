package actresscache

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
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

// Cover Build's stale-journal error propagation via the seam.
func TestBuildPropagatesStaleWriteFailure(t *testing.T) {
	source := &testSource{name: "test", candidates: []Candidate{{Key: "test:1", Source: "test", SourceID: "1", JapaneseName: "花子", ThumbURL: "https://example.test/thumb.jpg"}}}
	registry := NewRegistry()
	registry.Register("test", func() Source { return source })
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	if _, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator}); err != nil {
		t.Fatalf("seed build: %v", err)
	}
	source.candidates = []Candidate{{Key: "test:2", Source: "test", SourceID: "2", JapaneseName: "插花", ThumbURL: "https://example.test/t2.jpg"}}
	original := markStale
	markStale = func(*stateStore, []string) error { return errors.New("stale journal write failed") }
	t.Cleanup(func() { markStale = original })
	_, _, err := Build(context.Background(), BuildOptions{Registry: registry, Sources: []string{"test"}, StatePath: statePath, ValidateThumbnail: testValidator})
	if err == nil || !strings.Contains(err.Error(), "stale journal write failed") {
		t.Fatalf("expected propagated stale-write error, got %v", err)
	}
}
