package history

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// vacClaimHookFs detects the bound-unlink terminal claim — the O_EXCL create
// of target+".vac.<token>" inside fsutil.UnlinkVerified — and fires hook
// exactly once. That claim is the first filesystem mutation after the pinned
// file's hash window closed, so the hook replays the hash→unlink TOCTOU
// window: a race that lands anywhere between the digest and the vacate lands
// no later than this claim.
type vacClaimHookFs struct {
	afero.Fs
	target string
	hook   func()
	done   bool
}

func (f *vacClaimHookFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if !f.done && flag&os.O_CREATE != 0 && flag&os.O_EXCL != 0 && strings.HasPrefix(name, f.target+".vac.") {
		f.done = true
		f.hook()
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// vacateVanishFs drops the pinned name the moment the verified unlink's
// vacate rename reaches it (fsutil.PublishNoReplace's virtual leg renames
// through the fs surface): the vacate then answers ENOENT exactly like a
// racer that consumed the entry first.
type vacateVanishFs struct {
	afero.Fs
	target string
	done   bool
}

func (f *vacateVanishFs) Rename(oldname, newname string) error {
	if !f.done && filepath.Clean(oldname) == filepath.Clean(f.target) {
		f.done = true
		if rmErr := f.Fs.Remove(oldname); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
		return &os.PathError{Op: "rename", Path: oldname, Err: os.ErrNotExist}
	}
	return f.Fs.Rename(oldname, newname)
}

// errPinnedStatFile turns the pinned identity capture off the open handle
// into a fault: with nothing provable to bind, the removal must refuse.
type errPinnedStatFile struct {
	afero.File
	err error
}

func (f *errPinnedStatFile) Stat() (os.FileInfo, error) { return nil, f.err }

type pinnedStatDenyFs struct {
	afero.Fs
	path string
	err  error
}

func (f *pinnedStatDenyFs) Open(name string) (afero.File, error) {
	fh, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(name) == filepath.Clean(f.path) {
		return &errPinnedStatFile{File: fh, err: f.err}, nil
	}
	return fh, nil
}

func plannedDeleteOp(t *testing.T, path, content string) *models.BatchFileOperation {
	t.Helper()
	return &models.BatchFileOperation{
		OperationType:  models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PlannedDeletes: []models.DeleteEntry{{Path: path, SHA256: sha256HexOf([]byte(content))}}}),
	}
}

