package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statFailingFile wedges the verified link's post-open handle Stat leg: the
// handle opens but refuses to describe itself.
type statFailingFile struct {
	afero.File
	err error
}

func (f statFailingFile) Stat() (os.FileInfo, error) { return nil, f.err }

// syslessInfo strips the kernel identity a lookup carried (the weak-probe
// shape of a wrapped filesystem): a STRONG admission must refuse to re-prove
// through it rather than degrade to the metadata legs.
type syslessInfo struct{ os.FileInfo }

func (syslessInfo) Sys() any { return nil }

// faithfulLink models link(2) on an identity-free filesystem: the destination
// names the SAME object as the source, so the weak legs (size + modtime) hold
// across the install.
func faithfulLink(t *testing.T, fs afero.Fs, src, dst string) error {
	t.Helper()
	info, err := fs.Stat(src)
	require.NoError(t, err)
	data, err := afero.ReadFile(fs, src)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, dst, data, 0o644))
	require.NoError(t, fs.Chtimes(dst, info.ModTime(), info.ModTime()))
	return nil
}

func linkFixture(t *testing.T) (afero.Fs, string, string) {
	t.Helper()
	fs, src, dst, _ := verifyFixture(t)
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	return fs, src, dst
}

// The nil-proof contract: pure by-name passthrough — no source open, no
// classification, no post-link proof, even when the link verb lands nothing.
func TestLinkFileNoReplaceVerifiedNilProofKeepsLegacyShape(t *testing.T) {
	fs, src, dst := linkFixture(t)
	var got struct{ old, new string }
	err := LinkFileNoReplaceVerified(fs, src, dst, func(oldname, newname string) error {
		got.old, got.new = oldname, newname
		return nil
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, src, got.old)
	assert.Equal(t, dst, got.new)
	exists, _ := afero.Exists(fs, dst)
	assert.False(t, exists, "the unbound leg never post-proves — the verb's answer stands")
}

// Lexical self with a bound proof is the adoption no-op, mirroring the
// move/copy verified twins — the link verb is never invoked.
func TestLinkFileNoReplaceVerifiedSelfNoOp(t *testing.T) {
	fs, src, _ := linkFixture(t)
	proof, _ := verifyProofOf(t, fs, src)
	called := false
	require.NoError(t, LinkFileNoReplaceVerified(fs, src, src, func(_, _ string) error {
		called = true
		return nil
	}, proof))
	assert.False(t, called)
	got, err := afero.ReadFile(fs, src)
	require.NoError(t, err)
	assert.Equal(t, "admitted video bytes", string(got))
}

// An occupied destination refuses typed before any source/destination touch —
// the verified move/copy twins' collision class.
func TestLinkFileNoReplaceVerifiedDestinationCollision(t *testing.T) {
	fs, src, dst := linkFixture(t)
	require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign occupant"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)
	called := false
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error {
		called = true
		return nil
	}, proof)
	require.ErrorIs(t, err, ErrPublishCollision)
	assert.True(t, PublishRefusal(err))
	assert.False(t, called)
	got, _ := afero.ReadFile(fs, dst)
	assert.Equal(t, "foreign occupant", string(got), "the occupied entry is never touched")
}

func TestLinkFileNoReplaceVerifiedMkdirFailure(t *testing.T) {
	base, src, dst, _ := verifyFixture(t)
	fs := &verifyHookFS{Fs: base, mkdirAll: func(string, os.FileMode) error {
		return errors.New("mkdir denied")
	}}
	proof, _ := verifyProofOf(t, base, src)
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error { return nil }, proof)
	require.ErrorContains(t, err, "verified link: create destination directory")
}

