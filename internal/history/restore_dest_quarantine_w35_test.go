package history

// POSTER-WRITE-HARDENING wave-35 (codex local review round 5, PR#215): the
// restore-undo destination unlink moves through the shared quarantine
// construction (claimed O_EXCL sibling name, verified object moved aside,
// re-proven, only the quarantine name unlinked, every wedge compensated
// NO-REPLACE). These legs pin the finding end-to-end:
//
//   - a foreign substitution inside the wave-34 verdict→unlink window is
//     REFUSED byte-intact (dev/inode and metadata legs, OsFs);
//   - the plain path still unlinks exactly the verified object, and a plant
//     landing on the destination name mid-flow survives (OsFs);
//   - a wedge after the move compensates the verified bytes back onto the
//     destination, and the compensation itself is substitution-safe
//     (no-replace collision keeps the verified object recoverable at the
//     quarantine name);
//   - the claim/move/gate wedge legs remove nothing.

import (
	"crypto/sha256"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w35DestLstatFs scripts the destination's no-follow lookup — the wave-35
// gate's mode/indeterminate legs — while every other name passes through.
type w35DestLstatFs struct {
	afero.Fs
	dest string
	info os.FileInfo
	err  error
}

func (f *w35DestLstatFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.dest {
		return f.info, false, f.err
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		info, _, err := ls.LstatIfPossible(name)
		return info, false, err
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// w35MoveWedgeFs fails the quarantining rename of the victim (its dest +
// ".dlq." sibling) — the move leg's failure instant.
type w35MoveWedgeFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w35MoveWedgeFs) Rename(oldname, newname string) error {
	if strings.Contains(newname, f.victim+backupQuarantineSuffix) {
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

// The finding's headline, metadata leg: dest stopped naming the published
// restore object between the wave-34 verdict and the unlink — different
// bytes answer the name now. The gate refuses, the foreign occupant
// survives byte-intact, and nothing is ever moved or unlinked.
func TestRestoredDestQuarantineW35_SubstitutedDestRefusedForeignSurvives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dev/inode identity and rename-over semantics are POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := tmp + "/poster.jpg"
	require.NoError(t, os.WriteFile(dest, []byte("restored bytes"), 0o640))
	pubInfo, err := os.Lstat(dest)
	require.NoError(t, err)
	id := restoredDestIdentityFrom(pubInfo)

	// The foreign writer substitutes dest inside the verdict→unlink window.
	require.NoError(t, os.Remove(dest))
	require.NoError(t, os.WriteFile(dest, []byte("foreign plant — much longer than before"), 0o644))

	err = removeRestoredDestQuarantined(base, dest, "w35 unit", id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refused the undo unlink")
	require.Contains(t, err.Error(), "foreign bytes preserved")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign plant — much longer than before", string(got),
		"the pre-wave-35 pathname unlink would have destroyed the foreign bytes")
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	require.Len(t, entries, 1, "nothing was quarantined or removed besides the refusal itself")
}

// Same-length substitution with replayed timestamps (CI-1 + wave-36 finding
// F2): remove+create on CI runners routinely REUSES the freed inode, so the
// dev/inode + metadata binding can bless the foreign plant — only the
// wave-36 content gate separates it deterministically on every platform and
// on inode-reusing filesystems alike (where the inode was NOT reused, the
// dev/inode leg refuses first; both answers are the same refusal class).
func TestRestoredDestQuarantineW35_SubstitutedDestContentMismatchRefused(t *testing.T) {
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := tmp + "/poster.jpg"
	published := []byte("abcdefghij")
	require.NoError(t, os.WriteFile(dest, published, 0o640))
	pubInfo, err := os.Lstat(dest)
	require.NoError(t, err)
	id := restoredDestIdentityFromContent(pubInfo, sha256.Sum256(published))

	// Foreign plant with identical size AND replayed mtime (and, on
	// inode-reusing filesystems, the SAME dev/inode): metadata
	// identity alone would bless it; the published-bytes digest refuses.
	require.NoError(t, os.Remove(dest))
	require.NoError(t, os.WriteFile(dest, []byte("KLMNOPQRST"), 0o644))
	require.NoError(t, os.Chtimes(dest, pubInfo.ModTime(), pubInfo.ModTime()))

	err = removeRestoredDestQuarantined(base, dest, "w35 unit", id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refused the undo unlink of restored destination")
	require.Contains(t, err.Error(), "foreign bytes preserved")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "KLMNOPQRST", string(got), "the foreign plant survives the refused undo")
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	require.Len(t, entries, 1, "nothing was quarantined or removed besides the refusal itself")
}

// The dev/inode pre-move gate leg, deterministically covered: the scripted
// no-follow lookup answers a genuinely-foreign object's FileInfo (same size,
// replayed mtime — two simultaneously-existing real files, so the inode
// comparison can never collapse) while the on-disk destination is the
// published object untouched.
func TestRestoredDestQuarantineW35_ScriptedDevInodeMismatchRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dev/inode identity is POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := tmp + "/poster.jpg"
	require.NoError(t, os.WriteFile(dest, []byte("abcdefghij"), 0o640))
	pubInfo, err := os.Lstat(dest)
	require.NoError(t, err)
	foreign := tmp + "/foreign"
	require.NoError(t, os.WriteFile(foreign, []byte("KLMNOPQRST"), 0o644))
	require.NoError(t, os.Chtimes(foreign, pubInfo.ModTime(), pubInfo.ModTime()))
	foreignInfo, ferr := os.Lstat(foreign)
	require.NoError(t, ferr)

	fs := &w35DestLstatFs{Fs: base, dest: dest, info: foreignInfo}
	err = removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentityFrom(pubInfo))
	require.Error(t, err)
	require.Contains(t, err.Error(), "dev/inode mismatch")
	require.Equal(t, "abcdefghij", string(mustRead2(t, base, dest)),
		"the real destination is untouched — the scripted answer above is what the gate refused")
}

// The wave-36 hash-open wedge: the occupant cannot even be read no-follow —
// indeterminate, so the undo fails closed BEFORE anything moves.
func TestRestoredDestQuarantineW35_HashOpenFailureBindsNothing(t *testing.T) {
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := tmp + "/poster.jpg"
	published := []byte("abcdefghij")
	require.NoError(t, os.WriteFile(dest, published, 0o640))
	pubInfo, err := os.Lstat(dest)
	require.NoError(t, err)
	id := restoredDestIdentityFromContent(pubInfo, sha256.Sum256(published))

	sentinel := errors.New("w36 dest open wedged")
	fs := &w36DestOpenFailFs{Fs: base, dest: dest, err: sentinel}
	err = removeRestoredDestQuarantined(fs, dest, "w35 unit", id)
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "cannot bind restored destination")
	require.Equal(t, "abcdefghij", string(mustRead2(t, base, dest)), "indeterminate content binds nothing")
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	require.Len(t, entries, 1, "nothing was claimed, moved, or removed")
}

