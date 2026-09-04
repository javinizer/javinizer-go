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

// MorePin* pins: execution no-op lanes on each strategy leg + the link
// inspect-error branch (#224 coverage sweep).

func sameInodePlan(dir string) *OrganizePlan {
	src := filepath.Join(dir, "in", "L.mp4")
	dst := filepath.Join(dir, "out", "L.mp4")
	return &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "L.mp4", Extension: ".mp4", MovieID: "L"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetPath: dst, TargetFile: "L.mp4",
		WillMove: true, moveFiles: true, overwriteAuthorized: true,
		Conflicts: []PlanConflict{},
	}
}

func TestMorePin_OrganizeSameInodeLane(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "L.mp4")
	dst := filepath.Join(dir, "out", "L.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	require.NoError(t, os.Link(src, dst))
	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	res, err := strategy.Execute(sameInodePlan(dir))
	require.NoError(t, err)
	_ = res
	content, _ := os.ReadFile(src)
	assert.Equal(t, "v", string(content), "alias no-op keeps both names")
}

func TestMorePin_InPlaceNoRenameSameInodeLane(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in", "L.mp4")
	dst := filepath.Join(dir, "out", "L.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	require.NoError(t, os.Link(src, dst))
	cfg := &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceNoRenameFolderStrategy(afero.NewOsFs(), cfg, nil, nil)
	res, err := strategy.Execute(sameInodePlan(dir))
	require.NoError(t, err)
	_ = res
}

func TestMorePin_IsDirAtPlanConflict(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out", 0755))
	target := "/out/dir-abc.mp4"
	require.NoError(t, fs.MkdirAll(target, 0755))
	conflicts := checkTargetConflict(fs, "/in/x.mp4", target, false, true)
	require.Len(t, conflicts, 1)
	assert.Equal(t, ConflictDirectory, conflicts[0].Kind)
}
