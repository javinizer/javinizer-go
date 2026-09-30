package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verifyHookFS wedges individual afero operations for the verified-composite
// fault legs: each nil hook delegates to the wrapped filesystem.
type verifyHookFS struct {
	afero.Fs
	openFile func(name string, flag int, perm os.FileMode) (afero.File, error)
	open     func(name string) (afero.File, error)
	rename   func(oldname, newname string) error
	remove   func(name string) error
	mkdirAll func(name string, perm os.FileMode) error
	lstat    func(name string) (os.FileInfo, bool, error)
}

func (f *verifyHookFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.openFile != nil {
		return f.openFile(name, flag, perm)
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *verifyHookFS) Open(name string) (afero.File, error) {
	if f.open != nil {
		return f.open(name)
	}
	return f.Fs.Open(name)
}

func (f *verifyHookFS) Rename(oldname, newname string) error {
	if f.rename != nil {
		return f.rename(oldname, newname)
	}
	return f.Fs.Rename(oldname, newname)
}

func (f *verifyHookFS) Remove(name string) error {
	if f.remove != nil {
		return f.remove(name)
	}
	return f.Fs.Remove(name)
}

func (f *verifyHookFS) MkdirAll(name string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(name, perm)
	}
	return f.Fs.MkdirAll(name, perm)
}

func (f *verifyHookFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.lstat != nil {
		return f.lstat(name)
	}
	if l, ok := f.Fs.(afero.Lstater); ok {
		return l.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// verifyProofOf admits exactly the object snapshot captured at admission
// (no-follow). The capture is EAGER wherever the platform exposes an identity —
// dev/inode out of a POSIX Stat_t, the volume-serial+file-index handle identity
// through BoundObjectIdentity on Windows — and re-proves the object under proof
// at equal strength. os.SameFile cannot serve as this pin on Windows: a
// path-captured FileInfo there records the PATH, not the object, and SameFile's
// lazy file-id load re-opens that path at COMPARE time — by the verified move's
// claim re-proof the source name is already vacated (the comparison fails
// closed and refuses the admitted object's own happy path), and after a
// pre-execute swap the same lazy load binds the REPLACEMENT, silently admitting
// it. In-memory filesystems keep the asideSameObject size+modtime discipline
// (Sys()==nil, no identity to pin).
func verifyProofOf(t *testing.T, fs afero.Fs, path string) (VerifiedSourceProof, os.FileInfo) {
	t.Helper()
	admitted, err := asideLstat(fs, path)
	require.NoError(t, err)
	// Snapshot the metadata legs EAGERLY at admission: virtual filesystems
	// answer lstat with a LIVE FileInfo view (afero mem.FileInfo wraps the
	// shared FileData), so comparing admitted.Size() at proof time would
	// re-read an in-place rewrite and admit exactly what the proof exists to
	// refuse — the production matchers snapshot into scalars for the same
	// reason (workflow.captureArtifactSourceIdentity).
	admSize, admMod := admitted.Size(), admitted.ModTime()
	admDev, admIno, admStrong := BoundObjectIdentity(fs, path, admitted)
	return func(probed string, info os.FileInfo) error {
		if admStrong {
			dev, ino, strong := BoundObjectIdentity(fs, probed, info)
			if info == nil || !info.Mode().IsRegular() || !strong || dev != admDev || ino != admIno ||
				info.Size() != admSize || !info.ModTime().Equal(admMod) {
				return fmt.Errorf("%s no longer names the admitted object", path)
			}
			return nil
		}
		if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
			info.Size() != admSize || !info.ModTime().Equal(admMod) {
			return fmt.Errorf("%s no longer names the admitted object", path)
		}
		return nil
	}, admitted
}

// renameSwap replaces path's directory entry with a differently-sized foreign
// object via a genuine rename, mirroring a concurrent writer's swap.
func renameSwap(t *testing.T, fs afero.Fs, path string, aside string, data []byte) string {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, aside, data, 0o644))
	require.NoError(t, fs.Remove(path))
	require.NoError(t, fs.Rename(aside, path))
	return aside
}

