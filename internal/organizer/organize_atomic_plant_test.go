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

// Authorized leg parity: a dedicated-folder in-place rename under
// overwriteAuthorized keeps the plain inner rename (the unauthorized window
// is covered by the no-replace leg; this proves the leg flip is reachable).
func TestInPlaceStrategy_AuthorizedInnerRenameUsesPlainRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source/mixed", 0777))
	require.NoError(t, afero.WriteFile(fs, "/source/mixed/ABC-123.mp4", []byte("video"), 0644))
	// Occupy the inner target so ONLY the replace-capable (authorized) inner
	// rename can succeed.
	require.NoError(t, afero.WriteFile(fs, "/source/mixed/ABC-123-KEEP.mp4", []byte("prior"), 0644))

	cfg := &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceStrategy(fs, cfg, nil, nil)

	// TargetFile DIFFERS from the sitting name so the inner rename leg runs.
	plan := &OrganizePlan{
		SourcePath:          "/source/mixed/ABC-123.mp4",
		TargetDir:           "/source/ABC-123",
		TargetPath:          "/source/ABC-123/ABC-123-new.mp4",
		TargetFile:          "ABC-123-new.mp4",
		OldDir:              "/source/mixed",
		WillMove:            true,
		InPlace:             true,
		IsDedicated:         true,
		Match:               models.FileMatchInfo{MovieID: "ABC-123", Path: "/source/mixed/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4"},
		overwriteAuthorized: true,
		Conflicts:           []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	content, rerr := afero.ReadFile(fs, "/source/ABC-123/ABC-123-new.mp4")
	require.NoError(t, rerr)
	assert.Equal(t, "video", string(content))
}
