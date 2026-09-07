package fsutil

// PR #249 codex P2 follow-up (F1) — the EXDEV partial publish must carry its
// displaced-occupancy evidence THROUGH the ErrPublishCompleted error: the
// bound staged publish landed (and, on an occupied destination, really
// displaced resident bytes) before the source cleanup refused, so dropping
// the DestReplaced answer on that leg erased exactly the audit evidence the
// force-overwrite crumb exists to keep. The pre-build code returned
// replaced=false pre-error and the organizer bailed before warning.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exdevCleanupWedgeFs drives the F1 replay deterministically: the same-volume
// rename of the exact (src, dst) pair answers EXDEV (forcing the cross-device
// leg — stage, bound publish, source cleanup), and exactly the post-publish
// removal of the SOURCE is refused. Unlike the content-sniffing w241 wedge
// this binds by name, so an occupied destination never participates in the
// failure injection.
type exdevCleanupWedgeFs struct {
	afero.Fs
	src, dst     string
	removeFailed bool
}

func (w *exdevCleanupWedgeFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(w.src) && filepath.Clean(newname) == filepath.Clean(w.dst) {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
	}
	return w.Fs.Rename(oldname, newname)
}

func (w *exdevCleanupWedgeFs) Remove(name string) error {
	if filepath.Clean(name) == filepath.Clean(w.src) && !w.removeFailed {
		w.removeFailed = true
		return &os.PathError{Op: "remove", Path: name, Err: syscall.EPERM}
	}
	return w.Fs.Remove(name)
}

func TestMoveFileFsDestReplaced_EXDEVCleanupFailure_KeepsDisplacementEvidence(t *testing.T) {
	t.Run("occupied destination: displaced evidence rides the publish-completed error", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/in/src.txt", []byte("winner-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/out/dst.txt", []byte("resident-bytes"), 0o644))
		fs := &exdevCleanupWedgeFs{Fs: base, src: "/in/src.txt", dst: "/out/dst.txt"}

		replaced, err := MoveFileFsDestReplaced(fs, "/in/src.txt", "/out/dst.txt")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrPublishCompleted,
			"the publish landed before the source cleanup refused — the typed ambiguity rides along")
		require.True(t, fs.removeFailed, "the wedge really fired at the post-publish source removal")
		assert.True(t, replaced,
			"the bound staged publish displaced the resident occupant — its evidence must survive the cleanup failure")

		dst, derr := afero.ReadFile(base, "/out/dst.txt")
		require.NoError(t, derr, "the published destination stands")
		assert.Equal(t, []byte("winner-bytes"), dst, "the resident bytes were really displaced")
		src, serr := afero.ReadFile(base, "/in/src.txt")
		require.NoError(t, serr, "the source is preserved byte-intact (both kept on the ambiguous leg, #224)")
		assert.Equal(t, []byte("winner-bytes"), src)
	})

	t.Run("vacant destination: same error, no displacement evidence", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, "/in/src.txt", []byte("winner-bytes"), 0o644))
		fs := &exdevCleanupWedgeFs{Fs: base, src: "/in/src.txt", dst: "/out/dst.txt"}

		replaced, err := MoveFileFsDestReplaced(fs, "/in/src.txt", "/out/dst.txt")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrPublishCompleted)
		assert.False(t, replaced,
			"the publish displaced nothing — the crumb never fires on unconfirmed evidence, error or not")
		dst, derr := afero.ReadFile(base, "/out/dst.txt")
		require.NoError(t, derr)
		assert.Equal(t, []byte("winner-bytes"), dst)
		src, serr := afero.ReadFile(base, "/in/src.txt")
		require.NoError(t, serr)
		assert.Equal(t, []byte("winner-bytes"), src)
	})
}