// liveEntrySwap re-points path at a differently-bodied foreign object WHILE a
// reader handle pins the admitted object — the post-open swap the verified
// copy leg is bound against. POSIX unlink/rename of an open entry is
// permissive, so the plain remove+rename construction stands. Windows refuses
// a Remove against an open entry, and a DeleteFile'd name stays parked until
// the last handle closes even with the delete share granted, so the
// platform's expressible live swap is the downloader's own construction:
// rename the live entry aside (admitted only because the composite opened it
// with FILE_SHARE_DELETE sharing) and create the replacement fresh at the
// freed name. The displaced victim's name is returned for post-completion
// cleanup — remove-after-return ordering is load-bearing on Windows (the
// composite's own defer must already have closed the pin), and the POSIX leg
// returns "".
func liveEntrySwap(t *testing.T, fs afero.Fs, path string, aside string, data []byte) (victim string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		victim = path + ".swapped-aside"
		require.NoError(t, fs.Rename(path, victim))
		require.NoError(t, afero.WriteFile(fs, path, data, 0o644))
		return victim
	}
	renameSwap(t, fs, path, aside, data)
	return ""
}

func assertNoBoundResidue(t *testing.T, fs afero.Fs, dir string) {
	t.Helper()
	_ = afero.Walk(fs, dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			assert.NotContains(t, info.Name(), ".vac.", "bound-take residue: %s", path)
			assert.NotContains(t, info.Name(), ".nrstg", "copy staging residue: %s", path)
		}
		return nil
	})
}

func verifyFixture(t *testing.T) (afero.Fs, string, string, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	src := filepath.FromSlash("/in/movie.mp4")
	dst := filepath.FromSlash("/lib/movie/movie.mp4")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
	return fs, src, dst, "admitted video bytes"
}

func TestMoveFileNoReplaceVerifiedSelfNoOp(t *testing.T) {
	fs, src, _, _ := verifyFixture(t)
	proof, _ := verifyProofOf(t, fs, src)
	require.NoError(t, MoveFileNoReplaceVerified(fs, src, src, proof))
	got, err := afero.ReadFile(fs, src)
	require.NoError(t, err)
	assert.Equal(t, "admitted video bytes", string(got))
}

func TestMoveFileNoReplaceVerifiedDestinationCollision(t *testing.T) {
	fs, src, dst, _ := verifyFixture(t)
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign occupant"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorIs(t, err, ErrPublishCollision)
	assert.True(t, PublishRefusal(err))
	got, _ := afero.ReadFile(fs, src)
	assert.Equal(t, "admitted video bytes", string(got))
	got, _ = afero.ReadFile(fs, dst)
	assert.Equal(t, "foreign occupant", string(got))
}

func TestMoveFileNoReplaceVerifiedMkdirFailure(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, mkdirAll: func(name string, perm os.FileMode) error {
		return errors.New("mkdir denied")
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "create destination directory")
}

func TestMoveFileNoReplaceVerifiedClaimReserveFailure(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
		if strings.Contains(name, ".vac.") {
			return nil, errors.New("claim draw denied")
		}
		return base.OpenFile(name, flag, perm)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "reserve the source claim name")
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, body, string(got), "nothing relocated when the claim cannot draw")
}

func TestMoveFileNoReplaceVerifiedClaimReleaseFailure(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, remove: func(name string) error {
		if strings.Contains(name, ".vac.") {
			return errors.New("release unlink denied")
		}
		return base.Remove(name)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "free the source claim name")
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, body, string(got), "nothing relocated when the claim cannot be freed")
}

func TestMoveFileNoReplaceVerifiedTakeVanished(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, rename: func(oldname, newname string) error {
		if strings.Contains(newname, ".vac.") {
			return fmt.Errorf("simulated vanish: %w", os.ErrNotExist)
		}
		return base.Rename(oldname, newname)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, body, string(got), "a refused take relocates nothing")
	assertNoBoundResidue(t, base, "/")
}

