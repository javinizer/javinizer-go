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

// Every failure stage of writeFileSync surfaces the right error.
func TestWriteFileSyncReportsEveryStage(t *testing.T) {
	type stage struct{ w, s, c error }
	mk := func(st stage) *faultyFile {
		return &faultyFile{writeErr: st.w, syncErr: st.s, closeErr: st.c}
	}
	cases := map[string]struct {
		inject  stage
		want    string
		openErr error
	}{
		"open":                {openErr: errors.New("no dir"), want: "no dir"},
		"write":               {inject: stage{w: errors.New("disk full")}, want: "disk full"},
		"sync":                {inject: stage{s: errors.New("sync denied")}, want: "sync denied"},
		"close":               {inject: stage{c: errors.New("close failed")}, want: "close failed"},
		"sync loses to write": {inject: stage{w: errors.New("disk full"), s: errors.New("sync denied")}, want: "disk full"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			original := stateSyncOpen
			t.Cleanup(func() { stateSyncOpen = original })
			if tc.openErr != nil {
				stateSyncOpen = func(string, os.FileMode) (syncFile, error) { return nil, tc.openErr }
				require.ErrorContains(t, writeFileSync("/no/such/x", []byte("d"), 0o600), tc.want)
				return
			}
			file := mk(tc.inject)
			stateSyncOpen = func(string, os.FileMode) (syncFile, error) { return file, nil }
			require.ErrorContains(t, writeFileSync("x", []byte("d"), 0o600), tc.want)
		})
	}
	// Happy path nonetheless durable: content lands correctly.
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	require.NoError(t, writeFileSync(path, []byte(`{"k":1}`+"\n"), 0o600))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"k":1`)
}

type faultyFile struct {
	writeErr error
	syncErr  error
	closeErr error
}

func (f *faultyFile) Write(p []byte) (int, error) { return len(p), f.writeErr }
func (f *faultyFile) Sync() error                 { return f.syncErr }
func (f *faultyFile) Close() error                { return f.closeErr }
