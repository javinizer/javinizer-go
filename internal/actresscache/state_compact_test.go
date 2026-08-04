package actresscache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenStateCompactsOversizedJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	file, err := os.Create(path)
	require.NoError(t, err)
	encoder := json.NewEncoder(file)
	// 30k updates on 3 keys ≈ tens of MB — over the compaction threshold.
	for i := 0; i < 30000; i++ {
		key := "k" + string(rune('a'+(i%3)))
		entry := StateEntry{Key: key, Status: "ok", Candidate: &Candidate{Key: key, ThumbURL: "https://example.test/" + strings.Repeat("x", 300) + ".jpg", JapaneseName: strings.Repeat("名", 80)}, Thumbnail: &ThumbnailValidation{CheckedAt: "now", SHA256: strings.Repeat("a", 64), Bytes: 100 + i, Width: 100, Height: 100, Format: "jpeg"}}
		require.NoError(t, encoder.Encode(entry))
	}
	require.NoError(t, file.Close())
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(stateCompactionThreshold), "fixture must exceed the threshold")

	store, err := openState(path)
	require.NoError(t, err)
	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Less(t, info.Size(), int64(stateCompactionThreshold), "journal must be compacted on open")
	entry, ok := store.get("kb")
	require.True(t, ok)
	assert.Equal(t, "ok", entry.Status)
	// Latest value survived: the last write to key 'b' had i ≡ 2 mod 3.
	assert.Equal(t, 100+29998-((29998%3)-1), entry.Thumbnail.Bytes)
	require.NoError(t, store.close())
}