func TestMoveFileNoReplaceVerifiedTakeRefused(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, rename: func(oldname, newname string) error {
		if strings.Contains(newname, ".vac.") {
			return fmt.Errorf("simulated refusal: %w", syscall.EPERM)
		}
		return base.Rename(oldname, newname)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "nothing relocated")
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, body, string(got))
	assertNoBoundResidue(t, base, "/")
}

// The pre-re-proof claim-name lookups: releaseTakeAsideVacClaim proves the
// claim twice (calls 1-2) and the virtual no-replace publish classifies the
// claim name once (call 3); the composite's own post-take re-proof is call 4.
func moveVerifiedClaimLstatHook(base afero.Fs, failAt int, failWith error) func(string) (os.FileInfo, bool, error) {
	calls := 0
	return func(name string) (os.FileInfo, bool, error) {
		if strings.Contains(name, ".vac.") {
			calls++
			if calls == failAt {
				return nil, false, failWith
			}
		}
		if l, ok := base.(afero.Lstater); ok {
			return l.LstatIfPossible(name)
		}
		info, err := base.Stat(name)
		return info, false, err
	}
}

func TestMoveFileNoReplaceVerifiedPostTakeLstatVanished(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, lstat: moveVerifiedClaimLstatHook(base, 4, os.ErrNotExist)}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	_, statErr := base.Stat(src)
	assert.True(t, os.IsNotExist(statErr), "the take had already consumed the source")
	claims := 0
	_ = afero.Walk(base, "/", func(path string, info os.FileInfo, werr error) error {
		if werr == nil && !info.IsDir() && strings.Contains(info.Name(), ".vac.") {
			claims++
			data, _ := afero.ReadFile(base, path)
			assert.Equal(t, body, string(data), "the consumed source stays recoverable at the unproven claim name")
		}
		return nil
	})
	assert.Equal(t, 1, claims, "the vanished re-proof compensates nothing but strands no body-less name")
}

func TestMoveFileNoReplaceVerifiedPostTakeLstatIndeterminateRestores(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, lstat: moveVerifiedClaimLstatHook(base, 4, errors.New("claim lookup I/O"))}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "inspect the taken claim")
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, body, string(got), "the taken-aside source rode back onto its name")
	assertNoBoundResidue(t, base, "/")
}

// A rename-swap landing before the claim take: the take moves the REPLACEMENT
// aside, the proof refuses it, and the restore returns the foreign object to
// the source name untouched.
func TestMoveFileNoReplaceVerifiedForeignSwapRestoredByteIntact(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	proof, admitted := verifyProofOf(t, base, src)
	renameSwap(t, base, src, filepath.FromSlash("/in/replacement.mp4"), []byte("foreign replacement — different length"))
	err := MoveFileNoReplaceVerified(base, src, dst, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	assert.False(t, PublishRefusal(err), "an admission refusal is a pre-publish failure, not a refusal class")
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, "foreign replacement — different length", string(got), "foreign bytes restored, never consumed")
	exists, _ := afero.Exists(base, dst)
	assert.False(t, exists)
	assertNoBoundResidue(t, base, "/")
	info, _ := asideLstat(base, src)
	assert.False(t, asideSameObject(info, admitted), "the restored entry is provably not the admitted object")
}

// The proof refuses AFTER another writer planted at the (now vacant) source
// name: the restore cannot clobber the plant, so both objects survive and the
// restore failure joins the refusal.
func TestMoveFileNoReplaceVerifiedRestoreCollisionKeepsBoth(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	proof, _ := verifyProofOf(t, base, src)
	renameSwap(t, base, src, filepath.FromSlash("/in/replacement.mp4"), []byte("foreign replacement — different length"))
	refusing := VerifiedSourceProof(func(path string, info os.FileInfo) error {
		require.NoError(t, afero.WriteFile(base, src, []byte("planted at source"), 0o644))
		return proof(path, info)
	})
	err := MoveFileNoReplaceVerified(base, src, dst, refusing)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorIs(t, err, ErrTakeAsideRestoreFailed)
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, "planted at source", string(got), "the plant at the source name is never clobbered")
	exists, _ := afero.Exists(base, dst)
	assert.False(t, exists)
	found := false
	_ = afero.Walk(base, "/", func(path string, info os.FileInfo, werr error) error {
		if werr == nil && !info.IsDir() && strings.Contains(info.Name(), ".vac.") {
			data, _ := afero.ReadFile(base, path)
			if strings.Contains(string(data), "foreign replacement") {
				found = true
			}
		}
		return nil
	})
	assert.True(t, found, "the refused replacement stays recoverable at the claim name")
}

