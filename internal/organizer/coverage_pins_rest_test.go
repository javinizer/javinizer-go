package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// in-place main-move authorized lane with same-inode alias: the classify must
// no-op (never replacing the geometry).
func TestRestPin_InPlaceMainSameInodeNoOp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in", "V.mp4")
	dst := filepath.Join(dir, "out", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	require.NoError(t, os.Link(src, dst))

	fs := afero.NewOsFs()
	strategy := newInPlaceStrategy(fs, &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}, nil, nil)
	plan := &OrganizePlan{
		Match:               models.FileMatchInfo{Path: src, Name: "V.mp4", Extension: ".mp4", MovieID: "V"},
		SourcePath:          src,
		TargetDir:           filepath.Dir(dst),
		TargetFile:          "V.mp4",
		TargetPath:          dst,
		WillMove:            true,
		InPlace:             false, // main-move lane
		Conflicts:           []PlanConflict{},
		overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.NoError(t, err)
	content, _ := os.ReadFile(dst)
	assert.Equal(t, "v", string(content))
}

// plan.Conflicts non-empty on the organize Execute() surface returns
// grouped refusal from execute leg (the canonical gate).
func TestRestPin_ExecuteConflictSurface(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in", "V.mp4")
	dst := filepath.Join(dir, "out", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o644))

	fs := afero.NewOsFs()
	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "V.mp4", Extension: ".mp4", MovieID: "V"},
		SourcePath: src,
		TargetDir:  filepath.Dir(dst),
		TargetFile: "V.mp4",
		TargetPath: dst,
		WillMove:   true,
		moveFiles:  false, // copy leg
		Conflicts:  []PlanConflict{{Path: dst, Kind: ConflictFile}},
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts detected")
}

// Force-update organize onto a directory at the target: planConflicts
// (Directory kind, never suppressed) hits the pre-execute join.cds
func TestRestPin_ForceExecuteWhilePlanHasDirectoryConflict(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))

	// Put a DIRECTORY at the video's rendered landing address.
	outDir := filepath.Join(dir, "out")
	dstDir := filepath.Join(outDir, "V")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))
	dst := filepath.Join(dstDir, "V.mp4")
	require.NoError(t, os.MkdirAll(dst, 0o755)) // directory at what must be a file

	org := NewOrganizer(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, nil)
	_, err := org.Organize(t.Context(), OrganizeCmd{
		Match:   models.FileMatchInfo{Path: src, Name: "V.mp4", Extension: ".mp4", MovieID: "V"},
		Movie:   &models.Movie{ID: "V"},
		DestDir: outDir, MoveFiles: true, ForceUpdate: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts detected")
	entries, _ := os.ReadDir(dst)
	assert.Empty(t, entries, "wire-occupied directory's contents stay untouched")
}
