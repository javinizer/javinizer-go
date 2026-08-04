package actresscache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeOversizedJournal writes enough padded keys to cross the compaction
// threshold (reuses the same shape as state_compact_test.go).
func writeOversizedJournal(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	require.NoError(t, err)
	encoder := json.NewEncoder(file)
	for i := 0; i < 30000; i++ {
		key := "k" + strings.Repeat("x", 250) + string(rune('a'+(i%3)))
		entry := StateEntry{Key: key, Status: "ok", Candidate: &Candidate{Key: key, ThumbURL: "https://example.test/x.jpg"}, Thumbnail: &ThumbnailValidation{CheckedAt: "n", SHA256: "x", Bytes: 100, Width: 100, Height: 100, Format: "jpeg"}}
		require.NoError(t, encoder.Encode(entry))
	}
	require.NoError(t, file.Close())
}

func TestOpenStateSurfacesCompactionWriteError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	writeOversizedJournal(t, path)
	original := stateWriteFileNew
	stateWriteFileNew = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	t.Cleanup(func() { stateWriteFileNew = original })
	_, err := openState(path)
	require.ErrorContains(t, err, "compact actress cache state")
}

func TestOpenStateSurfacesCompactionRenameError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	writeOversizedJournal(t, path)
	original := stateRename
	stateRename = func(string, string) error { return errors.New("rename denied") }
	t.Cleanup(func() { stateRename = original })
	_, err := openState(path)
	require.ErrorContains(t, err, "compact actress cache state")
	// The failed rename must leave the original journal in place.
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)
}
