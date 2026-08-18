package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-15 (P2) — "avoid replacing a
// racer on the create path": between the create path's Lstat-ENOENT and its
// publish, a foreign writer could create the destination, and a plain
// ReplaceFile would replace it with no backup and no ledger entry while the
// download reported success. The create path now publishes through
// fsutil.PublishNoReplace; an occupied destination is RECLASSIFIED into the
// armed-overwrite discipline (the racer's bytes are set aside + journaled
// like any pre-existing destination) instead of being destroyed.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w15DestRaceFs models a foreign writer claiming the destination INSIDE the
// create path's classify→publish window, plus the refusal the platform's
// no-replace primitive gives at the publish itself — renameat2
// (RENAME_NOREPLACE) on Linux, MoveFileExW without MOVEFILE_REPLACE_EXISTING
// on Windows, link(2)'s EEXIST elsewhere. The racer's bytes land first, then
// the rename refuses with an exists-class error, exactly once; every later
// rename (the set-aside handoff, the final install replace) forwards
// unchanged.
type w15DestRaceFs struct {
	afero.Fs
	dest   string
	racer  []byte
	armed  bool
	landed bool
}

func (f *w15DestRaceFs) Rename(oldname, newname string) error {
	if f.armed && !f.landed && filepath.Clean(newname) == filepath.Clean(f.dest) {
		f.landed = true
		if err := afero.WriteFile(f.Fs, f.dest, f.racer, 0o644); err != nil {
			return err
		}
		return &os.PathError{Op: "rename", Path: newname, Err: os.ErrExist}
	}
	return f.Fs.Rename(oldname, newname)
}

// w15PublishWedgeFs fails the destination publish with a non-exists error to
// pin the plain-failure leg (no reclassification).
type w15PublishWedgeFs struct {
	afero.Fs
	dest string
	err  error
}

func (f *w15PublishWedgeFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) {
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

// The finding's window: a racer claims the destination between the create
// classification and the publish. The no-replace publish refuses it, the
// destination is reclassified as present, and the armed-overwrite discipline
// sets the racer's bytes aside and journals them — the racer survives as the
// backup while the staged bytes install.
func TestInstallOverwritingW15_CreateRaceReclassifiesIntoArmedOverwrite(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W15-RACE"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	// Destination ABSENT — the create path.
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	fs := &w15DestRaceFs{Fs: base, dest: dest, racer: []byte("racer-bytes"), armed: true}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w15-race", recorder: recorder})
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced, "the racer reclassified the install as an overwrite")
	require.True(t, fs.landed, "the injected race fired")

	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, dest)), "staged bytes installed")
	require.Len(t, recorder.get(), 1, "the racer's set-aside journaled exactly once")
	firstBackup := backupCandidateW22(dest, "w15-race", 1)
	require.Equal(t, firstBackup, recorder.get()[0].backupPath)
	require.Equal(t, "racer-bytes", string(mustReadDownloaderW7(t, base, firstBackup)),
		"the racer's bytes are preserved as the armed backup")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the busy marker is released")
	_, stagedErr := base.Stat(staged)
	require.ErrorIs(t, stagedErr, os.ErrNotExist, "the staged file consumed")
}

// The same window without an armed ledger: reclassification lands on the
// present-destination refusal discipline (skip+warn) and the racer is
// untouched.
func TestInstallOverwritingW15_CreateRaceUnarmedLedgerRefuses(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W15-RACE-UNARMED"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	fs := &w15DestRaceFs{Fs: base, dest: dest, racer: []byte("racer-bytes"), armed: true}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "   ", recorder: recorder}) // unarmed: blank opID
	require.NoError(t, err)
	require.True(t, skipped, "the unarmed reclassification is the refusal class")
	require.True(t, replaced)
	require.Equal(t, "racer-bytes", string(mustReadDownloaderW7(t, base, dest)),
		"the racer's bytes are refused AND preserved, never clobbered")
	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, staged)),
		"a refused install leaves the staged file for the caller's cleanup")
	require.Empty(t, recorder.get(), "nothing journaled on the refusal")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}

// A create with no racer is the pre-wave-15 fast path: direct install, no
// journal, no backup, replaced=false.
func TestInstallOverwritingW15_NormalCreatePathUnaffected(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W15-CREATE"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w15-create", recorder: recorder})
	require.NoError(t, err)
	require.False(t, skipped)
	require.False(t, replaced, "a plain create still reports the absent classification")

	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, dest)))
	require.Empty(t, recorder.get(), "the create path journals nothing")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
	entries, readErr := afero.ReadDir(base, dir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "only the published destination remains (no staged/backup residue)")
}

// A non-collision publish failure (the destination still absent) surfaces as
// the install error — no reclassification loop on plain rename failures.
func TestInstallOverwritingW15_CreatePublishWedgeSurfacesError(t *testing.T) {
	resetBackupOrdinalW22(t)
	base := afero.NewMemMapFs()
	dir := "/out/W15-PUBWEDGE"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	sentinel := errors.New("w15 publish wedged")
	fs := &w15PublishWedgeFs{Fs: base, dest: dest, err: sentinel}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w15-pubwedge", recorder: recorder})
	require.ErrorIs(t, err, sentinel)
	require.NotErrorIs(t, err, fsutil.ErrPublishCollision)
	require.False(t, skipped)
	require.False(t, replaced)
	_, statErr := base.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "a wedged publish leaves the destination absent")
	require.Empty(t, recorder.get())
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}
