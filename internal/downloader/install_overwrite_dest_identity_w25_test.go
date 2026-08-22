package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-25 —
//
// finding 2 (journal ownership): RecordReplacement carries the set-aside
// backup's identity facts (size + mtime) captured right after the aside
// handoff, so history's removal gate can bind its unlink to the OWNED
// object. A capture failure rolls the set-aside back exactly like a record
// failure.
//
// finding 3 (confirm-rollback discipline): a journal-confirm failure rollback
// used to copy the backup onto the destination with REPLACE semantics — a
// foreign writer claiming the destination inside the install→confirm window
// had its bytes destroyed with no journal trace. The rollback now verifies
// the destination still names the JUST-INSTALLED object (dev/inode when
// exposed, size + mtime always) and otherwise REFUSES: foreign bytes stay
// byte-intact, the backup is retained in place, and the journal entry stays
// armed for sweep/revert arbitration.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w25JournalEntry is one journaled record as observed by the wave-25
// capturing ledger — including the stamped backup identity facts.
type w25JournalEntry struct {
	dest   string
	backup string
	facts  []models.ReplacementBackupFacts
}

// w25Ledger records every recorder call landing on an install.
type w25Ledger struct {
	mu        sync.Mutex
	records   []w25JournalEntry
	released  []string
	confirmed int
	pendings  int
	// confirmErr fails ConfirmReplacement; confirmHook runs INSIDE
	// ConfirmReplacement (the install→confirm window) before the error returns.
	confirmErr  error
	confirmHook func()
}

func (l *w25Ledger) RecordReplacement(_ context.Context, _, dest, backup string, facts ...models.ReplacementBackupFacts) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, w25JournalEntry{dest: dest, backup: backup, facts: append([]models.ReplacementBackupFacts(nil), facts...)})
	return nil
}

func (l *w25Ledger) ConfirmReplacement(context.Context, string, string, string) error {
	if l.confirmHook != nil {
		l.confirmHook()
	}
	l.mu.Lock()
	l.confirmed++
	l.mu.Unlock()
	return l.confirmErr
}

func (l *w25Ledger) ReleaseReplacement(_ context.Context, _, _, backup string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released = append(l.released, backup)
	return nil
}

func (l *w25Ledger) MarkReplacementRestorePendingKind(context.Context, string, string, string, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pendings++
	return nil
}

func (l *w25Ledger) recordCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.records)
}

func (l *w25Ledger) firstRecord() w25JournalEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.records[0]
}

// w25BusyReset cleans any durable busy marker state for dest between
// subtests sharing one process-wide registry.
func w25InstallFixture(t *testing.T, fs afero.Fs, destSeed []byte) (staged, dest string) {
	t.Helper()
	dir := t.Name()
	require.NoError(t, fs.MkdirAll("/w25/"+dir, 0o755))
	dest = "/w25/" + dir + "/poster.jpg"
	require.NoError(t, afero.WriteFile(fs, dest, destSeed, 0o644))
	staged = "/w25/" + dir + "/.staged-download"
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new bytes from cdn"), 0o644))
	return staged, dest
}

// Finding 2 pipeline: the armed overwrite journals the set-aside backup's
// size + mtime, facts that history's removal gate can verify later.
func TestInstallOverwritingW25_JournalsBackupIdentityFacts(t *testing.T) {
	fs := afero.NewMemMapFs()
	staged, dest := w25InstallFixture(t, fs, []byte("old bytes on disk"))
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{}

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w25-facts", recorder: ledger})
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced)
	require.Equal(t, 1, ledger.recordCount())

	rec := ledger.firstRecord()
	require.Len(t, rec.facts, 1, "the downloader must stamp the backup identity facts at record time")
	info, statErr := fs.Stat(rec.backup)
	require.NoError(t, statErr)
	require.Equal(t, info.Size(), rec.facts[0].Size, "journaled size binds the on-disk set-aside")
	require.Equal(t, info.ModTime().Unix(), rec.facts[0].ModUnix, "journaled mtime binds the on-disk set-aside")
	require.Equal(t, int64(len("old bytes on disk")), rec.facts[0].Size)
	require.NotZero(t, rec.facts[0].ModUnix, "a zero mtime reads as an unstamped legacy entry")
}