// The published-bytes binding accepts the genuine object end-to-end: a
// hashed identity unlinked cleanly with nothing else touched.
func TestRestoredDestQuarantineW35_ContentBoundPlainUnlink(t *testing.T) {
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := tmp + "/poster.jpg"
	published := []byte("restored bytes")
	require.NoError(t, os.WriteFile(dest, published, 0o640))
	pubInfo, err := os.Lstat(dest)
	require.NoError(t, err)
	id := restoredDestIdentityFromContent(pubInfo, sha256.Sum256(published))

	require.NoError(t, removeRestoredDestQuarantined(base, dest, "w35 unit", id))
	_, serr := os.Lstat(dest)
	require.ErrorIs(t, serr, os.ErrNotExist)
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	require.Empty(t, entries, "the quarantined verified object was unlinked")
}

// The content-mismatch leg DETERMINISTICALLY (every platform, no
// inode-reuse dependence): the scripted no-follow lookup answers the
// published object's OWN FileInfo — dev/inode AND metadata pass by
// construction — while the on-disk destination carries foreign bytes with a
// replayed mtime. Only the published-bytes digest refuses the undo.
func TestRestoredDestQuarantineW36_ContentMismatchAfterIdentityMatchRefused(t *testing.T) {
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := tmp + "/poster.jpg"
	published := []byte("abcdefghij")
	require.NoError(t, os.WriteFile(dest, published, 0o640))
	pubInfo, err := os.Lstat(dest)
	require.NoError(t, err)
	id := restoredDestIdentityFromContent(pubInfo, sha256.Sum256(published))

	// The swap: same size, replayed mtime, scripted-IDENTICAL identity — and
	// different bytes.
	require.NoError(t, os.Remove(dest))
	require.NoError(t, os.WriteFile(dest, []byte("KLMNOPQRST"), 0o644))
	require.NoError(t, os.Chtimes(dest, pubInfo.ModTime(), pubInfo.ModTime()))
	fs := &w35DestLstatFs{Fs: base, dest: dest, info: pubInfo}

	err = removeRestoredDestQuarantined(fs, dest, "w35 unit", id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "occupant content no longer matches the published restore bytes")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "KLMNOPQRST", string(got), "the foreign plant survives the refused undo")
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	require.Len(t, entries, 1, "nothing was claimed, moved, or removed besides the refusal itself")
}

