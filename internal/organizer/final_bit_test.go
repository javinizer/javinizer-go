package organizer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Final pin for the IsRegular arm: have a no-follow fallback wrapper report a
// STREAM (fifo)-mode occupancy at the destination after authorization. This
// is handler Live where other pin classes (conflictFile already-silent state) do not fire.
type forceFifoFS struct {
	afero.Fs
	targetName string // the path whose Stat/Lstat calls report ModeNamedPipe
}

func (f forceFifoFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Base(name) == filepath.Base(f.targetName) {
		return &fifoInfo{name: f.targetName}, false, nil
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
	// (never reached — both paths above return)
}
func (f forceFifoFS) Stat(name string) (os.FileInfo, error) {
	if filepath.Base(name) == filepath.Base(f.targetName) {
		return &fifoInfo{name: f.targetName}, nil
	}
	return f.Fs.Stat(name)
}

func (f forceFifoFS) ReadlinkIfPossible(name string) (string, error) {
	return "", os.ErrNotExist
}

type fifoInfo struct{ name string }

func (fi *fifoInfo) Name() string       { return fi.name }
func (fi *fifoInfo) Size() int64        { return 0 }
func (fi *fifoInfo) Mode() os.FileMode  { return os.ModeNamedPipe }
func (fi *fifoInfo) IsDir() bool        { return false }
func (fi *fifoInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (fi *fifoInfo) Sys() any           { return nil }

// Pin the authorized Remove gate's non-regular refusal arm (the exit today,
// mistake strategy descended eng fee where state exists).
func TestFinalPin_AuthorizedRemoveOnFifoRefused(t *testing.T) {
	dir := t.TempDir()
	base := afero.NewOsFs()
	src := filepath.Join(dir, "in", "X.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	dst := filepath.Join(dir, "out", "X.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o644))

	wrapped := forceFifoFS{Fs: base, targetName: dst}
	strategy := newOrganizeStrategy(wrapped, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:               models.FileMatchInfo{Path: src, Name: "X.mp4", Extension: ".mp4", MovieID: "X"},
		SourcePath:          src,
		TargetDir:           filepath.Dir(dst),
		TargetFile:          "X.mp4",
		TargetPath:          dst,
		WillMove:            true,
		moveFiles:           false,
		LinkMode:            LinkModeHard,
		overwriteAuthorized: true,
		Conflicts:           []PlanConflict{},
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	require.Contains(t, err.Error(), "regular file")
}