// The F1 window itself: the source entry is re-pointed AFTER the claim take
// re-proved the admitted object. Publication must land the ADMITTED bytes and
// leave the plant at the source name byte-intact.
func TestMoveFileNoReplaceVerifiedPublishesAdmittedDespitePostClaimPlant(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	proof, _ := verifyProofOf(t, base, src)
	planting := VerifiedSourceProof(func(path string, info os.FileInfo) error {
		if err := proof(path, info); err != nil {
			return err
		}
		require.NoError(t, afero.WriteFile(base, src, []byte("planted after the take"), 0o644))
		return nil
	})
	require.NoError(t, MoveFileNoReplaceVerified(base, src, dst, planting))
	got, err := afero.ReadFile(base, dst)
	require.NoError(t, err)
	assert.Equal(t, body, string(got), "the admitted object published")
	got, err = afero.ReadFile(base, src)
	require.NoError(t, err)
	assert.Equal(t, "planted after the take", string(got), "the post-take plant is preserved, never consumed")
	assertNoBoundResidue(t, base, "/")
}

// A foreign writer occupying the destination inside the classify→publish
// window collides at the no-replace publish and the claim restores.
func TestMoveFileNoReplaceVerifiedPublishCollisionRestores(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	proof, _ := verifyProofOf(t, base, src)
	planting := VerifiedSourceProof(func(path string, info os.FileInfo) error {
		if err := proof(path, info); err != nil {
			return err
		}
		require.NoError(t, afero.WriteFile(base, dst, []byte("foreign destination"), 0o644))
		return nil
	})
	err := MoveFileNoReplaceVerified(base, src, dst, planting)
	require.ErrorIs(t, err, ErrPublishCollision)
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, body, string(got), "the verified claim rode back onto the source name")
	got, _ = afero.ReadFile(base, dst)
	assert.Equal(t, "foreign destination", string(got))
	assertNoBoundResidue(t, base, "/")
}

// Publish collision and a re-occupied source name together: the join keeps
// the collision class and the restore-failure class, and nothing is clobbered.
func TestMoveFileNoReplaceVerifiedPublishCollisionAndRestoreCollision(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	proof, _ := verifyProofOf(t, base, src)
	planting := VerifiedSourceProof(func(path string, info os.FileInfo) error {
		if err := proof(path, info); err != nil {
			return err
		}
		require.NoError(t, afero.WriteFile(base, dst, []byte("foreign destination"), 0o644))
		require.NoError(t, afero.WriteFile(base, src, []byte("plant at source"), 0o644))
		return nil
	})
	err := MoveFileNoReplaceVerified(base, src, dst, planting)
	require.ErrorIs(t, err, ErrPublishCollision)
	require.ErrorIs(t, err, ErrTakeAsideRestoreFailed)
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, "plant at source", string(got))
	got, _ = afero.ReadFile(base, dst)
	assert.Equal(t, "foreign destination", string(got))
	_ = body
}