// w36DestOpenFailFs fails the no-follow open of the destination while every
// other call passes through — the wave-36 content-gate's hash-open wedge.
type w36DestOpenFailFs struct {
	afero.Fs
	dest string
	err  error
}

func (f *w36DestOpenFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == f.dest && flag&os.O_WRONLY == 0 && flag&os.O_RDWR == 0 && flag&os.O_CREATE == 0 {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// The plain path: dest still names the published object — it is quarantined,
// re-verified, and unlinked; NOTHING else is touched. A racer planting on
// the destination name mid-flow (after the quarantining move) survives,
// because only the quarantine name is ever unlinked.
func TestRestoredDestQuarantineW35_PlainPathUnlinksVerifiedObject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-over semantics are POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := tmp + "/poster.jpg"
	require.NoError(t, os.WriteFile(dest, []byte("restored bytes"), 0o640))
	pubInfo, err := os.Lstat(dest)
	require.NoError(t, err)

	fs := &w26RenameHookFs{Fs: base, afterMove: func() {
		// The plant lands on the destination name between the quarantine
		// move and the verified unlink — under the pre-wave-35 pathname
		// unlink this would have been the Remove target.
		require.NoError(t, os.WriteFile(dest, []byte("foreign plant mid-flow"), 0o644))
	}}

	require.NoError(t, removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentityFrom(pubInfo)),
		"the verified object was quarantined + re-verified + removed — the removal itself succeeds")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign plant mid-flow", string(got),
		"the plant at the destination SURVIVES — never the quarantine unlink's target")
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), backupQuarantineSuffix, "the quarantined verified object was removed")
	}
}

// A substitution inside the gate→move window moves the plant instead of the
// verified object; the post-move re-verify catches it, NOTHING is unlinked,
// and the compensation moves the plant back onto the destination name.
func TestRestoredDestQuarantineW35_SubstitutionBeforeMoveRefusesAndPreserves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-over semantics are POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := tmp + "/poster.jpg"
	require.NoError(t, os.WriteFile(dest, []byte("restored bytes"), 0o640))
	pubInfo, err := os.Lstat(dest)
	require.NoError(t, err)

	fs := &w26RenameHookFs{Fs: base, beforeMove: func() {
		require.NoError(t, os.Remove(dest))
		require.NoError(t, os.WriteFile(dest, []byte("foreign plant substituted before the move"), 0o644))
	}}

	err = removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentityFrom(pubInfo))
	require.Error(t, err)
	var refused *BackupRemovalRefusedError
	require.ErrorAs(t, err, &refused,
		"typed proven-foreign refusal on every platform (the dev/inode vs metadata identity leg depends on whether the runner's filesystem reused the substituted inode)")
	require.Contains(t, refused.Reason, "the verified object")
	require.Contains(t, refused.Reason, "foreign bytes preserved")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign plant substituted before the move", string(got),
		"the compensation put the plant back NO-REPLACE — foreign bytes kept")
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	require.Len(t, entries, 1, "the quarantine name is consumed by the compensation")
}

