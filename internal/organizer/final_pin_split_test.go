package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

type plainStatFsReadlink struct{ afero.Fs }

// model Wrapping fixture: report didLstat=false and still advertise readlink.
func (p plainStatFsReadlink) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	info, err := p.Fs.Stat(name)
	return info, false, err
}

func (p plainStatFsReadlink) ReadlinkIfPossible(name string) (string, error) {
	if lr, ok := p.Fs.(afero.LinkReader); ok {
		return lr.ReadlinkIfPossible(name)
	}
	return "", os.ErrNotExist
}

// Covers the authorized-link lane refusal leg: classified text does not care
// (destination is not a regular file) never replaced silently.
func TestFinalPin_AuthorizedLinkLaneRefuseSymlink(t *testing.T) {
	dir := t.TempDir()
	base := afero.NewOsFs()
	src := filepath.Join(dir, "in", "X.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	foreign := filepath.Join(dir, "foreign.mp4")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o644))
	dst := filepath.Join(dir, "out", "X.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.Symlink(foreign, dst))
	strategy := newOrganizeStrategy(plainStatFsReadlink{Fs: base}, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "X.mp4", Extension: ".mp4", MovieID: "X"},
		SourcePath: src,
		TargetDir:  filepath.Dir(dst),
		TargetFile: "X.mp4",
		TargetPath: dst,
		WillMove:   true,
		moveFiles:  false, // LinkModeNone copy lane
		Conflicts:  []PlanConflict{},
		// Authorize + no plan conflicts → executes the gate where destMaybeXing
		// sees the non-regular occupant alive.
	}
	plan.LinkMode = LinkModeNone
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	// The symlink remains: no replacement happened.
	info, lerr := os.Lstat(dst)
	require.NoError(t, lerr)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
	_ = dst
}