func TestMoveFileNoReplaceVerifiedHappyPath(t *testing.T) {
	for _, fsKind := range []string{"memfs", "osfs"} {
		t.Run(fsKind, func(t *testing.T) {
			var fs afero.Fs
			var src, dst string
			if fsKind == "memfs" {
				var b string
				fs, src, dst, b = verifyFixture(t)
				_ = b
			} else {
				fs = afero.NewOsFs()
				root := t.TempDir()
				src = filepath.Join(root, "in", "movie.mp4")
				dst = filepath.Join(root, "lib", "movie.mp4")
				require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
				require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
			}
			proof, _ := verifyProofOf(t, fs, src)
			require.NoError(t, MoveFileNoReplaceVerified(fs, src, dst, proof))
			got, err := afero.ReadFile(fs, dst)
			require.NoError(t, err)
			assert.Equal(t, "admitted video bytes", string(got))
			exists, _ := afero.Exists(fs, src)
			assert.False(t, exists, "the verified publish consumed the admitted source")
			assertNoBoundResidue(t, fs, filepath.Dir(filepath.Dir(src)))
		})
	}
}

// OsFs strong-identity refusal: a same-size, mtime-restored rename-swap is
// still a different inode, so the proof must refuse where the weak legs
// cannot see the difference by themselves.
func TestMoveFileNoReplaceVerifiedOsFsInodeSwapRefuses(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
	proof, admitted := verifyProofOf(t, fs, src)
	aside := renameSwap(t, fs, src, filepath.Join(root, "in", "replacement.bin"), []byte("swapped video bytes!"))
	require.Len(t, "swapped video bytes!", len("admitted video bytes"), "same size, mtime restored below — only the inode differs")
	require.NoError(t, os.Chtimes(src, admitted.ModTime(), admitted.ModTime()))
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	exists, _ := afero.Exists(fs, dst)
	assert.False(t, exists)
	got, _ := afero.ReadFile(fs, aside)
	assert.Equal(t, "", string(got), "the swap simulation consumed its aside name")
	got, _ = afero.ReadFile(fs, src)
	assert.Equal(t, "swapped video bytes!", string(got), "the replacement rides back byte-intact")
	assertNoBoundResidue(t, fs, root)
}

// The take hop's link-publish landing with a refused staged unlink is an
// internal-hop publish-completion: the class must NOT surface, or callers
// would register the video destination as published when nothing reached it.
// The scenario stands only on the hard-link take leg, so the per-GOOS
// TestMoveFileNoReplaceVerifiedTakeCompletedClassStripped wrappers
// (move_verified_take_completed_*_test.go) route the take through
// publishNoReplaceFallback and wedge its staged-unlink seam BEFORE calling
// this body: Linux otherwise publishes the take through
// renameat2(RENAME_NOREPLACE) — a kernel-atomic rename with no
// link-then-unlink construction, where the wedged remove seam never fires
// and the scenario silently degrades into the happy path — and Windows's
// MoveFileEx take has no residue construction at all, so the completed-class
// strip has no reachable take-hop leg there (the class remains guarded for
// the fallback shape the POSIX legs express).
func assertVerifiedTakeCompletedClassStripped(t *testing.T) {
	t.Helper()
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)

	// Precondition installed by the per-GOOS wrapper: the take hop rides the
	// hard-link fallback with its staged-unlink seam wedged.
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPublishCompleted, "the internal take hop's completed-with-residue class never surfaces")
	assert.NotErrorIs(t, err, ErrTakeAsideForeign, "no admission refusal — nothing foreign was involved")
	got, rerr := afero.ReadFile(fs, src)
	require.NoError(t, rerr)
	assert.Equal(t, "admitted video bytes", string(got), "the source name keeps its bytes")
	exists, _ := afero.Exists(fs, dst)
	assert.False(t, exists)
	claims := 0
	_ = afero.Walk(fs, root, func(path string, info os.FileInfo, werr error) error {
		if werr == nil && !info.IsDir() && strings.Contains(info.Name(), ".vac.") {
			claims++
			data, _ := afero.ReadFile(fs, path)
			assert.Equal(t, "admitted video bytes", string(data), "the hop's residue is recoverable at the claim name")
		}
		return nil
	})
	assert.Equal(t, 1, claims)
}

// exdevHookFS fails the claim→destination no-replace rename with EXDEV,
// routing the verified move onto its cross-device copy leg.
type exdevHookFS struct {
	afero.Fs
	dst  string
	fire bool
}

