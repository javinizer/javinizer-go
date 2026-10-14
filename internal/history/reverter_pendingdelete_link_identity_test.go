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

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// The planned-delete pin shapes (DeleteEntry LinkTarget / Identity /
// CopyPartial) consume on proof, retain on doubt, and NEVER touch an
// occupant outside their install shape — the m5HF7 nonregular-retain
// partition carried into each shape. The CopyPartial interim is retain-only
// by rule (codex P1, PRRT_kwDORn9KaM6novbT): the crash-surviving entry's
// bounded proof cannot authorize an unlink — only the sealed SHA256 may
// remove. Symlink-bearing legs run on OsFs and skip where the platform
// cannot create links (unprivileged Windows).

// symlinkVacClaimHookFs is vacClaimHookFs plus a TRUE no-follow Lstat: the
// symlink pin's regularity pre-check must see the link OBJECT (a wrapped fs
// exposing only the afero.Fs interface falls back to a following Stat, whose
// answer — the link's target — belongs to a different entry entirely).
type symlinkVacClaimHookFs struct {
	afero.Fs
	target string
	hook   func()
	done   bool
}

func (f *symlinkVacClaimHookFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if !f.done && flag&os.O_CREATE != 0 && flag&os.O_EXCL != 0 && strings.HasPrefix(name, f.target+".vac.") {
		f.done = true
		f.hook()
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *symlinkVacClaimHookFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func pinLedgerOp(typ models.OperationTypeEnum, entries ...models.DeleteEntry) *models.BatchFileOperation {
	return &models.BatchFileOperation{
		OperationType:  typ,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PlannedDeletes: entries}),
	}
}

func setupLinkPair(t *testing.T) (afero.Fs, string, string, string) {
	t.Helper()
	base := afero.NewOsFs()
	root := t.TempDir()
	source := filepath.Join(root, "incoming", "movie.mkv")
	dest := filepath.Join(root, "library", "movie.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, os.WriteFile(source, []byte("video payload for link pins"), 0o644))
	return base, root, source, dest
}

// resetDest puts dest into a known state for the next subtest: consumed runs
// prune the (then-empty) library parent, so subtests must not assume any
// survivor of a previous run.
func resetDest(t *testing.T, source, dest string) {
	t.Helper()
	_ = os.Remove(dest)
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	_ = source
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unsupported here: %v", err)
	}
}

func TestCleanupGeneratedFilesFS_SoftLinkPinFiresOnThePinnedLink(t *testing.T) {
	base, root, source, dest := setupLinkPair(t)
	mustSymlink(t, source, dest)

	cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeSymlink, models.DeleteEntry{Path: dest, LinkTarget: source}), root)

	_, statErr := os.Lstat(dest)
	assert.True(t, os.IsNotExist(statErr), "the pinned link object is consumed by the revert")
	assert.FileExists(t, source, "the link TARGET is never this pin's removal subject")
}

func TestCleanupGeneratedFilesFS_SoftLinkPinRetainsNonLinkOccupants(t *testing.T) {
	base, root, source, dest := setupLinkPair(t)

	t.Run("regular file with the target's own bytes", func(t *testing.T) {
		resetDest(t, source, dest)
		require.NoError(t, os.WriteFile(dest, []byte("video payload for link pins"), 0o644))
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeSymlink, models.DeleteEntry{Path: dest, LinkTarget: source}), root)
		got, err := os.ReadFile(dest)
		require.NoError(t, err, "a regular occupant is foreign to the link install shape — retained, bytes or not")
		assert.Equal(t, "video payload for link pins", string(got))
	})

	t.Run("foreign-target link", func(t *testing.T) {
		resetDest(t, source, dest)
		foreign := filepath.Join(root, "incoming", "other.mkv")
		require.NoError(t, os.WriteFile(foreign, []byte("other"), 0o644))
		mustSymlink(t, foreign, dest)
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeSymlink, models.DeleteEntry{Path: dest, LinkTarget: source}), root)
		got, err := os.Readlink(dest)
		require.NoError(t, err, "a link naming another object is foreign — retained")
		assert.Equal(t, foreign, got)
	})

	t.Run("directory occupant", func(t *testing.T) {
		resetDest(t, source, dest)
		require.NoError(t, os.MkdirAll(dest, 0o755))
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeSymlink, models.DeleteEntry{Path: dest, LinkTarget: source}), root)
		info, err := os.Lstat(dest)
		require.NoError(t, err, "a directory occupant is retained")
		assert.True(t, info.IsDir())
	})

	t.Run("absent destination consumes", func(t *testing.T) {
		resetDest(t, source, dest)
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeSymlink, models.DeleteEntry{Path: dest, LinkTarget: source}), root)
		_, err := os.Lstat(dest)
		assert.True(t, os.IsNotExist(err), "nothing to consume — nothing to create")
	})
}