// The metadata gate leg: a filesystem exposing no dev/inode (MemMap)
// still refuses a substituted occupant whose bytes differ, before anything
// is moved or unlinked. MemMap FileInfos are LIVE views of their object,
// so the published identity rides an untouched snapshot file standing in
// for the wave-31 publish-time stat.
func TestRestoredDestQuarantineW35_KnownIdentityMetadataMismatchRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w35k", 0o755))
	dest := "/w35k/poster.jpg"
	require.NoError(t, afero.WriteFile(base, "/w35k/published", []byte("restored bytes"), 0o644))
	pubInfo, err := base.Stat("/w35k/published")
	require.NoError(t, err)
	id := restoredDestIdentityFrom(pubInfo)
	require.NoError(t, afero.WriteFile(base, dest, []byte("foreign plant — much longer"), 0o644))

	err = removeRestoredDestQuarantined(base, dest, "w35 unit", id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no longer matches the published restore object")
	require.Equal(t, "foreign plant — much longer", string(mustRead2(t, base, dest)),
		"the substituted occupant survives the refused undo")
	require.Empty(t, w26DirQuarNames(t, base, "/w35k"))
}

// MemMap/wrapper posture (no provable publish identity): the verified object
// is snapshot-bound and unlinked — the plain undo path.
func TestRestoredDestQuarantineW35_UnknownIdentityPlainUnlink(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w35m", 0o755))
	dest := "/w35m/poster.jpg"
	require.NoError(t, afero.WriteFile(fs, dest, []byte("restored bytes"), 0o644))

	require.NoError(t, removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentity{}))
	exists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.False(t, exists)
	require.Empty(t, w26DirQuarNames(t, fs, "/w35m"))
}

// The unlink-time wedge compensates NO-REPLACE: the verified object returns
// to the destination name and nothing is lost (the production callers warn
// with the wrapped unlink error).
func TestRestoredDestQuarantineW35_UnlinkWedgeRestoresDestination(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w35u", 0o755))
	dest := "/w35u/poster.jpg"
	require.NoError(t, afero.WriteFile(base, dest, []byte("restored bytes"), 0o644))
	fs := &removeFailFs{Fs: base, victim: dest}

	err := removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentity{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "remove wedged")
	require.Equal(t, "restored bytes", string(mustRead2(t, base, dest)),
		"the compensation moved the verified object back onto the destination name")
	require.Empty(t, w26DirQuarNames(t, base, "/w35u"))
}

// Rollback under substitution: the compensation rename is itself
// substitution-safe (no-replace) — a racer occupying the destination name at
// the rollback instant keeps its bytes, and the VERIFIED object stays
// recoverable at the quarantine name for manual recovery.
func TestRestoredDestQuarantineW35_RollbackCollisionKeepsBoth(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w35r", 0o755))
	dest := "/w35r/poster.jpg"
	require.NoError(t, afero.WriteFile(base, dest, []byte("restored bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/w35r/foreign", []byte("a much longer foreign payload"), 0o644))
	foreignInfo, err := base.Stat("/w35r/foreign")
	require.NoError(t, err)

	fs := &w32QuarFs{Fs: base}
	fs.lstat = func(call int, name string) (os.FileInfo, error) {
		if call == 2 {
			// The unlink-time re-verify diverges AND the racer claims the
			// destination name before the compensation runs.
			require.NoError(t, afero.WriteFile(base, dest, []byte("foreign occupant at dest"), 0o644))
			return foreignInfo, nil
		}
		return w32RestoreReadsReal(fs)(call, name)
	}

	err = removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentity{})
	require.Error(t, err)
	require.Equal(t, "foreign occupant at dest", string(mustRead2(t, base, dest)),
		"the no-replace compensation never clobbers the racer's occupant")
	names := w26DirQuarNames(t, base, "/w35r")
	require.Len(t, names, 1, "the verified object stays recoverable at the quarantine name")
	require.Equal(t, "restored bytes", string(mustRead2(t, base, "/w35r/"+names[0])))
}

// The post-move re-verify mismatch refuses typed and compensates the
// verified object back onto the destination name — nothing is unlinked.
func TestRestoredDestQuarantineW35_ReverifyMismatchRestoresOriginalName(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w35v", 0o755))
	dest := "/w35v/poster.jpg"
	require.NoError(t, afero.WriteFile(base, dest, []byte("restored bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/w35v/foreign", []byte("a much longer foreign payload"), 0o644))
	foreignInfo, err := base.Stat("/w35v/foreign")
	require.NoError(t, err)

	fs := &w32QuarFs{Fs: base}
	fs.lstat = func(call int, name string) (os.FileInfo, error) {
		if call == 1 {
			return foreignInfo, nil // the moved object is not the verified one
		}
		return w32RestoreReadsReal(fs)(call, name)
	}

	err = removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentity{})
	require.Error(t, err)
	var refused *BackupRemovalRefusedError
	require.ErrorAs(t, err, &refused, "the re-verify mismatch keeps the typed proven-foreign class")
	require.Equal(t, "restored bytes", string(mustRead2(t, base, dest)),
		"the rollback rename restored the original destination name")
	require.Empty(t, w26DirQuarNames(t, base, "/w35v"))
}