func (f *exdevHookFS) Rename(oldname, newname string) error {
	if f.fire && filepath.Clean(newname) == filepath.Clean(f.dst) && strings.Contains(oldname, ".vac.") {
		return fmt.Errorf("simulated cross-device link: %w", syscall.EXDEV)
	}
	return f.Fs.Rename(oldname, newname)
}

func TestMoveFileNoReplaceVerifiedCrossDevicePublishesAdmitted(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &exdevHookFS{Fs: base, dst: dst, fire: true}
	proof, _ := verifyProofOf(t, base, src)
	require.NoError(t, MoveFileNoReplaceVerified(fs, src, dst, proof))
	got, err := afero.ReadFile(base, dst)
	require.NoError(t, err)
	assert.Equal(t, body, string(got), "the admitted object's bytes crossed devices")
	exists, _ := afero.Exists(base, src)
	assert.False(t, exists, "the verified cross-device move consumed its source")
	assertNoBoundResidue(t, base, "/")
}

func TestMoveFileNoReplaceVerifiedCrossDeviceCopyFailureRestores(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	exdev := &exdevHookFS{Fs: base, dst: dst, fire: true}
	fs := &verifyHookFS{Fs: exdev, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
		if strings.Contains(name, ".nrstg.") {
			return nil, errors.New("staging draw denied")
		}
		return exdev.OpenFile(name, flag, perm)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "cross-device publish")
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, body, string(got), "a failed cross-device copy rode the claim back")
	exists, _ := afero.Exists(base, dst)
	assert.False(t, exists)
	assertNoBoundResidue(t, base, "/")
}

func TestMoveFileNoReplaceVerifiedCrossDeviceClaimOpenFailureRestores(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	exdev := &exdevHookFS{Fs: base, dst: dst, fire: true}
	fs := &verifyHookFS{Fs: exdev, open: func(name string) (afero.File, error) {
		if strings.Contains(name, ".vac.") {
			return nil, errors.New("claim open denied")
		}
		return exdev.Open(name)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "open the claimed source")
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, body, string(got))
	assertNoBoundResidue(t, base, "/")
}

func TestMoveFileNoReplaceVerifiedCrossDeviceClaimStatFailureRestores(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	exdev := &exdevHookFS{Fs: base, dst: dst, fire: true}
	fs := &verifyHookFS{Fs: exdev, open: func(name string) (afero.File, error) {
		if strings.Contains(name, ".vac.") {
			fh, err := exdev.Open(name)
			if err != nil {
				return nil, err
			}
			return &statFailFile{File: fh}, nil
		}
		return exdev.Open(name)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "inspect the open claim handle")
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, body, string(got))
	assertNoBoundResidue(t, base, "/")
}

func TestMoveFileNoReplaceVerifiedCrossDeviceClaimProofRefusalRestores(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &exdevHookFS{Fs: base, dst: dst, fire: true}
	proof, _ := verifyProofOf(t, base, src)
	calls := 0
	flipping := VerifiedSourceProof(func(path string, info os.FileInfo) error {
		calls++
		if calls > 1 {
			return errors.New("admission re-proof denied")
		}
		return proof(path, info)
	})
	err := MoveFileNoReplaceVerified(fs, src, dst, flipping)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorContains(t, err, "open claim handle failed its admission proof")
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, body, string(got))
	assertNoBoundResidue(t, base, "/")
}