// The probe→vacate window discipline: a swap landing after the lstat
// regularity probe must ride the no-replace vacate onto the terminal, fail
// the readlink re-auth, and ride BACK — never a pathname remove of an
// unproven link.
func TestCleanupGeneratedFilesFS_SoftLinkPinSwapInsideWindowRewinds(t *testing.T) {
	base, root, source, dest := setupLinkPair(t)
	mustSymlink(t, source, dest)
	foreign := filepath.Join(root, "incoming", "foreign.mkv")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o644))

	fs := &symlinkVacClaimHookFs{Fs: base, target: dest, hook: func() {
		require.NoError(t, os.Remove(dest))
		if err := os.Symlink(foreign, dest); err != nil {
			panic(err)
		}
	}}
	cleanupGeneratedFilesFS(fs, pinLedgerOp(models.OperationTypeSymlink, models.DeleteEntry{Path: dest, LinkTarget: source}), root)
	require.True(t, fs.done, "the swap actually fired inside the window")
	got, err := os.Readlink(dest)
	require.NoError(t, err, "the swapped-in foreign link is retained byte-intact")
	assert.Equal(t, foreign, got)
	entries, readErr := os.ReadDir(filepath.Dir(dest))
	require.NoError(t, readErr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".vac.", "no terminal litter remains after the rewind")
	}
}

func TestCleanupGeneratedFilesFS_HardLinkIdentityPin(t *testing.T) {
	base, root, source, dest := setupLinkPair(t)
	srcInfo, err := os.Stat(source)
	require.NoError(t, err)
	dev, ino, identityOK := fsutil.BoundObjectIdentity(base, source, srcInfo)
	require.True(t, identityOK, "OsFs exposes a kernel identity — the strong-pin leg is the test subject")

	entryFor := func(dest string) models.DeleteEntry {
		return models.DeleteEntry{
			Path:            dest,
			IdentityStrong:  true,
			IdentityDev:     dev,
			IdentityIno:     ino,
			IdentitySize:    srcInfo.Size(),
			IdentityModUnix: srcInfo.ModTime().Unix(),
		}
	}

	t.Run("fires on the linked object", func(t *testing.T) {
		resetDest(t, source, dest)
		require.NoError(t, os.Link(source, dest))
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, entryFor(dest)), root)
		_, statErr := os.Lstat(dest)
		assert.True(t, os.IsNotExist(statErr), "the linked name is consumed")
		assert.FileExists(t, source, "the source's bytes survive — the unlink never took an inode, only a name")
	})

	t.Run("retains a foreign regular file of the same size", func(t *testing.T) {
		resetDest(t, source, dest)
		require.NoError(t, os.WriteFile(dest, []byte("video payload for link XXXX"), 0o644))
		foreignInfo, err := os.Stat(dest)
		require.NoError(t, err)
		require.InDelta(t, srcInfo.Size(), foreignInfo.Size(), 4, "constructed near-size occupant")
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, entryFor(dest)), root)
		assert.FileExists(t, dest, "a different object keeps its name — the dev/inode legs refused")
	})

	t.Run("retains a symlink occupant even to the pinned target", func(t *testing.T) {
		resetDest(t, source, dest)
		mustSymlink(t, source, dest)
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, entryFor(dest)), root)
		got, err := os.Readlink(dest)
		require.NoError(t, err, "the nonregular-retain rule holds for the identity pin: a link is never this shape's subject")
		assert.Equal(t, source, got)
	})

	t.Run("tuple drift retains", func(t *testing.T) {
		resetDest(t, source, dest)
		require.NoError(t, os.Link(source, dest))
		drifted := entryFor(dest)
		drifted.IdentityModUnix = srcInfo.ModTime().Unix() + 7
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, drifted), root)
		assert.FileExists(t, dest, "a stale tuple (source mutated after the pin) retains the name")
	})

	t.Run("absent destination consumes", func(t *testing.T) {
		resetDest(t, source, dest)
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, entryFor(dest)), root)
		_, statErr := os.Lstat(dest)
		assert.True(t, os.IsNotExist(statErr))
	})
}