func TestLinkFileNoReplaceVerifiedSourceOpenFault(t *testing.T) {
	base, src, dst := linkFixture(t)
	fs := &verifyHookFS{Fs: base, open: func(string) (afero.File, error) {
		return nil, errors.New("open denied")
	}}
	proof, _ := verifyProofOf(t, base, src)
	called := false
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error {
		called = true
		return nil
	}, proof)
	require.ErrorContains(t, err, "verified link: open source")
	assert.False(t, called, "no link without a proven source")
}

func TestLinkFileNoReplaceVerifiedHandleStatFault(t *testing.T) {
	base, src, dst := linkFixture(t)
	fs := &verifyHookFS{Fs: base, open: func(name string) (afero.File, error) {
		file, err := base.Open(name)
		if err != nil {
			return nil, err
		}
		return statFailingFile{File: file, err: errors.New("describe denied")}, nil
	}}
	proof, _ := verifyProofOf(t, base, src)
	called := false
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error {
		called = true
		return nil
	}, proof)
	require.ErrorContains(t, err, "verified link: inspect the open source handle")
	assert.False(t, called)
}

// The pre-link proof refusal: the source already names a foreign object (the
// swap landed before the composite ran) — no link is ever issued and the
// foreign object stays byte-intact at the source name.
func TestLinkFileNoReplaceVerifiedRefusesSwappedSourceBeforeLink(t *testing.T) {
	base, src, dst := linkFixture(t)
	proof, _ := verifyProofOf(t, base, src)
	renameSwap(t, base, src, filepath.Join(filepath.Dir(src), "aside.bin"), []byte("replacement video — different size"))
	fs := &verifyHookFS{Fs: base}
	called := false
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error {
		called = true
		return nil
	}, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	assert.False(t, PublishRefusal(err), "a pre-publish admission refusal never classifies as a publish refusal")
	assert.False(t, PublishCompleted(err), "nothing reached the destination")
	assert.False(t, called, "the link verb must never run for an unadmitted source")
	exists, _ := afero.Exists(base, dst)
	assert.False(t, exists)
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, "replacement video — different size", string(got), "the foreign replacement is never consumed or displaced")
}

// The raw link failure passes through unwrapped so the caller's EXDEV /
// permission classification keeps working on the verified lane.
func TestLinkFileNoReplaceVerifiedLinkErrorPassthrough(t *testing.T) {
	fs, src, dst := linkFixture(t)
	proof, _ := verifyProofOf(t, fs, src)
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error { return syscall.EXDEV }, proof)
	require.ErrorIs(t, err, syscall.EXDEV)
	assert.False(t, PublishRefusal(err) || PublishCompleted(err))
}

// A nil-answer link that landed NOTHING (NotExist at the post-link lookup)
// proves no install stands at the destination — a plain refusal, never the
// doubt-as-published class and never an admission refusal.
func TestLinkFileNoReplaceVerifiedInstalledEntryVanished(t *testing.T) {
	fs, src, dst := linkFixture(t)
	proof, _ := verifyProofOf(t, fs, src)
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error { return nil }, proof)
	require.ErrorContains(t, err, "vanished before its identity proof")
	assert.False(t, PublishCompleted(err), "nothing provably stands at the destination")
	assert.False(t, errors.Is(err, ErrTakeAsideForeign), "no foreign object was ever installed")
	got, _ := afero.ReadFile(fs, src)
	assert.Equal(t, "admitted video bytes", string(got), "the source is never consumed by a link install")
}

// An indeterminate post-link lookup (the installed entry cannot be re-proven
// either way) keeps the doubt-as-published posture: the publish may stand and
// the bound unlink has no identity to act on, so nothing is unlinked.
func TestLinkFileNoReplaceVerifiedIndeterminateReproofKeepsCompleted(t *testing.T) {
	base, src, dst := linkFixture(t)
	proof, _ := verifyProofOf(t, base, src)
	linked := false
	fs := &verifyHookFS{Fs: base}
	fs.lstat = func(name string) (os.FileInfo, bool, error) {
		if linked && filepath.Clean(name) == filepath.Clean(dst) {
			return nil, false, errors.New("lookup wedged")
		}
		if l, ok := base.(afero.Lstater); ok {
			return l.LstatIfPossible(name)
		}
		info, err := base.Stat(name)
		return info, false, err
	}
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error {
		require.NoError(t, faithfulLink(t, base, src, dst))
		linked = true
		return nil
	}, proof)
	require.ErrorIs(t, err, ErrPublishCompleted)
	assert.False(t, errors.Is(err, ErrTakeAsideForeign))
	exists, _ := afero.Exists(base, dst)
	assert.True(t, exists, "an unproven entry is never unlinked by pathname")
}