// The EXDEV leg replays the mid-stream in-place mutation (codex P2,
// PRRT_kwDORn9KaM6nkVjY) against the taken-aside claim: the claim's open
// handle pins the object, not its bytes, so a concurrent rewrite mid-stream
// must fail the post-stream re-proof — the stale staged copy is discarded,
// nothing publishes, and the claim compensation rides the (mutated) object
// back onto the source name byte-intact.
func TestMoveFileNoReplaceVerifiedCrossDeviceMidStreamMutationRestores(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	mutant := []byte(strings.Repeat("post-admission in-place rewrite ", 64))
	exdev := &exdevHookFS{Fs: base, dst: dst, fire: true}
	mut := &midStreamMutator{fs: base, body: mutant, quota: 2}
	fs := &verifyHookFS{Fs: exdev, open: func(name string) (afero.File, error) {
		if strings.Contains(name, ".vac.") {
			return mut.open(name)
		}
		return exdev.Open(name)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	assert.True(t, mut.mutated, "the replay really landed mid-stream")
	exists, _ := afero.Exists(base, dst)
	assert.False(t, exists, "the refused publish writes no destination")
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, mutant, got, "the claim compensation restores the object onto the source name")
	assertNoBoundResidue(t, base, "/")
}

// The published destination stands but the claim cannot be removed: the
// ambiguity keeps BOTH objects and reports the publish-completed class,
// matching MoveFileNoReplace's cross-device cleanup refusal.
func TestMoveFileNoReplaceVerifiedCrossDeviceCleanupRefusalCompleted(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	exdev := &exdevHookFS{Fs: base, dst: dst, fire: true}
	fs := &verifyHookFS{Fs: exdev, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
		if strings.Count(name, ".vac.") >= 2 {
			return nil, errors.New("terminal draw denied")
		}
		return exdev.OpenFile(name, flag, perm)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := MoveFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorIs(t, err, ErrPublishCompleted)
	assert.False(t, PublishRefusal(err))
	got, rerr := afero.ReadFile(base, dst)
	require.NoError(t, rerr)
	assert.Equal(t, body, string(got), "the publish landed and stands")
	_, statErr := base.Stat(src)
	assert.True(t, os.IsNotExist(statErr), "the consumed source name stays vacant")
	claims := 0
	_ = afero.Walk(base, "/", func(path string, info os.FileInfo, werr error) error {
		if werr == nil && !info.IsDir() && strings.Contains(info.Name(), ".vac.") {
			claims++
			data, _ := afero.ReadFile(base, path)
			assert.Equal(t, body, string(data), "the admitted source stays recoverable at the claim name")
		}
		return nil
	})
	assert.Equal(t, 1, claims)
}

func TestCopyFileNoReplaceVerifiedSelfNoOp(t *testing.T) {
	fs, src, _, _ := verifyFixture(t)
	proof, _ := verifyProofOf(t, fs, src)
	require.NoError(t, CopyFileNoReplaceVerified(fs, src, src, proof))
	got, _ := afero.ReadFile(fs, src)
	assert.Equal(t, "admitted video bytes", string(got))
}

func TestCopyFileNoReplaceVerifiedDestinationCollision(t *testing.T) {
	fs, src, dst, _ := verifyFixture(t)
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign occupant"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)
	require.ErrorIs(t, CopyFileNoReplaceVerified(fs, src, dst, proof), ErrPublishCollision)
	got, _ := afero.ReadFile(fs, dst)
	assert.Equal(t, "foreign occupant", string(got))
}

func TestCopyFileNoReplaceVerifiedMkdirFailure(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, mkdirAll: func(name string, perm os.FileMode) error {
		return errors.New("mkdir denied")
	}}
	proof, _ := verifyProofOf(t, base, src)
	require.ErrorContains(t, CopyFileNoReplaceVerified(fs, src, dst, proof), "create destination directory")
}

func TestCopyFileNoReplaceVerifiedOpenFailure(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, open: func(name string) (afero.File, error) {
		if filepath.Clean(name) == filepath.Clean(src) {
			return nil, errors.New("source open denied")
		}
		return base.Open(name)
	}}
	proof, _ := verifyProofOf(t, base, src)
	require.ErrorContains(t, CopyFileNoReplaceVerified(fs, src, dst, proof), "open source")
}

func TestCopyFileNoReplaceVerifiedHandleStatFailure(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, open: func(name string) (afero.File, error) {
		fh, err := base.Open(name)
		if err == nil && filepath.Clean(name) == filepath.Clean(src) {
			return &statFailFile{File: fh}, nil
		}
		return fh, err
	}}
	proof, _ := verifyProofOf(t, base, src)
	require.ErrorContains(t, CopyFileNoReplaceVerified(fs, src, dst, proof), "inspect the open source handle")
}

