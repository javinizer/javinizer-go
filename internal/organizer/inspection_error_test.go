package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Wrapper whose target lookups fail with a non-NotExist error. The
// authorized copy lane must propagate it rather than skip the gate and
// blindly copy (codex r6 P2).
type breakLookupFs struct{ afero.Fs }

func (b breakLookupFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Base(name) == "fragile.mp4" {
		return nil, false, os.ErrPermission
	}
	info, err := b.Fs.Stat(name)
	return info, false, err
}

func (b breakLookupFs) Stat(name string) (os.FileInfo, error) {
	if filepath.Base(name) == "fragile.mp4" {
		return nil, os.ErrPermission
	}
	return b.Fs.Stat(name)
}

func TestInspectionErr_AuthorizedCopyFails(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "fragile.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	dst := filepath.Join(dir, "out", "fragile.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o644))

	fs = breakLookupFs{Fs: fs}
	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:               models.FileMatchInfo{Path: src, Name: "fragile.mp4", Extension: ".mp4", MovieID: "BOOM"},
		SourcePath:          src,
		TargetDir:           filepath.Dir(dst),
		TargetFile:          "fragile.mp4",
		TargetPath:          dst,
		WillMove:            true,
		moveFiles:           false,
		LinkMode:            LinkModeNone,
		overwriteAuthorized: true,
		Conflicts:           []PlanConflict{},
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inspect")
}