// Finding 3: a foreign writer claiming the destination inside the
// install→confirm window must keep its bytes; the rollback is REFUSED, the
// backup retained, the journal entry left armed.
func TestInstallOverwritingW25_ConfirmRollbackForeignSwapRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	foreign := []byte("a foreign writer planted these bytes mid-window") // deliberately longer than the install payload
	staged, dest := w25InstallFixture(t, fs, old)
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{
		confirmErr: errors.New("journal store wedged"),
		confirmHook: func() {
			// The foreign writer replaces the destination between the install
			// publish and the confirm failure.
			_ = fs.Remove(dest)
			_ = afero.WriteFile(fs, dest, foreign, 0o644)
		},
	}

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w25-swap", recorder: ledger})
	require.Error(t, err)
	require.Contains(t, err.Error(), "install-confirm failed")
	require.Contains(t, err.Error(), "rollback restore refused — destination no longer holds the installed object")
	require.False(t, skipped)
	require.True(t, replaced)

	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	require.Equal(t, foreign, got, "foreign destination bytes must survive the refused rollback")

	rec := ledger.firstRecord()
	backup, berr := afero.ReadFile(fs, rec.backup)
	require.NoError(t, berr, "the backup is retained in place for sweep/revert arbitration")
	require.Equal(t, old, backup, "the retained backup still carries the pre-existing bytes")
	require.Empty(t, ledger.released, "the armed entry is never released after a refused rollback")
	require.Zero(t, ledger.pendings)
}

// Finding 3: a foreign writer DELETING the destination in the window is the
// same mismatch class — the name no longer names the installed object.
func TestInstallOverwritingW25_ConfirmRollbackForeignDeleteRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, fs, old)
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{
		confirmErr: errors.New("journal store wedged"),
		confirmHook: func() {
			_ = fs.Remove(dest)
		},
	}

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w25-del", recorder: ledger})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rollback restore refused")
	exists, _ := afero.Exists(fs, dest)
	require.False(t, exists, "the foreign deletion is never 'healed' by the rollback")
	rec := ledger.firstRecord()
	backup, berr := afero.ReadFile(fs, rec.backup)
	require.NoError(t, berr)
	require.Equal(t, old, backup)
	require.Empty(t, ledger.released)
}

// Finding 3 normal path: confirm failure WITHOUT a foreign claim restores
// the pre-existing bytes exactly like the pre-wave-25 flow (the identity
// still matches, so the rollback proceeds).
func TestInstallOverwritingW25_ConfirmRollbackRestoresWhenIdentityMatches(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, fs, old)
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{confirmErr: errors.New("journal store wedged")}

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w25-normal", recorder: ledger})
	require.Error(t, err)
	require.Contains(t, err.Error(), "install-confirm failed, rolled back to pre-existing bytes")

	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	require.Equal(t, old, got, "rollback restored the pre-existing bytes")
	rec := ledger.firstRecord()
	exists, _ := afero.Exists(fs, rec.backup)
	require.False(t, exists, "the consumed backup was released with the retract")
	require.Len(t, ledger.released, 1, "the journal entry was retracted after the clean rollback")
}

