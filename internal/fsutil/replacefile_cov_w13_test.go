package fsutil

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Wave-13 (Windows CI w28 restore-rename): ReplaceFile's virtual-FS leg
// formatted the filesystem error with %v, so the takeover-restore wrap chain
// surfaced "restore replacement busy marker: replace existing destination
// unsupported: restore rename wedged" as TEXT while errors.Is(err, wedge)
// failed. These tests drive the wedge through that exact chain on the host.

type w13RenameWedgeFs struct {
	afero.Fs
	err error
}

func (f *w13RenameWedgeFs) Rename(_, _ string) error { return f.err }

// The non-OsFs ReplaceFile leg (shared across build tags via
// replaceFileVirtualFallback) must keep BOTH sentinels unwrap-reachable:
// the refusal classification and the filesystem's original wedge.
func TestReplaceFileW13_VirtualFallbackKeepsBothErrorsUnwrappable(t *testing.T) {
	wedge := errors.New("restore rename wedged")
	fs := &w13RenameWedgeFs{Fs: afero.NewMemMapFs(), err: wedge}

	err := replaceFileVirtualFallback(fs, "/out/staged.rstr.3", "/out/poster.jpg.dlbusy")
	require.Error(t, err)
	require.ErrorIs(t, err, wedge, "the filesystem wedge stays an unwrap target (wave-12 %v dropped it)")
	require.ErrorIs(t, err, ErrReplaceUnsupported, "the refusal classification joins the same chain")
	require.Equal(t,
		"replace existing destination unsupported: restore rename wedged",
		err.Error(), "both layers' text survives, matching the observed Windows CI chain wording")
}

// The fallback's success leg keeps virtual-FS rename semantics: the staged
// source lands on the destination and the source disappears.
func TestReplaceFileW13_VirtualFallbackSuccessReplacesDestination(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/out/staged.rstr.3", []byte("staged"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/out/poster.jpg", []byte("old"), 0o644))

	require.NoError(t, replaceFileVirtualFallback(fs, "/out/staged.rstr.3", "/out/poster.jpg"))
	got, err := afero.ReadFile(fs, "/out/poster.jpg")
	require.NoError(t, err)
	require.Equal(t, []byte("staged"), got)
	_, err = fs.Stat("/out/staged.rstr.3")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// End-to-end through the takeover-restore leg: the rename wedge must remain
// errors.Is-reachable behind "restore replacement busy marker: ..." on every
// platform (POSIX raw rename wraps directly; Windows routes the same wedge
// through replaceFileVirtualFallback first).
func TestReplacementBusyW13_RestoreRenameWedgeSurvivesEveryWrapLayer(t *testing.T) {
	base, path, takeover, content := newW28TakeoverFixture(t, false)
	wedge := errors.New("restore rename wedged")
	fs := &w28RenameFailureFs{Fs: base, oldPath: takeover, newPath: path, err: wedge}

	err := replacementBusyReturnTakeover(fs, path, takeover, content, w28TakeoverIdentity(t, base, takeover))
	require.ErrorIs(t, err, wedge, "the wedge stays unwrap-reachable through every intermediate wrap layer")
	require.Contains(t, err.Error(), "restore replacement busy marker", "the takeover-restore layer labels the chain")
	require.Contains(t, err.Error(), "restore rename wedged", "the wedge text is preserved, not replaced")
}
