package fsutil

// POSTER-WRITE-HARDENING wave-59 (codex P2, PR#215 finding F1) — the
// verify→Remove pathname window in releaseClaimedBusyObject is closed by
// delegating to the wave-44 bound-unlink construction (BoundAside.Unlink):
// the verified object vacates onto a fresh claimed terminal name NO-REPLACE,
// is re-bound to the observed identity there, and only the terminal name is
// unlinked. A foreign swap landing inside the verify→unlink window is caught
// at the terminal re-bind and refused typed (ErrTakeAsideForeign), the
// occupant rewound onto the claimed name byte-intact — never deleted by a
// pathname Remove. This test replays that swap on the REAL OsFs (dev/inode
// identity distinguishes the plant from the verified object) through the
// vacate rename, the one syscall a path-based verify→Remove pair could never
// bind.

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w59SwapOnVacateRenameFs replays a foreign swap onto the claimed name
// BETWEEN the bound-unlink's #1 no-follow re-prove (which passed) and the
// no-replace vacate rename: the rename moves the PLANT onto the fresh
// terminal name, the terminal identity re-bind catches it, and the
// preservational ride-back rewinds the plant onto the claimed name — never
// deleted.
type w59SwapOnVacateRenameFs struct {
	afero.Fs
	scratch string
	plant   []byte
	done    bool
}

func (f *w59SwapOnVacateRenameFs) Rename(oldname, newname string) error {
	if !f.done && oldname == f.scratch && strings.Contains(newname, ".vac.") {
		f.done = true
		foreign := f.scratch + ".foreign"
		if err := afero.WriteFile(f.Fs, foreign, f.plant, 0o600); err != nil {
			return err
		}
		if err := f.Fs.Rename(foreign, oldname); err != nil { // foreign now at the claimed name
			return err
		}
	}
	return f.Fs.Rename(oldname, newname)
}

// w59TerminalRemoveFailFs arms the bound unlink's terminal name through the
// vacate rename REGARDLESS of the moved object's size (releaseClaimedBusyObject
// unlinks 0-byte placeholders too — w44TerminalRemoveFailFs's size>0 gate
// excludes the w44 fixture's own 0-byte reservation, not this flow's object)
// and fails the first armed Remove. Mirrors w44TerminalRemoveFailFs's shape.
type w59TerminalRemoveFailFs struct {
	afero.Fs
	err   error
	fail  int
	armed atomic.Value // string: the vacate-armed terminal name
	fails int
}

func (f *w59TerminalRemoveFailFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, ".vac.") {
		f.armed.Store(newname)
	}
	return err
}

func (f *w59TerminalRemoveFailFs) Remove(name string) error {
	if name == f.armed.Load() && (f.fail < 0 || f.fails < f.fail) {
		f.fails++
		return f.err
	}
	return f.Fs.Remove(name)
}

// A swap in the verify→unlink window is refused typed and the foreign plant
// keeps its bytes byte-intact at the claimed name — pre-wave-59 the pathname
// Remove deleted it.
func TestReleaseClaimedBusyObjectW59_SwapInVerifyUnlinkWindowPreserved(t *testing.T) {
	base := afero.NewOsFs()
	dir := t.TempDir()
	path := filepath.Join(dir, "claimed")
	require.NoError(t, afero.WriteFile(base, path, []byte("genuine claimed bytes"), 0o600))
	info, err := base.Stat(path)
	require.NoError(t, err)

	plant := []byte("foreign-plant-bytes")
	fs := &w59SwapOnVacateRenameFs{Fs: base, scratch: path, plant: plant}

	err = releaseClaimedBusyObject(fs, path, info)
	require.ErrorIs(t, err, ErrTakeAsideForeign,
		"a swap in the verify→unlink window is refused, never deleted")
	require.True(t, fs.done, "the vacate-rename wedge must have fired")
	got, rerr := afero.ReadFile(base, path)
	require.NoError(t, rerr)
	require.Equal(t, plant, got,
		"the foreign plant keeps its bytes byte-intact at the claimed name — the wave-59 bound unlink never deleted it")
}