// Finding F2 (wave-62, codex P2, PR#215): a foreign file swapped onto the
// BACKUP name between arm and the confirm-failure rollback used to be
// streamed into the media directory — copyBackupToDestBound re-proved only
// the CURRENT backup name, not the ORIGINALLY armed identity (captured
// pre-RecordReplacement). The rollback now authenticates the opened backup
// against the arm-time capture (dev/ino consistency + size/mtime) BEFORE any
// bytes reach dest and refuses typed ErrTakeAsideForeign: whatever currently
// sits at the backup name is preserved byte-intact and dest is left intact
// (installed bytes kept, journal entry armed for sweep/revert arbitration).
func TestInstallOverwritingW25_ConfirmRollbackBackupSubstitutedRefuses(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	foreign := []byte("a foreign file swapped onto the backup name mid-window") // deliberately different length
	staged, dest := w25InstallFixture(t, fs, old)
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{confirmErr: errors.New("journal store wedged")}
	ledger.confirmHook = func() {
		// Swap the backup name's occupant between arm and the rollback
		// copy — the armed identity still describes the original set-aside.
		rec := ledger.firstRecord()
		_ = fs.Remove(rec.backup)
		_ = afero.WriteFile(fs, rec.backup, foreign, 0o644)
	}

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w25-f2-sub", recorder: ledger})
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrTakeAsideForeign,
		"the substituted backup refuses the rollback copy typed")
	require.Contains(t, err.Error(), "install-confirm failed")
	require.Contains(t, err.Error(), "no longer names the armed set-aside")

	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	require.Equal(t, []byte("new bytes from cdn"), got,
		"dest keeps the installed bytes — the substituted backup never landed")

	rec := ledger.firstRecord()
	backup, berr := afero.ReadFile(fs, rec.backup)
	require.NoError(t, berr)
	require.Equal(t, foreign, backup,
		"the foreign occupant at the backup name is preserved byte-intact")
	require.Empty(t, ledger.released, "the armed entry is never released after a refused rollback")
	require.Zero(t, ledger.pendings)
}

// The dev/inode binding leg needs a real filesystem — MemMapFs exposes no
// inode identity. The foreign swap below replaces the destination object
// with a NEW inode of identical byte length and identical mtime, so ONLY the
// dev/inode comparison can catch it.
func TestInstallOverwritingW25_ConfirmRollbackForeignSwapDevIno(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode identity assertions are POSIX-shaped; Windows coverage runs through the size/mtime legs")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "poster.jpg")
	staged := filepath.Join(tmp, ".staged")
	payload := []byte("new bytes, same length!")
	require.NoError(t, os.WriteFile(staged, payload, 0o644))
	require.NoError(t, os.WriteFile(dest, []byte("old poster bytes here."), 0o644))

	// A pre-built foreign object with identical bytes: on CI filesystems
	// (overlayfs/tmpfs) remove+create at the SAME path routinely reuses the
	// freed inode, which would make the substitution undetectable. Renaming
	// a distinct pre-created file over the destination guarantees a foreign
	// inode regardless of allocation policy.
	foreign := filepath.Join(tmp, "foreign-plant.jpg")
	require.NoError(t, os.WriteFile(foreign, payload, 0o644))

	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{confirmErr: errors.New("journal store wedged")}
	ledger.confirmHook = func() {
		// Swap in the foreign inode carrying bytes of the SAME length — only
		// dev/inode can tell it apart from the just-installed object.
		info, err := os.Lstat(dest)
		require.NoError(t, err)
		require.NoError(t, os.Rename(foreign, dest))
		// Even a matching mtime must not rescue the substitution.
		require.NoError(t, os.Chtimes(dest, info.ModTime(), info.ModTime()))
	}

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w25-devino", recorder: ledger})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rollback restore refused",
		"a same-bytes same-mtime foreign inode must still mismatch via dev/inode")

	rec := ledger.firstRecord()
	backup, berr := os.ReadFile(rec.backup)
	require.NoError(t, berr, "the backup is retained for arbitration")
	require.Equal(t, []byte("old poster bytes here."), backup)
	require.Empty(t, ledger.released)
}

// Finding 2: a failed facts capture (the just-moved set-aside is unreadable)
// rolls the set-aside back exactly like a record failure — the destination
// bytes come back, no journal entry exists, no backup residue remains.
type w25BackupStatFailFs struct {
	afero.Fs
	err error
}

// Stat fails exactly for the populated set-aside: the claim-phase candidates
// (absent, 0-byte placeholders) read through while the CONTENT-carrying
// backup — non-empty, the only .dlbak the capture can target — trips the
// injected failure.
func (f *w25BackupStatFailFs) Stat(name string) (os.FileInfo, error) {
	info, err := f.Fs.Stat(name)
	if err == nil && strings.Contains(name, ".dlbak.") && info != nil && info.Size() > 0 {
		return nil, f.err
	}
	return info, err
}