// A quarantined vanished-under-us answer is the typed indeterminate
// retention (shared wave-32 sentinel): dest is absent, nothing consumed.
func TestRestoredDestQuarantineW35_VanishedAfterMoveIsTyped(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w35n", 0o755))
	dest := "/w35n/poster.jpg"
	require.NoError(t, afero.WriteFile(base, dest, []byte("restored bytes"), 0o644))

	fs := &w32QuarFs{Fs: base}
	fs.lstat = func(call int, name string) (os.FileInfo, error) {
		if call == 1 {
			return nil, afero.ErrFileNotFound
		}
		return w32RestoreReadsReal(fs)(call, name)
	}

	err := removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentity{})
	require.ErrorIs(t, err, errReplacementBackupQuarantineVanished)
	exists, eerr := afero.Exists(base, dest)
	require.NoError(t, eerr)
	require.False(t, exists, "the verified bytes vanished unownably — no move-back ran")
}

// The pre-move gate legs remove nothing: an absent destination is
// indeterminate, and a non-regular occupant is refused untouched.
func TestRestoredDestQuarantineW35_GateLegsRemoveNothing(t *testing.T) {
	t.Run("absent destination is indeterminate", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/w35g1", 0o755))
		err := removeRestoredDestQuarantined(fs, "/w35g1/never-existed", "w35 unit", restoredDestIdentity{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot bind restored destination")
	})

	t.Run("symlink occupant is refused untouched", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/w35g2", 0o755))
		dest := "/w35g2/poster.jpg"
		require.NoError(t, afero.WriteFile(base, dest, []byte("real bytes"), 0o644))
		linkRoot := t.TempDir()
		require.NoError(t, os.Symlink("nowhere", linkRoot+"/link"))
		linkInfo, lerr := os.Lstat(linkRoot + "/link")
		require.NoError(t, lerr)
		fs := &w35DestLstatFs{Fs: base, dest: dest, info: linkInfo}

		err := removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentity{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no longer addresses the published regular file")
		require.Equal(t, "real bytes", string(mustRead2(t, base, dest)), "the occupant is untouched")
		require.Empty(t, w26DirQuarNames(t, base, "/w35g2"))
	})
}

// Reservation and move wedges remove nothing: the claim failure leaves the
// destination byte-intact, and a failed quarantining rename cleans only OUR
// reservation placeholder.
func TestRestoredDestQuarantineW35_ClaimAndMoveWedgesRemoveNothing(t *testing.T) {
	t.Run("quarantine name claim failure", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/w35c", 0o755))
		dest := "/w35c/poster.jpg"
		require.NoError(t, afero.WriteFile(fs, dest, []byte("restored bytes"), 0o644))
		sentinel := errors.New("w35 entropy wedged")
		prev := backupQuarantineRandReader
		backupQuarantineRandReader = w26ErrReader{err: sentinel}
		t.Cleanup(func() { backupQuarantineRandReader = prev })

		err := removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentity{})
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "restored bytes", string(mustRead2(t, fs, dest)))
	})

	t.Run("quarantining move failure cleans the reservation", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/w35w", 0o755))
		dest := "/w35w/poster.jpg"
		require.NoError(t, afero.WriteFile(base, dest, []byte("restored bytes"), 0o644))
		sentinel := errors.New("w35 rename wedged")
		fs := &w35MoveWedgeFs{Fs: base, victim: dest, err: sentinel}

		err := removeRestoredDestQuarantined(fs, dest, "w35 unit", restoredDestIdentity{})
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "restored bytes", string(mustRead2(t, base, dest)), "a failed move relocated nothing")
		require.Empty(t, w26DirQuarNames(t, base, "/w35w"), "our claim placeholder is cleaned, nothing else")
	})
}
