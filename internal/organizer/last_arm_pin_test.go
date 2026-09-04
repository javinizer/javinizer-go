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

type verifyInfo struct{ name string }

func (v *verifyInfo) Name() string       { return v.name }
func (v *verifyInfo) Size() int64        { return 0 }
func (v *verifyInfo) Mode() os.FileMode  { return os.ModeNamedPipe }
func (v *verifyInfo) IsDir() bool        { return false }
func (v *verifyInfo) Sys() any           { return nil }
func (v *verifyInfo) ModTime() time.Time { return time.Unix(0, 0) }

// The last-arm fs: Lstat reports fifo on the destination; readlink says none.
type lastArmFs struct {
	afero.Fs
	pick string
}

func (f lastArmFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Clean(name) == filepath.Clean(f.pick) {
		return &verifyInfo{name: name}, false, nil
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f lastArmFs) ReadlinkIfPossible(name string) (string, error) {
	return "", os.ErrNotExist
}

func TestLastArm_AuthorizeCopyFifoInspErr(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "X.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	dst := filepath.Join(dir, "out", "X.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))

	wrapper := lastArmFs{Fs: fs, pick: dst}
	strategy := newOrganizeStrategy(wrapper, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "X.mp4", Extension: ".mp4", MovieID: "X"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetFile: "X.mp4",
		TargetPath: dst, WillMove: true, moveFiles: false,
		LinkMode:            LinkModeNone,
		overwriteAuthorized: true,
		Conflicts:           []PlanConflict{},
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	require.Contains(t, err.Error(), "regular file")
}
