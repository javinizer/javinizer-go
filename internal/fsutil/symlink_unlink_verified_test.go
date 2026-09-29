package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// symlinkHarness lays down a real link object on an OsFs temp volume and
// skips where the platform cannot create one (unprivileged Windows).
func symlinkHarness(t *testing.T) (afero.Fs, string, string, string) {
	t.Helper()
	base := afero.NewOsFs()
	root := t.TempDir()
	target := filepath.Join(root, "media", "movie.mkv")
	link := filepath.Join(root, "library", "movie.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("video payload"), 0o644))
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unsupported here: %v", err)
	}
	return base, root, target, link
}

func readlinkOS(t *testing.T, link string) string {
	t.Helper()
	target, err := os.Readlink(link)
	require.NoError(t, err)
	return target
}

func TestUnlinkSymlinkVerified_RemovesThePinnedLink(t *testing.T) {
	base, root, target, link := symlinkHarness(t)

	require.NoError(t, UnlinkSymlinkVerified(base, link, target))

	_, statErr := os.Lstat(link)
	assert.True(t, os.IsNotExist(statErr), "the pinned link object is removed")
	assert.FileExists(t, target, "the link TARGET is never touched")
	entries, readErr := os.ReadDir(filepath.Join(root, "library"))
	require.NoError(t, readErr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".vac.", "no terminal litter remains")
	}
}

func TestUnlinkSymlinkVerified_ForeignPayloadRewoundByteIntact(t *testing.T) {
	base, root, target, link := symlinkHarness(t)
	foreign := filepath.Join(root, "media", "other.mkv")
	require.NoError(t, os.WriteFile(foreign, []byte("other"), 0o644))
	foreignLink := filepath.Join(root, "library", "foreign.mkv")
	if err := os.Symlink(foreign, foreignLink); err != nil {
		t.Skipf("symlink creation unsupported here: %v", err)
	}

	err := UnlinkSymlinkVerified(base, foreignLink, target)
	require.ErrorIs(t, err, ErrTakeAsideForeign, "a link whose readback payload differs from the pin refuses")
	assert.Equal(t, foreign, readlinkOS(t, foreignLink), "the foreign occupant rides back byte-intact onto its name")
	_, statErr := os.Lstat(foreignLink)
	assert.NoError(t, statErr)
	entries, readErr := os.ReadDir(filepath.Join(root, "library"))
	require.NoError(t, readErr)
	terminals := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".vac.") {
			terminals++
		}
	}
	assert.Zero(t, terminals, "the rewind repoints the terminal, leaving no residue")
	_ = link
}

func TestUnlinkSymlinkVerified_RegularOccupantRewoundByteIntact(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	name := filepath.Join(root, "movie.mkv")
	require.NoError(t, os.WriteFile(name, []byte("foreign bytes"), 0o644))

	err := UnlinkSymlinkVerified(base, name, filepath.Join(root, "elsewhere.mkv"))
	require.ErrorIs(t, err, ErrTakeAsideForeign, "a regular file is not the pinned link object")
	got, readErr := os.ReadFile(name)
	require.NoError(t, readErr)
	assert.Equal(t, "foreign bytes", string(got), "the regular occupant survives the rewind untouched")
}

func TestUnlinkSymlinkVerified_AbsentNameClassifiesVanished(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	err := UnlinkSymlinkVerified(base, filepath.Join(root, "absent.mkv"), "whatever-target")
	require.ErrorIs(t, err, ErrTakeAsideVanished, "an already-consumed name classifies exactly like the regular-file twin")
}

// lstaterOnlyFsSymlink wraps an OsFs exposing TRUE no-follow Lstat but no
// LinkReader: the vacate must take the virtual (fs-surface) leg and the
// terminal re-auth must refuse on the missing link model, rewinding.
type lstaterOnlyFsSymlink struct {
	afero.Fs
}

func (f lstaterOnlyFsSymlink) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func TestUnlinkSymlinkVerified_NoLinkModelRetains(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	wrapped := lstaterOnlyFsSymlink{Fs: base}

	err := UnlinkSymlinkVerified(wrapped, link, target)
	require.ErrorIs(t, err, ErrTakeAsideForeign, "an unprovable link model never authorizes a remove")
	assert.Equal(t, target, readlinkOS(t, link), "the link object rides back onto its name")
}

// readlinkFailSymlinkFs is a LinkReader whose readback always refuses. It
// forwards a TRUE no-follow Lstat so the terminal re-auth reaches the
// readback leg instead of deciding on the through-link Stat.
type readlinkFailSymlinkFs struct {
	afero.Fs
	err error
}

