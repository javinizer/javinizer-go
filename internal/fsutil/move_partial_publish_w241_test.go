package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exdevCleanupFailFs replays the PR #241 P1 partial-publish ambiguity on a
// virtual filesystem, cross-platform: the same-volume rename of the exact
// (src, dst) pair answers EXDEV so the move degrades to its cross-device leg
// (stage, bound publish, source cleanup), and the first removal of the
// now-non-empty source fails — the post-publish cleanup wedge. Destination
// publication has already succeeded at that point, so BOTH objects must be
// preserved byte-intact and the error must carry the typed publish-completed
// marker. The cleanup of empty bookkeeping objects (if any) passes through
// untouched.
type exdevCleanupFailFs struct {
	afero.Fs
	src, dst string
	failed   bool
}

func (p *exdevCleanupFailFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(p.src) && filepath.Clean(newname) == filepath.Clean(p.dst) {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
	}
	return p.Fs.Rename(oldname, newname)
}

func (p *exdevCleanupFailFs) Remove(name string) error {
	if !p.failed {
		if info, err := p.Fs.Stat(name); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			p.failed = true
			return &os.PathError{Op: "remove", Path: name, Err: syscall.EPERM}
		}
	}
	return p.Fs.Remove(name)
}

// TestMoveFileFs_EXDEVCleanupFailure_PublishCompleted pins the PR #241 P1
// typing on the REPLACE-semantics move family: a cross-device move whose
// publish landed but whose source remove failed surfaces the SAME typed
// ambiguity the no-replace lineage already wraps (ErrPublishCompleted) —
// callers classifying "did the destination get my bytes?" must never treat
// this leg as a pre-publish no-op.
func TestMoveFileFs_EXDEVCleanupFailure_PublishCompleted(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(base, "/in/movie.mkv", []byte("owner-bytes"), 0o644))
	fs := &exdevCleanupFailFs{Fs: base, src: "/in/movie.mkv", dst: "/out/movie.mkv"}

	err := MoveFileFs(fs, "/in/movie.mkv", "/out/movie.mkv")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPublishCompleted,
		"the destination was published before the source cleanup failed — the typed ambiguity must ride along")

	dst, derr := afero.ReadFile(base, "/out/movie.mkv")
	require.NoError(t, derr, "the published destination stands")
	assert.Equal(t, []byte("owner-bytes"), dst)
	src, serr := afero.ReadFile(base, "/in/movie.mkv")
	require.NoError(t, serr, "the source is preserved byte-intact (both kept on the ambiguous leg)")
	assert.Equal(t, []byte("owner-bytes"), src)
}

// TestMoveFileFs_PrePublishFailure_NoPublishCompleted pins the disjoint
// class: a rename refusal BEFORE any publication carries NO publish-completed
// marker — callers keep treating it as "destination untouched".
func TestMoveFileFs_PrePublishFailure_NoPublishCompleted(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(base, "/in/movie.mkv", []byte("owner-bytes"), 0o644))
	fs := &exdevIOFailFs{Fs: base, src: "/in/movie.mkv", dst: "/out/movie.mkv"}

	err := MoveFileFs(fs, "/in/movie.mkv", "/out/movie.mkv")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrPublishCompleted, "a pre-publication refusal installed nothing")
	exists, _ := afero.Exists(base, "/out/movie.mkv")
	assert.False(t, exists, "the destination stays absent")
}

// exdevIOFailFs fails the same-volume rename of the exact pair with a plain
// I/O error (not the cross-device class): the move never degrades, nothing is
// published.
type exdevIOFailFs struct {
	afero.Fs
	src, dst string
}

func (p *exdevIOFailFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(p.src) && filepath.Clean(newname) == filepath.Clean(p.dst) {
		return &os.PathError{Op: "rename", Path: oldname, Err: syscall.EIO}
	}
	return p.Fs.Rename(oldname, newname)
}