func TestInstallOverwritingW25_FactsCaptureFailureRollsBackSetAside(t *testing.T) {
	base := afero.NewMemMapFs()
	sentinel := errors.New("w25 identity capture unavailable")
	fs := &w25BackupStatFailFs{Fs: base, err: sentinel}
	staged, dest := w25InstallFixture(t, fs, []byte("old bytes on disk"))
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{}

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w25-capture-fail", recorder: ledger})
	require.Error(t, err)
	require.True(t, replaced)
	require.False(t, skipped)
	require.Contains(t, err.Error(), "revert-ledger record failed")
	require.Contains(t, err.Error(), "capture backup identity facts")
	require.ErrorIs(t, err, sentinel)

	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	require.Equal(t, []byte("old bytes on disk"), got, "the set-aside rolled the destination back")
	require.Zero(t, ledger.recordCount(), "nothing was journaled without verifiable ownership facts")
	entries, _ := afero.ReadDir(fs, filepath.Dir(dest))
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".dlbak.", "no backup residue after the compensated arm")
	}
}

// Unit-level identity helper legs: every refusal classification.

type w25StatErrFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w25StatErrFs) Stat(name string) (os.FileInfo, error) {
	if name == f.victim {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

type w25NilStatFs struct{ afero.Fs }

func (f *w25NilStatFs) Stat(string) (os.FileInfo, error) { return nil, nil }

func TestW25_InstalledDestIdentity_CaptureLegs(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/w25u/file", []byte("payload"), 0o644))
	require.NoError(t, base.MkdirAll("/w25u/dir", 0o755))

	id := captureInstalledDestIdentity(base, "/w25u/file")
	require.True(t, id.known, "a regular file captures a known identity")
	require.Equal(t, int64(len("payload")), id.size)

	unknown := captureInstalledDestIdentity(base, "/w25u/dir")
	require.False(t, unknown.known, "a directory cannot be the installed object")

	unkFs := &w25StatErrFs{Fs: base, victim: "/w25u/file", err: errors.New("stat wedged")}
	unknown = captureInstalledDestIdentity(unkFs, "/w25u/file")
	require.False(t, unknown.known, "a stat failure fails closed to the unknown identity")

	nilFs := &w25NilStatFs{Fs: base}
	unknown = captureInstalledDestIdentity(nilFs, "/w25u/file")
	require.False(t, unknown.known, "a nil stat answer fails closed")
}

func TestW25_DestStillHoldsInstalledObject_Legs(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/w25m/file", []byte("payload"), 0o644))
	id := captureInstalledDestIdentity(base, "/w25m/file")
	require.True(t, id.known)

	require.False(t, destStillHoldsInstalledObject(base, "/w25m/file", installedDestIdentity{}),
		"an uncaptured (unknown) identity can never verify")
	require.True(t, destStillHoldsInstalledObject(base, "/w25m/file", id),
		"the untouched destination still names the installed object")

	unkFs := &w25StatErrFs{Fs: base, victim: "/w25m/file", err: errors.New("stat wedged")}
	require.False(t, destStillHoldsInstalledObject(unkFs, "/w25m/file", id),
		"a read failure cannot verify the installed object")

	require.NoError(t, afero.WriteFile(base, "/w25m/file", []byte("payload-longer"), 0o644))
	require.False(t, destStillHoldsInstalledObject(base, "/w25m/file", id),
		"a rewritten destination (new size) no longer names the installed object")

	require.NoError(t, base.MkdirAll("/w25m/dir", 0o755))
	require.False(t, destStillHoldsInstalledObject(base, "/w25m/dir", id),
		"a directory occupant never names the installed object")
}

func TestW25_CaptureReplacementBackupFacts_Legs(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/w25f/backup", []byte("payload"), 0o644))

	facts, err := captureReplacementBackupFacts(base, "/w25f/backup")
	require.NoError(t, err)
	require.Equal(t, int64(len("payload")), facts.Size)
	require.NotZero(t, facts.ModUnix)

	errFs := &w25StatErrFs{Fs: base, victim: "/w25f/backup", err: errors.New("stat wedged")}
	_, err = captureReplacementBackupFacts(errFs, "/w25f/backup")
	require.Error(t, err, "a stat failure fails the capture")

	nilFs := &w25NilStatFs{Fs: base}
	_, err = captureReplacementBackupFacts(nilFs, "/w25f/backup")
	require.Error(t, err, "a nil stat answer fails the capture")
}