func TestCleanupGeneratedFilesFS_HardLinkIdentityWeakLegs(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/lib", 0o755))
	content := []byte("memfs video")
	require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", content, 0o644))
	info, err := base.Stat("/lib/movie.mkv")
	require.NoError(t, err)
	weak := models.DeleteEntry{Path: "/lib/movie.mkv", IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix()}

	t.Run("weak pin fires on the metadata legs", func(t *testing.T) {
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, weak), "/lib")
		exists, _ := afero.Exists(base, "/lib/movie.mkv")
		assert.False(t, exists, "no kernel identity on either side: size+mtime authenticate, same as the admission's weak posture")
	})

	t.Run("weak pin with mtime drift retains", func(t *testing.T) {
		require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", content, 0o644))
		drifted := weak
		drifted.IdentityModUnix = info.ModTime().Unix() + 3
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, drifted), "/lib")
		exists, _ := afero.Exists(base, "/lib/movie.mkv")
		assert.True(t, exists)
	})

	t.Run("strong pin on an identity-free probe never degrades", func(t *testing.T) {
		strong := weak
		strong.IdentityStrong = true
		strong.IdentityDev, strong.IdentityIno = 0xBEEF, 0xCAFE
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, strong), "/lib")
		exists, _ := afero.Exists(base, "/lib/movie.mkv")
		assert.True(t, exists, "memfs exposes no kernel identity — a strong pin retains rather than trusting metadata alone")
	})
}

// codex P1 (PRRT_kwDORn9KaM6novbT): the interim copy pin is journaled BEFORE
// the execute→seal window it covers, so the crash leaves it durable for the
// whole crash→recovery interval — an interval in which a payload edited ONLY
// between the digest windows (same size, same first/last 64KiB) still matches
// the bounded proof. The unsealed interim is therefore intent, never removal
// authorization: recovery retains EVERY occupant under this shape; only
// FinalizeDeleteIntentCopyDigest’s sealed SHA256 may unlink. Before the fix,
// the two "retains" subtests below were deletions.
func TestCleanupGeneratedFilesFS_InterimCopyPinRetainsUnsealed(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/lib", 0o755))
	content := make([]byte, 2*fsutil.CopyPartialDigestSpan+128)
	for i := range content {
		content[i] = byte(i * 3)
	}
	require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", content, 0o644))
	_, wantDigest, err := fsutil.PartialCopyDigest(base, "/lib/movie.mkv")
	require.NoError(t, err)
	pinFor := func(path string) models.DeleteEntry {
		return models.DeleteEntry{Path: path, CopySize: int64(len(content)), CopyPartialSHA256: wantDigest}
	}

	t.Run("the byte-identical published copy retains", func(t *testing.T) {
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeCopy, pinFor("/lib/movie.mkv")), "/lib")
		got, readErr := afero.ReadFile(base, "/lib/movie.mkv")
		require.NoError(t, readErr, "even an exact interim match is intent, not removal power")
		assert.Equal(t, content, got)
	})

	t.Run("a middle-only payload edit survives recovery byte-intact", func(t *testing.T) {
		// The reported hazard: size and the first/last 64KiB are untouched,
		// only bytes BETWEEN the digest windows changed — the retired interim
		// probe would still have matched and unlinked the EDITED file.
		edited := make([]byte, len(content))
		copy(edited, content)
		edited[fsutil.CopyPartialDigestSpan+64] ^= 0xFF
		require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", edited, 0o644))
		_, editedDigest, digestErr := fsutil.PartialCopyDigest(base, "/lib/movie.mkv")
		require.NoError(t, digestErr)
		require.Equal(t, wantDigest, editedDigest, "constructed to fool the bounded probe: same size and edge windows")
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeCopy, pinFor("/lib/movie.mkv")), "/lib")
		got, readErr := afero.ReadFile(base, "/lib/movie.mkv")
		require.NoError(t, readErr, "the edited payload must survive — only the sealed full digest may authorize this unlink")
		assert.Equal(t, edited, got, "the middle-of-payload edit is intact")
	})

	t.Run("directory occupant retains (nonregular rule)", func(t *testing.T) {
		require.NoError(t, base.MkdirAll("/lib/dir.mkv", 0o755))
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeCopy, pinFor("/lib/dir.mkv")), "/lib")
		info, statErr := base.Stat("/lib/dir.mkv")
		require.NoError(t, statErr)
		assert.True(t, info.IsDir())
	})

	t.Run("absent destination consumes", func(t *testing.T) {
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeCopy, pinFor("/lib/absent.mkv")), "/lib")
		exists, _ := afero.Exists(base, "/lib/absent.mkv")
		assert.False(t, exists)
	})
}