// The copy leg's staging draw wedged mid-flight: the failure surfaces raw
// (no cross-device wrap), the source is never consumed, nothing publishes.
func TestCopyFileNoReplaceVerifiedStagingFailure(t *testing.T) {
	base, src, dst, body := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
		if strings.Contains(name, ".nrstg.") {
			return nil, errors.New("staging draw denied")
		}
		return base.OpenFile(name, flag, perm)
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := CopyFileNoReplaceVerified(fs, src, dst, proof)
	require.ErrorContains(t, err, "exclusive staging")
	got, rerr := afero.ReadFile(base, src)
	require.NoError(t, rerr)
	assert.Equal(t, body, string(got), "a failed stage never consumes the copy source")
	exists, _ := afero.Exists(base, dst)
	assert.False(t, exists)
	assertNoBoundResidue(t, base, "/")
}

// A source swapped before the open: the handle names the replacement, the
// proof refuses, and nothing stages or publishes.
func TestCopyFileNoReplaceVerifiedForeignSwapRefuses(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	proof, _ := verifyProofOf(t, base, src)
	renameSwap(t, base, src, filepath.FromSlash("/in/replacement.bin"), []byte("foreign replacement — different length"))
	err := CopyFileNoReplaceVerified(base, src, dst, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	assert.False(t, PublishRefusal(err))
	exists, _ := afero.Exists(base, dst)
	assert.False(t, exists, "the refused copy lands nothing")
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, "foreign replacement — different length", string(got), "the foreign entry is never touched")
	assertNoBoundResidue(t, base, "/")
}

// The copy leg's core F1 property: a rename-swap landing AFTER the verified
// open cannot retarget the stream — the pinned handle still stages the
// admitted object's bytes.
func TestCopyFileNoReplaceVerifiedPostOpenSwapPublishesAdmitted(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)
	victim := ""
	swapping := VerifiedSourceProof(func(path string, info os.FileInfo) error {
		if err := proof(path, info); err != nil {
			return err
		}
		victim = liveEntrySwap(t, fs, src, filepath.Join(root, "in", "replacement.bin"), []byte("the swapped-in replacement"))
		return nil
	})
	require.NoError(t, CopyFileNoReplaceVerified(fs, src, dst, swapping))
	if victim != "" {
		// Close-before-remove: the composite's verified source handle is
		// already closed here — a leaked pin wedges this remove on Windows.
		require.NoError(t, fs.Remove(victim))
	}
	got, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	assert.Equal(t, "admitted video bytes", string(got), "the pinned handle published the admitted bytes despite the swap")
	got, err = afero.ReadFile(fs, src)
	require.NoError(t, err)
	assert.Equal(t, "the swapped-in replacement", string(got), "the replacement entry is never read for the publish")
	assertNoBoundResidue(t, fs, root)
}

func TestCopyFileNoReplaceVerifiedHappyPath(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)
	require.NoError(t, CopyFileNoReplaceVerified(fs, src, dst, proof))
	got, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	assert.Equal(t, "admitted video bytes", string(got))
	exists, _ := afero.Exists(fs, src)
	assert.True(t, exists, "a copy never consumes its source")
	assertNoBoundResidue(t, fs, root)
}

// Nil proofs route to the legacy composites unchanged.
func TestVerifiedCompositesNilProofPassthrough(t *testing.T) {
	fs, src, dst, body := verifyFixture(t)
	require.NoError(t, MoveFileNoReplaceVerified(fs, src, dst, nil))
	got, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	assert.Equal(t, body, string(got))
	exists, _ := afero.Exists(fs, src)
	assert.False(t, exists)

	fs2, src2, dst2, body2 := verifyFixture(t)
	require.NoError(t, CopyFileNoReplaceVerified(fs2, src2, dst2, nil))
	got, err = afero.ReadFile(fs2, dst2)
	require.NoError(t, err)
	assert.Equal(t, body2, string(got))
	exists, _ = afero.Exists(fs2, src2)
	assert.True(t, exists, "the copy passthrough retains its source")
}
