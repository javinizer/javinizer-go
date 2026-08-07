package batch

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Witness filename stem: hostile/traversal-ish poster IDs never escape the
// job poster dir (the reconciler parses the ID from content, not filename).
func TestPromoteWitnessTraversalSafeName(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-1"
	if err := fs.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := writePromoteWitness(fs, "/tmp", "JOB-1", "../evil", "https://x/y.jpg")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".promote-___evil.json"), p, "stem is scrubbed inside the job dir")
	got, err := afero.ReadFile(fs, p)
	require.NoError(t, err)
	var w promoteWitness
	require.NoError(t, json.Unmarshal(got, &w))
	assert.Equal(t, "../evil", w.PosterID, "real identity preserved in content")
	removePromoteWitness(fs, p)
	_, statErr := fs.Stat(p)
	assert.Error(t, statErr)
}