// A swap that wins the open→link window lands a link to the REPLACEMENT; the
// post-link proof refuses it — and the rejected entry is RETAINED, never
// unlinked (codex P1, PRRT_kwDORn9KaM6npnwi): from the composite's vantage the
// post-link entry is indistinguishable from another writer's post-install
// successor (our install renamed aside, the name replanted inside the
// link→lstat window), so cleanup binds to the link operation's own provable
// identity — the admitted pre-link object — and an entry diverging from it is
// unproven. The doubt-as-published class joins the typed refusal so the
// caller's observe/rollback machinery reaps whatever stands; every object
// stays byte-intact under the composite's own resolution (the round-46
// precedent: unproven outcomes retain).
func TestLinkFileNoReplaceVerifiedPostLinkSwapRetainedUnproven(t *testing.T) {
	fs, src, dst := linkFixture(t)
	proof, _ := verifyProofOf(t, fs, src)
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error {
		return afero.WriteFile(fs, dst, []byte("foreign replacement — a different size"), 0o644)
	}, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorIs(t, err, ErrPublishCompleted, "the rejected entry may stand at the destination — the caller's rollback reaps it")
	got, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr, "the unproven entry is retained byte-intact — never an unlink authenticated against its own current identity")
	assert.Equal(t, "foreign replacement — a different size", string(got))
	got, _ = afero.ReadFile(fs, src)
	assert.Equal(t, "admitted video bytes", string(got))
	assertNoBoundResidue(t, fs, filepath.Dir(dst))
}