func (f readlinkFailSymlinkFs) ReadlinkIfPossible(string) (string, error) {
	return "", f.err
}

func (f readlinkFailSymlinkFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func TestUnlinkSymlinkVerified_ReadlinkFaultRewinds(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	sentinel := errors.New("readlink wedged")
	err := UnlinkSymlinkVerified(readlinkFailSymlinkFs{Fs: base, err: sentinel}, link, target)
	require.ErrorIs(t, err, sentinel)
	require.NotErrorIs(t, err, ErrTakeAsideVanished, "a read failure is doubt, not consumption")
	assert.Equal(t, target, readlinkOS(t, link), "the object rides back onto its name")
}

func TestUnlinkSymlinkVerified_ReadlinkNotExistClassifiesVanished(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	err := UnlinkSymlinkVerified(readlinkFailSymlinkFs{Fs: base, err: os.ErrNotExist}, link, target)
	require.ErrorIs(t, err, ErrTakeAsideVanished, "a terminal gone at the re-auth consumed itself")
	_ = target
}

// vacNameRemoveFailSymlinkFs wedges the terminal Remove: the object must ride
// back onto its name and the remove error surfaces (never silently consumed).
// It forwards a true no-follow Lstat and the readlink pass-through so the
// re-auth reaches the remove leg.
type vacNameRemoveFailSymlinkFs struct {
	afero.Fs
	err  error
	done bool
}

func (f *vacNameRemoveFailSymlinkFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *vacNameRemoveFailSymlinkFs) ReadlinkIfPossible(name string) (string, error) {
	return f.Fs.(afero.LinkReader).ReadlinkIfPossible(name)
}

func (f *vacNameRemoveFailSymlinkFs) Remove(name string) error {
	// Fire only on the TERMINAL remove (the vac name holding the vacated
	// symlink object): the claim's release removes the same name while it
	// still holds the regular claim file, and that leg must succeed for the
	// terminal remove to run at all.
	if strings.Contains(name, ".vac.") {
		info, lerr := lstatBaseSymlink(f.Fs, name)
		if lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			f.done = true
			return f.err
		}
	}
	return f.Fs.Remove(name)
}

func lstatBaseSymlink(fs afero.Fs, name string) (os.FileInfo, error) {
	if lst, ok := fs.(afero.Lstater); ok {
		info, _, err := lst.LstatIfPossible(name)
		return info, err
	}
	return fs.Stat(name)
}

func TestUnlinkSymlinkVerified_TerminalRemoveFaultRewinds(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	sentinel := errors.New("remove wedged")
	fs := &vacNameRemoveFailSymlinkFs{Fs: base, err: sentinel}
	err := UnlinkSymlinkVerified(fs, link, target)
	require.ErrorIs(t, err, sentinel)
	require.True(t, fs.done, "the terminal remove actually ran")
	assert.Equal(t, target, readlinkOS(t, link), "the object rides back after the wedged remove")
}

// vacNameRemoveVanishSymlinkFs answers the terminal Remove with NotExist:
// the object vanished unownably — the consumed class, no rewind.
func TestUnlinkSymlinkVerified_TerminalRemoveVanishConsumes(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	fs := &vacNameRemoveFailSymlinkFs{Fs: base, err: os.ErrNotExist}
	err := UnlinkSymlinkVerified(fs, link, target)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	require.True(t, fs.done)
	_ = target
}

// statFailAfterVacateSymlinkFs refuses the terminal's post-vacate Lstat (the
// claim/release probes run through the open handle, not Lstat, so arming on
// the vacate Rename is precise).
type statFailAfterVacateSymlinkFs struct {
	afero.Fs
	name  string
	err   error
	armed bool
}

func (f *statFailAfterVacateSymlinkFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(f.name) {
		f.armed = true
	}
	return f.Fs.Rename(oldname, newname)
}

func (f *statFailAfterVacateSymlinkFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.armed && strings.Contains(name, ".vac.") {
		return nil, true, f.err
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func TestUnlinkSymlinkVerified_TerminalStatFaultRewinds(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	sentinel := errors.New("lstat wedged")
	fs := &statFailAfterVacateSymlinkFs{Fs: base, name: link, err: sentinel}
	err := UnlinkSymlinkVerified(fs, link, target)
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, target, readlinkOS(t, link), "an indeterminate terminal rides back — nothing removed on doubt")
}

// vacateVanishSymlinkFs makes the vacate answer ENOENT exactly like a racer
// that consumed the entry first (the vacateVanishFs pattern).
type vacateVanishSymlinkFs struct {
	afero.Fs
	target string
	done   bool
}

func (f *vacateVanishSymlinkFs) Rename(oldname, newname string) error {
	if !f.done && filepath.Clean(oldname) == filepath.Clean(f.target) {
		f.done = true
		if rmErr := f.Fs.Remove(oldname); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
		return &os.PathError{Op: "rename", Path: oldname, Err: os.ErrNotExist}
	}
	return f.Fs.Rename(oldname, newname)
}

func TestUnlinkSymlinkVerified_VacateVanishConsumes(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	// The wrapped fs is not an *afero.OsFs, so the vacate takes the virtual
	// fs.Rename surface — exactly the leg the hook plays.
	fs := &vacateVanishSymlinkFs{Fs: lstaterOnlyFsSymlink{Fs: base}, target: link}
	err := UnlinkSymlinkVerified(fs, link, target)
	require.ErrorIs(t, err, ErrTakeAsideVanished, "the racer consumed the pinned entry — class shared with the regular-file twin")
	require.True(t, fs.done)
	_ = target
}

func TestUnlinkSymlinkVerified_ClaimFaultRefuses(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	sentinel := errors.New("claim wedged")
	prev := takeAsideVacRandReader
	takeAsideVacRandReader = &w43FailReader{err: sentinel}
	t.Cleanup(func() { takeAsideVacRandReader = prev })

	err := UnlinkSymlinkVerified(base, link, target)
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, target, readlinkOS(t, link), "a terminal-claim fault never touches the name")
}

func TestSymlinkTargetsEqual_Contract(t *testing.T) {
	assert.True(t, SymlinkTargetsEqual("/a/b.mkv", "/a/b.mkv"), "byte equality")
	assert.True(t, SymlinkTargetsEqual("/a/./b.mkv", "/a/b.mkv"), "clean-equal spellings name the same object")
	assert.False(t, SymlinkTargetsEqual("/a/b.mkv", "/a/c.mkv"))
	assert.False(t, SymlinkTargetsEqual("/a/b.mkv", "/A/b.mkv"), "case drift is substantive")
}

func TestReadlinkNoFollow(t *testing.T) {
	t.Run("no link model", func(t *testing.T) {
		base := afero.NewMemMapFs()
		_, ok, err := ReadlinkNoFollow(base, "/x")
		require.NoError(t, err)
		assert.False(t, ok, "memfs exposes no LinkReader — callers must retain")
	})
	t.Run("osfs real link", func(t *testing.T) {
		base, _, target, link := symlinkHarness(t)
		got, ok, err := ReadlinkNoFollow(base, link)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, target, got)
	})
	t.Run("osfs non-link errors", func(t *testing.T) {
		base, _, target, _ := symlinkHarness(t)
		_, ok, err := ReadlinkNoFollow(base, target)
		assert.True(t, ok)
		assert.Error(t, err, "readlink of a regular file fails — callers retain on doubt")
	})
}

func TestVacateLinkObject_MovesLinkNotTarget(t *testing.T) {
	base, root, target, link := symlinkHarness(t)
	terminal := filepath.Join(root, "library", "terminal.bin")

	require.NoError(t, vacateLinkObjectNoReplace(base, link, terminal))

	_, statErr := os.Lstat(link)
	assert.True(t, os.IsNotExist(statErr), "the link name vacated")
	assert.Equal(t, target, readlinkOS(t, terminal), "the TERMINAL carries the link object itself — never its target")
	info, lerr := os.Lstat(terminal)
	require.NoError(t, lerr)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the terminal is a symlink object")
	assert.FileExists(t, target)
}

func TestVacateLinkObject_OccupiedTerminalCollides(t *testing.T) {
	base, root, target, link := symlinkHarness(t)
	terminal := filepath.Join(root, "library", "terminal.bin")
	require.NoError(t, os.WriteFile(terminal, []byte("claimed"), 0o644))

	err := vacateLinkObjectNoReplace(base, link, terminal)
	require.ErrorIs(t, err, ErrPublishCollision)
	got, readErr := os.ReadFile(terminal)
	require.NoError(t, readErr)
	assert.Equal(t, "claimed", string(got), "the occupied terminal is never replaced")
	assert.Equal(t, target, readlinkOS(t, link), "the name keeps its link object")
}

func TestVacateLinkObject_AbsentNameNotExist(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	err := vacateLinkObjectNoReplace(base, filepath.Join(root, "absent"), filepath.Join(root, "terminal"))
	assert.True(t, errors.Is(err, os.ErrNotExist), "a vanished name surfaces the not-exist class for the caller's mapping")
}