// The SHA leg must still win for entries carrying a full hash (sealed copy
// pins land there): a partial-only digest never substitutes for the sealed
// upgrade, and the hash leg's behavior is untouched for every sealed entry.
func TestCleanupGeneratedFilesFS_SealedCopyPinUsesFullHashLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/lib", 0o755))
	content := []byte("sealed digest content")
	require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", content, 0o644))
	entry := models.DeleteEntry{Path: "/lib/movie.mkv", SHA256: sha256HexOf(content)}
	cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeCopy, entry), "/lib")
	exists, _ := afero.Exists(base, "/lib/movie.mkv")
	assert.False(t, exists, "the full-hash leg fires exactly as before the interim shape existed")

	require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", []byte("sealed digest contenX"), 0o644))
	partialInfo, _, err := fsutil.PartialCopyDigest(base, "/lib/movie.mkv")
	require.NoError(t, err)
	require.Equal(t, int64(len(content)), partialInfo.Size(), "constructed same-size content drift")
	cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeCopy, entry), "/lib")
	exists, _ = afero.Exists(base, "/lib/movie.mkv")
	assert.True(t, exists, "same-size content drift still retains under the full hash")
}

// --- Probe-failure, refusal, vanish, and rewound-swap legs of the three pin
// shapes: every doubt retains (retention-first), every proven vanish
// consumes. ---

// lstatFailFS forces the no-follow probe to answer something other than
// present/absent: an indeterminate occupancy NEVER licenses a removal.
type lstatFailFS struct {
	afero.Fs
	err error
}

func (f *lstatFailFS) LstatIfPossible(string) (os.FileInfo, bool, error) {
	return nil, true, f.err
}

func TestCleanupGeneratedFilesFS_PinProbeFailureRetains(t *testing.T) {
	sentinel := errors.New("lstat refused")
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/lib", 0o755))
	require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", []byte("vid"), 0o644))
	fs := &lstatFailFS{Fs: base, err: sentinel}
	for _, shape := range []struct {
		name  string
		typ   models.OperationTypeEnum
		entry models.DeleteEntry
	}{
		{"symlink pin", models.OperationTypeSymlink, models.DeleteEntry{Path: "/lib/movie.mkv", LinkTarget: "/in/movie.mkv"}},
		{"identity pin", models.OperationTypeHardlink, models.DeleteEntry{Path: "/lib/movie.mkv", IdentitySize: 3, IdentityModUnix: 1700000000}},
		{"partial pin", models.OperationTypeCopy, models.DeleteEntry{Path: "/lib/movie.mkv", CopySize: 3, CopyPartialSHA256: "aa"}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			cleanupGeneratedFilesFS(fs, pinLedgerOp(shape.typ, shape.entry), "/lib")
			exists, existsErr := afero.Exists(base, "/lib/movie.mkv")
			require.NoError(t, existsErr)
			assert.True(t, exists, "an indeterminate probe retains the occupant")
		})
	}
}

// symlinkRenameFaultFs forwards the true no-follow probes but wedges the
// vacate rename: the removal errors non-vanished and the reverter retains
// (never a pathname delete on a moving doubt).
type symlinkRenameFaultFs struct {
	afero.Fs
	err error
}

func (f *symlinkRenameFaultFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *symlinkRenameFaultFs) ReadlinkIfPossible(name string) (string, error) {
	return f.Fs.(afero.LinkReader).ReadlinkIfPossible(name)
}

func (f *symlinkRenameFaultFs) Rename(string, string) error { return f.err }