// The retained entry stays byte-intact even when the bound-unlink machinery
// could not have run anyway (the terminal claim wedged): retention of an
// unproven entry precedes any compensation attempt (codex P1,
// PRRT_kwDORn9KaM6npnwi), and BOTH classes still ride — the typed admission
// refusal the swap subject sees and the doubt-as-published class so the
// caller's observe/rollback machinery reaps whatever stands at the
// destination.
func TestLinkFileNoReplaceVerifiedUnprovenRetainKeepsBothClasses(t *testing.T) {
	base, src, dst := linkFixture(t)
	proof, _ := verifyProofOf(t, base, src)
	fs := &verifyHookFS{Fs: base, openFile: func(name string, flag int, perm os.FileMode) (afero.File, error) {
		if flag&os.O_EXCL != 0 && strings.Contains(name, ".vac.") {
			return nil, errors.New("terminal claim denied")
		}
		return base.OpenFile(name, flag, perm)
	}}
	err := LinkFileNoReplaceVerified(fs, src, dst, func(_, _ string) error {
		return afero.WriteFile(base, dst, []byte("foreign replacement — a different size"), 0o644)
	}, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorIs(t, err, ErrPublishCompleted, "the stranded install is caller-visible")
	exists, _ := afero.Exists(base, dst)
	assert.True(t, exists, "the rejected install stays recoverable for the caller's rollback")
	got, _ := afero.ReadFile(base, src)
	assert.Equal(t, "admitted video bytes", string(got))
}

// m8Zqb fail-closed posture on the link lane: a STRONG admission (kernel
// identity captured) whose post-link probe cannot re-prove at that strength
// refuses — the leg never degrades to size+modtime for an already-pinned
// strong identity — and the rejected install is compensated.
func TestLinkFileNoReplaceVerifiedStrongCaptureRejectsWeakProbe(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	require.NoError(t, base.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(base, src, []byte("admitted video bytes"), 0o644))
	info, err := base.Stat(src)
	require.NoError(t, err)
	if _, _, strong := BoundObjectIdentity(base, src, info); !strong {
		t.Skip("this target exposes no kernel identity — the weak-probe leg has nothing to refuse")
	}
	proof, _ := verifyProofOf(t, base, src)
	fs := &verifyHookFS{Fs: base}
	fs.lstat = func(name string) (os.FileInfo, bool, error) {
		var info os.FileInfo
		var err error
		if l, ok := base.(afero.Lstater); ok {
			info, _, err = l.LstatIfPossible(name)
		} else {
			info, err = base.Stat(name)
		}
		if info == nil || err != nil {
			return info, false, err
		}
		return syslessInfo{info}, false, nil
	}
	err = LinkFileNoReplaceVerified(fs, src, dst, os.Link, proof)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	exists, _ := afero.Exists(base, dst)
	assert.False(t, exists, "the unprovable install was unlinked, never retained")
}

// The real-link happy path on a kernel-identity filesystem: the installed
// entry provably ALIASES the admitted object — the post-link stat vs admitted
// identity is the actual guarantee, and the source stays put.
func TestLinkFileNoReplaceVerifiedOsFsLinkAliasesAdmittedObject(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)
	require.NoError(t, LinkFileNoReplaceVerified(fs, src, dst, os.Link, proof))
	srcInfo, err := os.Stat(src)
	require.NoError(t, err)
	dstInfo, err := os.Stat(dst)
	require.NoError(t, err)
	assert.True(t, os.SameFile(srcInfo, dstInfo), "the install aliases the admitted object — strong identity proven post-link")
	got, _ := afero.ReadFile(fs, src)
	assert.Equal(t, "admitted video bytes", string(got))
}

// The real-link mid-window swap on a kernel-identity filesystem: the swap
// lands between the verified open and the kernel's by-name link resolution,
// so link(2) binds the REPLACEMENT's inode. The post-link alias proof refuses
// the entry, and — diverging from the admitted pre-link identity, it could
// equally be another writer's successor replanted after our install was moved
// aside (codex P1, PRRT_kwDORn9KaM6npnwi) — it is RETAINED byte-intact, the
// doubt-as-published class joining the typed refusal for the caller's
// rollback machinery.
func TestLinkFileNoReplaceVerifiedOsFsMidWindowSwapRetainedUnproven(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	aside := filepath.Join(root, "in", "swapped-aside.bin")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)
	swapped := false
	err := LinkFileNoReplaceVerified(fs, src, dst, func(oldname, newname string) error {
		swapped = true
		require.NoError(t, fs.Rename(src, aside))
		require.NoError(t, afero.WriteFile(fs, src, []byte("replacement video — different bytes"), 0o644))
		return os.Link(oldname, newname)
	}, proof)
	require.True(t, swapped, "the swap actually landed inside the open→link window")
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorIs(t, err, ErrPublishCompleted, "the retained entry stays caller-visible for the rollback machinery")
	got, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr, "an entry diverging from the link operation's identity is never unlinked")
	assert.Equal(t, "replacement video — different bytes", string(got), "the entry stands as the swap's own bytes, retained byte-intact")
	got, _ = afero.ReadFile(fs, src)
	assert.Equal(t, "replacement video — different bytes", string(got), "the replacement stays byte-intact at the source name")
	got, _ = afero.ReadFile(fs, aside)
	assert.Equal(t, "admitted video bytes", string(got), "the admitted object was never consumed")
	assertNoBoundResidue(t, fs, root)
}

