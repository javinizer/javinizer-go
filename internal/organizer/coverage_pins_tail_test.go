package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Tail pins cover the leftover uncovered branches of the conflict taxonomy
// (#224 phase-C): preflight conflicts rendered through a plan-level failure,
// in-place inner same-inode aliases, and the authorized link-install Remove
// gate's filesystem-inspection error leg. Each maps to one unique path.

// Naming: for the preflight case the target must be the ALREADY-occupied
// rendered address, i.e. destDir/<movie-ID>/<entry> — the same tree the
// organizer plans into.
func TestTailPin_OrganizeConflictsPreflight(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "A.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "out", "A"), 0o755))
	dst := filepath.Join(dir, "out", "A", "A.mp4")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o644))

	org := NewOrganizer(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
	}, nil, nil)
	_, err := org.Organize(t.Context(), OrganizeCmd{
		Match:       models.FileMatchInfo{Path: src, Name: "A.mp4", Extension: ".mp4", MovieID: "A"},
		Movie:       &models.Movie{ID: "A"},
		DestDir:     filepath.Join(dir, "out"),
		MoveFiles:   true,
		ForceUpdate: false,
	})
	require.Error(t, err)
}

// in-place same-inode lane: TargetPath already aliases SourcePath via a
// hardlink inside the dedicated folder.
func TestTailPin_InPlaceSameInode(t *testing.T) {
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "mixed")
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	src := filepath.Join(oldDir, "ABC.mp4")
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	// hardlink at the renamed unit's address (its name matches the target).
	require.NoError(t, os.Link(src, filepath.Join(oldDir, "ABC-n.mp4")))

	fs := afero.NewOsFs()
	cfg := &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceStrategy(fs, cfg, nil, nil)
	plan := &OrganizePlan{
		Match:               models.FileMatchInfo{Path: src, Name: "ABC.mp4", Extension: ".mp4", MovieID: "ABC"},
		SourcePath:          src,
		OldDir:              oldDir,
		TargetDir:           filepath.Join(dir, "ABC"),
		TargetFile:          "ABC-n.mp4",
		TargetPath:          filepath.Join(dir, "ABC", "ABC-n.mp4"),
		WillMove:            true,
		InPlace:             true,
		IsDedicated:         true,
		Conflicts:           []PlanConflict{},
		overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.NoError(t, err)
}

// norenamefolder: sit the same inode at the rename target so the authorized
// leg no-js its same-inode check.
func TestTailPin_NoRenameFolderSameInode(t *testing.T) {
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "in")
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	src := filepath.Join(oldDir, "L.mp4")
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	require.NoError(t, os.Link(src, filepath.Join(oldDir, "L-renamed.mp4")))

	fs := afero.NewOsFs()
	cfg := &Config{FileFormat: "<ID>-renamed", FolderFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceNoRenameFolderStrategy(fs, cfg, nil, nil)
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "L.mp4", Extension: ".mp4", MovieID: "L"},
		SourcePath: src, TargetDir: oldDir, TargetFile: "L-renamed.mp4",
		TargetPath: filepath.Join(oldDir, "L-renamed.mp4"),
		WillMove:   true, overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.NoError(t, err)
}

// The organize main-move lane same-inode path.
func TestTailPin_OrganizeAuthorizedSameInode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in", "L.mp4")
	dst := filepath.Join(dir, "out", "L.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	require.NoError(t, os.Link(src, dst))

	strategy := newOrganizeStrategy(afero.NewOsFs(), &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "L.mp4", Extension: ".mp4", MovieID: "L"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetFile: "L.mp4", TargetPath: dst,
		WillMove: true, moveFiles: true, overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.NoError(t, err)
}

// Authorized link-install where the fs can't see the target (stat failure):
// the inspect path errors and the install must refuse clearly.
func TestTailPin_AuthorizedLinkInspectError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in", "fail-me.mp4")
	dst := filepath.Join(dir, "out", "fail-me.mp4")
	fs := afero.NewOsFs()
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))

	wrapped := &statFails{Fs: fs} // fails any filename containing fail-me
	strategy := newOrganizeStrategy(wrapped, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "fail-me.mp4", Extension: ".mp4", MovieID: "L"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetFile: "fail-me.mp4", TargetPath: dst,
		WillMove: true, moveFiles: false, LinkMode: LinkModeHard, overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inspect")
}