func TestCleanupGeneratedFilesFS_SoftLinkPinVacateRefusalRetains(t *testing.T) {
	base, root, source, dest := setupLinkPair(t)
	mustSymlink(t, source, dest)
	sentinel := errors.New("vacate wedged")
	fs := &symlinkRenameFaultFs{Fs: base, err: sentinel}

	cleanupGeneratedFilesFS(fs, pinLedgerOp(models.OperationTypeSymlink, models.DeleteEntry{Path: dest, LinkTarget: source}), root)
	got, err := os.Readlink(dest)
	require.NoError(t, err, "a refused vacate retains the occupant — no recovery-only deletion strides past the proof")
	assert.Equal(t, source, got)
}

// symlinkVacateRemoveVanishFs lets the vacate win the name but answers the
// terminal Remove with NotExist (the object vanished unownably) — the
// consumed class surfaces and the parent prunes.
func TestCleanupGeneratedFilesFS_VanishLegsConsume(t *testing.T) {
	t.Run("identity pin vanished under the unlink", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/lib", 0o755))
		require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", []byte("vid"), 0o644))
		info, statErr := base.Stat("/lib/movie.mkv")
		require.NoError(t, statErr)
		fs := &vacateVanishFs{Fs: base, target: "/lib/movie.mkv"}
		cleanupGeneratedFilesFS(fs, pinLedgerOp(models.OperationTypeHardlink, models.DeleteEntry{Path: "/lib/movie.mkv", IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix()}), "/lib")
		assert.True(t, fs.done, "the vacate hook ran — else the leg proves nothing")
	})

}

// Swaps landing INSIDE the unlink window against the identity leg: the
// vacate rides the foreign occupant, the identity rebind refuses it, and it
// rides BACK byte-intact — the bound discipline the SHA leg already proves.
func TestCleanupGeneratedFilesFS_PinWindowSwapRewinds(t *testing.T) {
	t.Run("identity pin", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/lib", 0o755))
		require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", []byte("vid"), 0o644))
		info, statErr := base.Stat("/lib/movie.mkv")
		require.NoError(t, statErr)
		plant := []byte("a foreign occupant swapped inside the window")
		fs := &vacClaimHookFs{Fs: base, target: "/lib/movie.mkv", hook: func() {
			require.NoError(t, base.Rename("/lib/movie.mkv", "/lib/aside.bin"))
			require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", plant, 0o644))
		}}
		cleanupGeneratedFilesFS(fs, pinLedgerOp(models.OperationTypeHardlink, models.DeleteEntry{Path: "/lib/movie.mkv", IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix()}), "/lib")
		require.True(t, fs.done)
		got, readErr := afero.ReadFile(base, "/lib/movie.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, plant, got, "the foreign occupant rides back byte-intact")
	})

}

// symlinkVacateVanishFs removes the pinned name the moment the vacate rename
// reaches it and answers ENOENT (the vacateVanishFs discipline, with the
// no-follow probe interfaces the symlink leg requires): the entry consumed
// itself — the reverter prunes the empty parent, same as the other shapes.
type symlinkVacateVanishFs struct {
	afero.Fs
	target string
	done   bool
}

func (f *symlinkVacateVanishFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *symlinkVacateVanishFs) ReadlinkIfPossible(name string) (string, error) {
	return f.Fs.(afero.LinkReader).ReadlinkIfPossible(name)
}

func (f *symlinkVacateVanishFs) Rename(oldname, newname string) error {
	if !f.done && filepath.Clean(oldname) == filepath.Clean(f.target) {
		f.done = true
		if rmErr := f.Fs.Remove(oldname); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
		return &os.PathError{Op: "rename", Path: oldname, Err: os.ErrNotExist}
	}
	return f.Fs.Rename(oldname, newname)
}

func TestCleanupGeneratedFilesFS_SoftLinkPinVanishedUnderUnlinkConsumes(t *testing.T) {
	base, root, source, dest := setupLinkPair(t)
	mustSymlink(t, source, dest)
	fs := &symlinkVacateVanishFs{Fs: base, target: dest}

	cleanupGeneratedFilesFS(fs, pinLedgerOp(models.OperationTypeSymlink, models.DeleteEntry{Path: dest, LinkTarget: source}), root)
	require.True(t, fs.done, "the vanish actually fired at the vacate")
	assert.FileExists(t, source, "the target object was never involved")
}