// codex P1 (PRRT_kwDORn9KaM6npnwi) — the flagged shape: the install lands as
// the admitted object's own alias, then ANOTHER writer renames the fresh link
// aside and plants a successor at dst inside the link→lstat window. The
// post-link proof fails on the successor as expected, but the cleanup must not
// authenticate against the successor's own identity — that unlink destroys
// another writer's file. The unproven entry is RETAINED byte-intact, the
// doubt-as-published class rides, and the moved-aside install stays
// recoverable at its new name.
func TestLinkFileNoReplaceVerifiedPostLinkSuccessorNeverUnlinked(t *testing.T) {
	base, src, dst := linkFixture(t)
	aside := filepath.Join(filepath.Dir(dst), "install-moved-aside.bin")
	proof, _ := verifyProofOf(t, base, src)
	successorPlanted := false
	err := LinkFileNoReplaceVerified(base, src, dst, func(_, _ string) error {
		require.NoError(t, faithfulLink(t, base, src, dst))
		require.NoError(t, base.Rename(dst, aside))
		require.NoError(t, afero.WriteFile(base, dst, []byte("another writer's successor — planted post-install"), 0o644))
		successorPlanted = true
		return nil
	}, proof)
	require.True(t, successorPlanted, "the successor genuinely claimed the destination inside the link→lstat window")
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorIs(t, err, ErrPublishCompleted, "the install may stand at another name — the caller's observe/rollback machinery reaps")
	got, readErr := afero.ReadFile(base, dst)
	require.NoError(t, readErr, "the successor is another writer's file — never destroyed by the failed admission proof's cleanup")
	assert.Equal(t, "another writer's successor — planted post-install", string(got))
	got, _ = afero.ReadFile(base, aside)
	assert.Equal(t, "admitted video bytes", string(got), "the install the successor's writer moved aside stays recoverable")
	got, _ = afero.ReadFile(base, src)
	assert.Equal(t, "admitted video bytes", string(got))
	assertNoBoundResidue(t, base, filepath.Dir(dst))
}

// The kernel-identity twin of the post-install successor window: link(2)
// landed a REAL alias of the admitted object, another process renamed it
// aside, and a successor claimed dst before the post-link lstat. The proof
// fails on the successor; the cleanup gate binds to the link operation's own
// identity (the admitted dev/ino captured pre-link), observes the divergence,
// and RETAINS the successor — the flagged construction authenticated
// UnlinkVerified against the successor's own identity and destroyed it.
func TestLinkFileNoReplaceVerifiedOsFsPostLinkSuccessorNeverUnlinked(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "in", "movie.mp4")
	dst := filepath.Join(root, "lib", "movie.mp4")
	aside := filepath.Join(root, "lib", "install-moved-aside.bin")
	require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted video bytes"), 0o644))
	proof, _ := verifyProofOf(t, fs, src)
	successorPlanted := false
	err := LinkFileNoReplaceVerified(fs, src, dst, func(oldname, newname string) error {
		if lerr := os.Link(oldname, newname); lerr != nil {
			return lerr
		}
		require.NoError(t, os.Rename(newname, aside))
		require.NoError(t, os.WriteFile(newname, []byte("another writer's successor — planted post-install"), 0o644))
		successorPlanted = true
		return nil
	}, proof)
	require.True(t, successorPlanted, "the successor genuinely claimed the destination inside the link→lstat window")
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorIs(t, err, ErrPublishCompleted)
	successor, readErr := os.ReadFile(dst)
	require.NoError(t, readErr, "the successor file is never destroyed by the cleanup")
	assert.Equal(t, "another writer's successor — planted post-install", string(successor))
	srcInfo, sErr := os.Stat(src)
	require.NoError(t, sErr)
	asideInfo, aErr := os.Stat(aside)
	require.NoError(t, aErr, "the moved-aside install stays recoverable")
	assert.True(t, os.SameFile(srcInfo, asideInfo), "the install is the admitted object, recoverable at the aside name")
	assertNoBoundResidue(t, fs, root)
}
