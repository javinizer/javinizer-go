package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #224 tasks 2.6: an occupied-by-foreign destination on the REAL filesystem
// must conflict and be preserved byte-intact, with our source preserved too.
// OsFs-backed (MemMapFs exercises the virtual classify leg, not the syscall
// no-replace legs that are the point of this change).
func TestOrganizeStrategy_AtomicNoClobber_ForeignPlantWins(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()

	srcDir := filepath.Join(dir, "in")
	dstDir := filepath.Join(dir, "out", "ABC-123")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.MkdirAll(dstDir, 0o755))
	src := filepath.Join(srcDir, "ABC-123.mp4")
	dst := filepath.Join(dstDir, "ABC-123.mp4")
	require.NoError(t, os.WriteFile(src, []byte("ours"), 0o644))
	// Foreign claim at the destination first (post-plan plant).
	require.NoError(t, os.WriteFile(dst, []byte("foreign — a user file"), 0o644))

	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		SourcePath: src,
		TargetDir:  dstDir,
		TargetPath: dst,
		TargetFile: "ABC-123.mp4",
		WillMove:   true,
		moveFiles:  true,
		Conflicts:  []string{},
	}

	_, err := strategy.Execute(plan)
	require.Error(t, err, "foreign-occupied destination must conflict")
	assert.Contains(t, err.Error(), "refusing to overwrite")

	content, rerr := os.ReadFile(dst)
	require.NoError(t, rerr)
	assert.Equal(t, "foreign — a user file", string(content), "foreign destination byte-preserved")
	content, rerr = os.ReadFile(src)
	require.NoError(t, rerr)
	assert.Equal(t, "ours", string(content), "our source byte-preserved")
}
