package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fallback probe leaves at in-place classifyExistingDestination fire for a
// fs that reports `didLstat=false` from its Lstater. planStatFs is that
// fixture — public, proven (it's used by tests in organize_pathclass_coverage_test.go).
func TestFallbackProbe_SymlinkKindViaDanglingProbe(t *testing.T) {
	dir := t.TempDir()
	fsOf := afero.NewOsFs()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "in"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "in", "V.mp4"), []byte("v"), 0o644))
	foreign := filepath.Join(dir, "foreign.mp4")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o644))
	dst := filepath.Join(dir, "out", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.Symlink(foreign, dst))

	wrapped := plainStatFs{Fs: fsOf}
	conflicts := checkTargetConflict(wrapped, filepath.Join(dir, "in", "V.mp4"), dst, false, true)
	require.Len(t, conflicts, 1)
	assert.Equal(t, ConflictSymlink, conflicts[0].Kind)
}