// codex P1 (PRRT_kwDORn9KaM6m6WAj): a rename swap landing between the pinned
// hash and the unlink must NOT delete the new occupant. The foreign plant
// rides the no-replace vacate onto the terminal, fails the identity rebind
// against the hashed handle's identity, and is rewound onto the freed name
// byte-intact; the hashed bytes stay untouched under the aside name.
func TestCleanupGeneratedFilesFS_PlannedDeleteVerifyUnlinkSwapRetainsForeign(t *testing.T) {
	const target = "/dst-w161f/lib/w161f-ours.nfo"
	const aside = "/dst-w161f/lib/w161f-aside.bin"
	plant := []byte("a foreign occupant swapped onto the pinned name")

	t.Run("memfs swap (size/window leg)", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll(filepath.Dir(target), 0o777))
		require.NoError(t, afero.WriteFile(base, target, []byte("ours"), 0o666))
		fs := &vacClaimHookFs{Fs: base, target: target, hook: func() {
			require.NoError(t, base.Rename(target, aside))
			require.NoError(t, afero.WriteFile(base, target, plant, 0o666))
		}}
		cleanupGeneratedFilesFS(fs, plannedDeleteOp(t, target, "ours"), "/dst-w161f")
		require.True(t, fs.done, "the swap actually fired inside the hash→unlink window")

		got, err := afero.ReadFile(base, target)
		require.NoError(t, err, "the foreign occupant is retained, never unlinked")
		assert.Equal(t, string(plant), string(got), "foreign bytes survive byte-intact after the rewind")
		kept, err := afero.ReadFile(base, aside)
		require.NoError(t, err, "the pinned bytes survive under the swapped-aside name")
		assert.Equal(t, "ours", string(kept), "the hashed object was never the removal target")
		entries, readErr := afero.ReadDir(base, filepath.Dir(target))
		require.NoError(t, readErr)
		for _, e := range entries {
			assert.NotContains(t, e.Name(), ".vac.", "no bound-unlink terminal litter remains")
		}
	})

	t.Run("osfs inode leg pins the swap even at identical size and mtime", func(t *testing.T) {
		base := afero.NewOsFs()
		root := t.TempDir()
		targetOS := filepath.Join(root, "lib", "w161f-ours.nfo")
		asideOS := filepath.Join(root, "lib", "w161f-aside.bin")
		require.NoError(t, base.MkdirAll(filepath.Dir(targetOS), 0o777))
		require.NoError(t, afero.WriteFile(base, targetOS, []byte("ours"), 0o666))
		admitted, statErr := base.Stat(targetOS)
		require.NoError(t, statErr)

		var plantInfo os.FileInfo
		fs := &vacClaimHookFs{Fs: base, target: targetOS, hook: func() {
			require.NoError(t, base.Rename(targetOS, asideOS))
			require.NoError(t, afero.WriteFile(base, targetOS, []byte("ours"), 0o666))
			require.NoError(t, base.Chtimes(targetOS, admitted.ModTime(), admitted.ModTime()))
			var err error
			plantInfo, err = base.Stat(targetOS)
			require.NoError(t, err)
			require.False(t, os.SameFile(admitted, plantInfo), "the swap fixture must name a different inode")
		}}
		cleanupGeneratedFilesFS(fs, plannedDeleteOp(t, targetOS, "ours"), root)
		require.True(t, fs.done, "the swap actually fired inside the hash→unlink window")

		cur, err := base.Stat(targetOS)
		require.NoError(t, err, "the foreign occupant is retained, never unlinked")
		assert.True(t, os.SameFile(plantInfo, cur), "the name still names the foreign inode — the hashed inode was never unlinked")
		survivor, err := base.Stat(asideOS)
		require.NoError(t, err, "the pinned bytes survive under the swapped-aside name")
		assert.True(t, os.SameFile(admitted, survivor), "the hashed inode was never removed")
	})
}

// A pinned destination consumed by a racer between the hash and the verified
// unlink is the consumed outcome: the vacate answers ENOENT, the entry is
// final, and the empty parent prunes exactly like a successful removal.
func TestCleanupGeneratedFilesFS_PlannedDeleteUnlinkVanishConsumes(t *testing.T) {
	const target = "/dst-w161v/lib/w161v-ours.nfo"
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll(filepath.Dir(target), 0o777))
	require.NoError(t, afero.WriteFile(base, target, []byte("ours"), 0o666))
	fs := &vacateVanishFs{Fs: base, target: target}
	cleanupGeneratedFilesFS(fs, plannedDeleteOp(t, target, "ours"), "/dst-w161v")
	require.True(t, fs.done, "the vanish actually fired at the vacate")

	if _, err := base.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("the vanished entry stays gone: %v", err)
	}
	if _, err := base.Stat(filepath.Dir(target)); !os.IsNotExist(err) {
		t.Fatalf("the consumed entry's empty parent was pruned: %v", err)
	}
	info, err := base.Stat("/dst-w161v")
	require.NoError(t, err, "the stopAt boundary itself is retained")
	assert.True(t, info.IsDir())
}

// A pinned identity that cannot be captured from the open handle leaves the
// removal with nothing to bind to: the entry retains even when its bytes
// match the pin.
func TestCleanupGeneratedFilesFS_PlannedDeletePinnedStatFaultRetains(t *testing.T) {
	const target = "/dst-w161s/w161s-ours.nfo"
	sentinel := errors.New("pinned stat wedged")
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll(filepath.Dir(target), 0o777))
	require.NoError(t, afero.WriteFile(base, target, []byte("ours"), 0o666))
	fs := &pinnedStatDenyFs{Fs: base, path: target, err: sentinel}
	cleanupGeneratedFilesFS(fs, plannedDeleteOp(t, target, "ours"), "/dst-w161s")
	got, err := afero.ReadFile(base, target)
	require.NoError(t, err, "an unprovable identity retains the entry — never a blind pathname remove")
	assert.Equal(t, "ours", string(got))
}