// --- Remaining bound-unlink doubt legs: release refusal, rewind double
// fault, refused vacate, and the terminal-vanished-mid-flight class. ---

// vacNameLstatFailFs fails every no-follow Lstat of the claim name: the
// release's syscall-adjacency rebind answers "indeterminate", so the unlink
// refuses BEFORE touching the pinned name.
type vacNameLstatFailFs struct {
	afero.Fs
	err error
}

func (f *vacNameLstatFailFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if strings.Contains(name, ".vac.") {
		return nil, true, f.err
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func TestUnlinkSymlinkVerified_ClaimReleaseFaultRefuses(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	sentinel := errors.New("claim rebind indeterminate")
	err := UnlinkSymlinkVerified(&vacNameLstatFailFs{Fs: base, err: sentinel}, link, target)
	require.ErrorIs(t, err, sentinel)
	got, readErr := os.Readlink(link)
	require.NoError(t, readErr, "a release doubt never touches the pinned name")
	assert.Equal(t, target, got)
	entries, _ := os.ReadDir(filepath.Dir(link))
	strays := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".vac.") {
			strays++
		}
	}
	assert.Equal(t, 1, strays, "the unproven claim is RETAINED for manual cleanup (the wave-r19 posture) — never a blind pathname remove of a maybe-foreign occupant")
}

// rewindVetoFs vacates forward but vetoes the ride-back rename: any doubt
// then reports the joined restore-failure class with the terminal left
// recoverable (the regular-file twin's ErrTakeAsideRestoreFailed posture).
type rewindVetoFs struct {
	afero.Fs
	name string
	err  error
}

func (f *rewindVetoFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *rewindVetoFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.name) {
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

func TestUnlinkSymlinkVerified_RewindFailureJoinsRestoreClass(t *testing.T) {
	base, root, target, _ := symlinkHarness(t)
	// A REGULAR occupant vacates fine, then fails the symlink re-auth; the
	// vetoed rewind strands it at the terminal instead of restoring.
	name := filepath.Join(root, "library", "regular.mkv")
	require.NoError(t, os.WriteFile(name, []byte("foreign regular"), 0o644))
	sentinel := errors.New("rewind vetoed")
	fs := &rewindVetoFs{Fs: base, name: name, err: sentinel}

	err := UnlinkSymlinkVerified(fs, name, target)
	require.ErrorIs(t, err, ErrTakeAsideRestoreFailed)
	assert.Contains(t, err.Error(), "rewind vetoed", "the rewind cause narrates the failure (the join carries it textually like rerideBoundUnlink)")
	entries, readErr := os.ReadDir(filepath.Dir(name))
	require.NoError(t, readErr)
	terminals := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".vac.") {
			terminals++
		}
	}
	assert.Equal(t, 1, terminals, "the occupant stays recoverable at the terminal name (never deleted)")
	assert.Empty(t, func() string { b, _ := os.ReadFile(name); return string(b) }(), "the original name is not restored under the veto")
}

// vacateRefuseFs refuses EVERY rename non-NotExist: the vacate reports the
// refused class and the occupant stays byte-intact at its name.
type vacateRefuseFs struct {
	afero.Fs
	err error
}

func (f *vacateRefuseFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *vacateRefuseFs) Rename(string, string) error { return f.err }

func TestUnlinkSymlinkVerified_VacateRefusalRetains(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	sentinel := errors.New("vacate wedged")
	err := UnlinkSymlinkVerified(&vacateRefuseFs{Fs: base, err: sentinel}, link, target)
	require.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "occupant preserved byte-intact")
	require.NotErrorIs(t, err, ErrTakeAsideVanished, "a refused vacate is doubt, never consumption")
	got, readErr := os.Readlink(link)
	require.NoError(t, readErr)
	assert.Equal(t, target, got)
}

// terminalVanishFs completes the vacate then drops the terminal: the
// post-vacate proof finds nothing — the vanished class consumes (a racer's
// removal is the outcome the unlink exists for).
type terminalVanishFs struct {
	afero.Fs
	name string
}

func (f *terminalVanishFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && filepath.Clean(oldname) == filepath.Clean(f.name) {
		_ = f.Fs.Remove(newname)
	}
	return err
}

func TestUnlinkSymlinkVerified_TerminalEmptyAfterVacateConsumes(t *testing.T) {
	base, _, target, link := symlinkHarness(t)
	err := UnlinkSymlinkVerified(&terminalVanishFs{Fs: base, name: link}, link, target)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	_ = target
}
