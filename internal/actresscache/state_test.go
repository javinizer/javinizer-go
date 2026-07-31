package actresscache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateStoreRoundTripAndLatestEntryWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	store, err := openState(path)
	require.NoError(t, err)
	require.NoError(t, store.append(StateEntry{Key: "one", Status: "failed"}))
	require.NoError(t, store.append(StateEntry{Key: "one", Status: "ok"}))
	require.NoError(t, store.close())
	loaded, err := openState(path)
	require.NoError(t, err)
	defer loaded.close()
	entry, ok := loaded.get("one")
	require.True(t, ok)
	assert.Equal(t, "ok", entry.Status)
}

func TestStateStoreRepairsInterruptedTailBeforeAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	data := []byte("{\"key\":\"one\",\"status\":\"ok\"}\n{\"key\":\"broken\"")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	store, err := openState(path)
	require.NoError(t, err)
	require.NoError(t, store.append(StateEntry{Key: "two", Status: "ok"}))
	require.NoError(t, store.close())
	loaded, err := openState(path)
	require.NoError(t, err)
	defer loaded.close()
	_, ok := loaded.get("one")
	assert.True(t, ok)
	entry, ok := loaded.get("two")
	require.True(t, ok)
	assert.Equal(t, "ok", entry.Status)
}

func TestStateStoreAddsDelimiterToCompleteTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{\"key\":\"one\",\"status\":\"ok\"}"), 0o644))
	store, err := openState(path)
	require.NoError(t, err)
	require.NoError(t, store.append(StateEntry{Key: "two", Status: "ok"}))
	require.NoError(t, store.close())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	entries := make(map[string]StateEntry)
	require.NoError(t, readState(strings.NewReader(string(data)), entries))
	assert.Len(t, entries, 2)
}

func TestReadStateRejectsCorruptCompleteLine(t *testing.T) {
	entries := make(map[string]StateEntry)
	err := readState(strings.NewReader(`{"key":"one","status":"ok"}
not-json
`), entries)
	require.Error(t, err)
}

func TestStateStoreWithoutPathIsMemoryOnly(t *testing.T) {
	store, err := openState("")
	require.NoError(t, err)
	require.NoError(t, store.append(StateEntry{Key: "one", Status: "ok"}))
	entry, ok := store.get("one")
	require.True(t, ok)
	assert.Equal(t, "ok", entry.Status)
	assert.NoError(t, store.close())
	var nilStore *stateStore
	_, ok = nilStore.get("missing")
	assert.False(t, ok)
	assert.NoError(t, nilStore.append(StateEntry{Key: "ignored"}))
	assert.NoError(t, nilStore.close())
}
