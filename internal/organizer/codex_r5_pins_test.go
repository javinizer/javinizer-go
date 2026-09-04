package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Live-symlink destination classified via didLstat=false fallback probe
// (readlink): a fallback Stat never distinguishes a live link from its target.
// Pin codex P2: live symlink at dst must be ConflictSymlink even when
// classification came through the fallback track.
func TestCodexR5_ClassifyLiveSymlinkViaFallback(t *testing.T) {
	dir := t.TempDir()
	fsOf := afero.NewOsFs()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "V.mp4"), []byte("v"), 0o644))
	foreign := filepath.Join(dir, "foreign.mp4")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o644))
	dst := filepath.Join(dir, "out", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.Symlink(foreign, dst))

	wrapped := plainStatFs{Fs: fsOf}
	conflicts := checkTargetConflict(wrapped, filepath.Join(dir, "src", "V.mp4"), dst, false, true)
	require.Len(t, conflicts, 1)
	assert.Equal(t, ConflictSymlink, conflicts[0].Kind)
}

// Live symlink under plainStatFs: kind stays symlink but the pre-execute
// classifier may otherwise mislabel it as a File conflict — probe via the
// codex-pinning path using plainStatFs (readlink is that fixture's natura).
func TestCodexR5_AuthorizedCopyOntoSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	dst := filepath.Join(dir, "out", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	foreign := filepath.Join(dir, "foreign.mp4")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o644))
	require.NoError(t, os.Symlink(foreign, dst))

	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "V.mp4", Extension: ".mp4", MovieID: "V"},
		SourcePath: src,
		TargetDir:  filepath.Dir(dst),
		TargetFile: "V.mp4",
		TargetPath: dst,
		WillMove:   true,
		moveFiles:  false, // LinkModeNone copy lane
		Conflicts:  []PlanConflict{{Path: dst, Kind: ConflictSymlink}},
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err, "symlink dest refuses on the copy lane")
	// Refusal is via the plan-surface bare-path rendering (per design: typed
	// conflicts don't paint the kind onto plan-conflict messages).
	assert.Contains(t, err.Error(), "conflicts detected")
	sl := filepath.ToSlash
	assert.Contains(t, sl(err.Error()), sl(dst))
	info, lerr := os.Lstat(dst)
	require.NoError(t, lerr)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the symlink object must not be rehearsed or removed")
}

// In-place TargetDir symlink at plan time: KindSymlink (special case exclusion
// from target-dir like-for-file kind mapping).
func TestCodexR5_InPlaceDetectsSymlinkTargetDir(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	oldDir := filepath.Join(dir, "mixed")
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "ABC.mp4"), []byte("v"), 0o644))
	// Dangling link at what in-place wants to rename to:
	require.NoError(t, os.Symlink(filepath.Join(dir, "whatever.mp4"), filepath.Join(dir, "ABC")))

	cfg := &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	stMatcher, mErr := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, mErr)
	strategy := newInPlaceStrategy(fs, cfg, stMatcher, nil)
	match := models.FileMatchInfo{Path: filepath.Join(oldDir, "ABC.mp4"), Name: "ABC.mp4", Extension: ".mp4", MovieID: "ABC"}
	movie := &models.Movie{ID: "ABC"}
	plan, err := strategy.Plan(match, movie, "", false)
	require.NoError(t, err)
	require.Len(t, plan.Conflicts, 1)
	assert.Equal(t, ConflictSymlink, plan.Conflicts[0].Kind)
}

// Direct OsFs without Lstater: classifyExistingDestination sees didLstat=false
// on a live symlink at dst.
func TestCodexR5_ClassifyLiveThroughPlainStatLane(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "V.mp4"), []byte("v"), 0o644))
	foreign := filepath.Join(dir, "foreign.mp4")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o644))
	dst := filepath.Join(dir, "out", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.Symlink(foreign, dst))

	wrapped := plainStatFs{Fs: fs}
	c := classifyExistingDestination(wrapped, filepath.Join(dir, "src", "V.mp4"), dst)
	require.NotNil(t, c.Conflict)
	assert.Equal(t, ConflictSymlink, c.Conflict.Kind)
}
