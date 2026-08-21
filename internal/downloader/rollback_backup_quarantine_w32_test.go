package downloader

// POSTER-WRITE-HARDENING wave-32 (codex local review round 2, PR#215 findings
// R1+R4) — downloader-side coverage of the rollback quarantine port: the
// claim legs, the verified move, the post-move re-verify arms, the
// unlink-time re-binding arms (a watcher swapping the quarantine name
// mid-window is caught; ENOENT at Remove time is indeterminate retention,
// never consumed), the hold lifecycle, and the confirm-failure rollback
// pipeline legs end-to-end (post-quarantine destination divergence and the
// vanished-unlink failure posture).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w32RollbackQuarFs scripts the rollback quarantine fs surface: armed latches
// on the quarantining PUBLISH rename (wave-42: the conditional handoff issues
// two suffix renames — the take-aside (suffix→suffix) and the publish
// (src→suffix) — the scripted surface keys on the publish, when the verified
// object lands at the quarantine name), post-arm quarantine-name lookups
// route through lstat by call number (call 1 = the post-move re-verify,
// call 2 = the unlink-time re-verify), Remove/OpenFile wedge the named
// surfaces, and lstatAny wedges the BACKUP-name inspect leg.
type w32RollbackQuarFs struct {
	afero.Fs
	armed    bool
	quarName string
	lookups  int
	lstat    func(call int, name string) (os.FileInfo, error)
	removeFn func(name string) error
	openFn   func(name string) (afero.File, bool, error) // handled=true when substituted
}

func (f *w32RollbackQuarFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, rollbackQuarantineSuffix) && !strings.Contains(oldname, rollbackQuarantineSuffix) {
		f.armed = true
		f.quarName = newname
	}
	return err
}

func (f *w32RollbackQuarFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.armed && name == f.quarName {
		f.lookups++
		if f.lstat != nil {
			info, err := f.lstat(f.lookups, name)
			return info, false, err
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *w32RollbackQuarFs) Remove(name string) error {
	// Wave-r19: the verified unlink runs the bound terminal unlink
	// (vacate→rebind→unlink terminal), so the scripted victim is the
	// object-bearing terminal Remove — a .dlq.-bearing name holding the
	// verified object after the vacate — not the vacated quarantine name.
	// The take-aside's 0-byte placeholder removes (warn-only) and the
	// bound-unlink's own 0-byte terminal-placeholder release fall through
	// (size 0); only the object-bearing remove fires removeFn.
	if f.armed && f.removeFn != nil && strings.Contains(name, rollbackQuarantineSuffix) {
		if info, err := f.Fs.Stat(name); err == nil && info.Size() > 0 {
			return f.removeFn(name)
		}
	}
	return f.Fs.Remove(name)
}

func (f *w32RollbackQuarFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.openFn != nil {
		if file, handled, err := f.openFn(name); handled {
			return file, err
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func w32RollbackBackup(t *testing.T, fs afero.Fs, backup, content string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(backup), 0o755))
	require.NoError(t, afero.WriteFile(fs, backup, []byte(content), 0o644))
}

func w32RealReads(f *w32RollbackQuarFs) func(call int, name string) (os.FileInfo, error) {
	return func(_ int, name string) (os.FileInfo, error) {
		if ls, ok := f.Fs.(afero.Lstater); ok {
			info, _, err := ls.LstatIfPossible(name)
			return info, err
		}
		return f.Fs.Stat(name)
	}
}

func w32RollbackQuarNames(t *testing.T, fs afero.Fs, dir string) []string {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if strings.Contains(e.Name(), rollbackQuarantineSuffix) {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestRemoveRollbackBackupW32_UnlinkTimeLegs(t *testing.T) {
	const backup = "/w32du/poster.jpg.dlbak.abcd"

	t.Run("unlink-time ENOENT is typed indeterminate retention", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32RollbackQuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return nil, afero.ErrFileNotFound
			}
			return w32RealReads(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w32 unit")
		require.ErrorIs(t, err, errRollbackQuarantineVanished)
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist)
		require.Len(t, w32RollbackQuarNames(t, base, "/w32du"), 1)
	})

	t.Run("unlink-time indeterminate lookup restores the journaled name", func(t *testing.T) {
		sentinel := errors.New("rollback unlink-time lstat wedged")
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32RollbackQuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return nil, sentinel
			}
			return w32RealReads(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w32 unit")
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(readW31(t, base, backup)))
		require.Empty(t, w32RollbackQuarNames(t, base, "/w32du"))
	})

	t.Run("unlink-time nil answer refuses typed and restores", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32RollbackQuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return nil, nil
			}
			return w32RealReads(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w32 unit")
		require.Error(t, err)
		require.Contains(t, err.Error(), "no longer names the verified regular file at the unlink")
		require.Equal(t, "old", string(readW31(t, base, backup)))
		require.Empty(t, w32RollbackQuarNames(t, base, "/w32du"))
	})

	t.Run("unlink-time metadata change refuses typed and restores", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		require.NoError(t, afero.WriteFile(base, "/w32du/foreign", []byte("a much longer foreign payload"), 0o644))
		foreignInfo, ferr := base.Stat("/w32du/foreign")
		require.NoError(t, ferr)
		fs := &w32RollbackQuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return foreignInfo, nil
			}
			return w32RealReads(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w32 unit")
		require.Error(t, err)
		require.Contains(t, err.Error(), "metadata changed between the re-verify and the unlink")
		require.Equal(t, "old", string(readW31(t, base, backup)))
		require.Empty(t, w32RollbackQuarNames(t, base, "/w32du"))
	})

	t.Run("unlink-time dev/inode mismatch refuses typed and restores", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("inode identity assertions are POSIX-shaped")
		}
		base := afero.NewOsFs()
		dir := t.TempDir()
		backupOS := filepath.Join(dir, "poster.jpg.dlbak.abcd")
		require.NoError(t, os.WriteFile(backupOS, []byte("old"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "foreign"), []byte("foreign-even-if-same-size"), 0o644))
		foreignInfo, ferr := os.Lstat(filepath.Join(dir, "foreign"))
		require.NoError(t, ferr)
		fs := &w32RollbackQuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return foreignInfo, nil
			}
			return w32RealReads(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backupOS, nil, "w32 unit")
		require.Error(t, err)
		require.Contains(t, err.Error(), "dev/inode mismatch")
		require.Equal(t, "old", string(readW31(t, base, backupOS)))
		require.Empty(t, w32RollbackQuarNames(t, base, dir))
	})

	t.Run("wedged quarantine unlink compensates the move-back", func(t *testing.T) {
		sentinel := errors.New("rollback quarantine unlink wedged")
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32RollbackQuarFs{Fs: base}
		fs.removeFn = func(string) error { return sentinel }

		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w32 unit")
		require.ErrorIs(t, err, sentinel)
		require.NotErrorIs(t, err, errRollbackQuarantineVanished)
		require.Equal(t, "old", string(readW31(t, base, backup)))
		require.Empty(t, w32RollbackQuarNames(t, base, "/w32du"))
	})

	t.Run("unlink answering ENOENT after a real removal is typed retention", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32RollbackQuarFs{Fs: base}
		fs.removeFn = func(name string) error {
			_ = base.Remove(name)
			return os.ErrNotExist
		}

		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w32 unit")
		require.ErrorIs(t, err, errRollbackQuarantineVanished,
			"the bytes vanished unownably at the unlink — never consumed")
		require.Empty(t, w32RollbackQuarNames(t, base, "/w32du"))
	})
}

// Hold lifecycle + claim + move + re-verify arms on the downloader port.
func TestRollbackBackupQuarantineW32_LifecycleClaimMoveReverify(t *testing.T) {
	const backup = "/w32dl/poster.jpg.dlbak.abcd"

	t.Run("restore is idempotent and an unlinked hold never acts", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		hold, err := quarantineRollbackBackupForRemoval(base, backup, nil, "w32 unit")
		require.NoError(t, err)
		require.NoError(t, hold.removeVerified())
		exists, _ := afero.Exists(base, backup)
		require.False(t, exists)
		hold.restore()
		require.False(t, hold.moved)
	})

	t.Run("absent-at-gate hold is an inert success", func(t *testing.T) {
		base := afero.NewMemMapFs()
		hold, err := quarantineRollbackBackupForRemoval(base, "/w32dl/never", nil, "w32 unit")
		require.NoError(t, err)
		require.NoError(t, hold.removeVerified())
		hold.restore()
	})

	t.Run("open vanished under the gate is an inert success", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32RollbackQuarFs{Fs: base}
		fs.openFn = func(name string) (afero.File, bool, error) {
			if name == backup {
				return nil, true, os.ErrNotExist
			}
			return nil, false, nil
		}
		hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
		require.NoError(t, err)
		require.NoError(t, hold.removeVerified())
		hold.restore()
	})

	t.Run("entropy draw failure retains", func(t *testing.T) {
		sentinel := errors.New("entropy wedged")
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		prev := rollbackQuarantineRandReader
		rollbackQuarantineRandReader = w32ErrReader{err: sentinel}
		t.Cleanup(func() { rollbackQuarantineRandReader = prev })

		_, err := quarantineRollbackBackupForRemoval(base, backup, nil, "w32 unit")
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(readW31(t, base, backup)))
	})

	t.Run("occupied first draw climbs to the next candidate", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32QuarClaimFs{Fs: base, lstatOccupiedAnswers: 1}
		hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
		require.NoError(t, err)
		require.NoError(t, hold.removeVerified())
		require.Empty(t, w32RollbackQuarNames(t, base, "/w32dl"))
	})

	t.Run("candidate inspection failure retains", func(t *testing.T) {
		sentinel := errors.New("rollback quarantine lstat wedged")
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32QuarClaimFs{Fs: base, lstatErr: sentinel}

		_, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "inspect quarantine candidate")
		require.Equal(t, "old", string(readW31(t, base, backup)))
	})

	t.Run("racing reservation on the first draw climbs", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32QuarClaimFs{Fs: base, openExistAnswers: 1}
		hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
		require.NoError(t, err)
		require.NoError(t, hold.removeVerified())
	})

	t.Run("reservation failure retains", func(t *testing.T) {
		sentinel := errors.New("rollback quarantine reserve wedged")
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32QuarClaimFs{Fs: base, openErr: sentinel}

		_, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "reserve quarantine candidate")
		require.Equal(t, "old", string(readW31(t, base, backup)))
	})

	t.Run("reservation close failure drops the placeholder", func(t *testing.T) {
		sentinel := errors.New("reservation close wedged")
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32QuarClaimFs{Fs: base, closeErr: sentinel}

		_, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "close quarantine reservation")
		require.Empty(t, w32RollbackQuarNames(t, base, "/w32dl"))
	})

	t.Run("quarantine name exhaustion refuses", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32QuarClaimFs{Fs: base, lstatOccupiedAnswers: rollbackQuarantineClaimTries}

		_, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
		require.Error(t, err)
		require.Contains(t, err.Error(), "quarantine names exhausted")
		require.Equal(t, "old", string(readW31(t, base, backup)))
	})

	t.Run("move failure keeps the journaled name untouched", func(t *testing.T) {
		sentinel := errors.New("rollback quarantine move wedged")
		base := afero.NewMemMapFs()
		w32RollbackBackup(t, base, backup, "old")
		fs := &w32QuarMoveFailFs{Fs: base, err: sentinel}

		_, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(readW31(t, base, backup)))
		require.Empty(t, w32RollbackQuarNames(t, base, "/w32dl"), "the reservation placeholder was cleaned")
	})

	t.Run("post-move re-verify arms", func(t *testing.T) {
		type arm struct {
			name    string
			script  func(fs *w32RollbackQuarFs, call int, name string) (os.FileInfo, error)
			wantErr string
			isSent  bool
		}
		sentinel := errors.New("post-move reverify wedged")
		arms := []arm{
			{
				name: "vanished at the post-move re-verify is typed retention",
				script: func(_ *w32RollbackQuarFs, _ int, _ string) (os.FileInfo, error) {
					return nil, afero.ErrFileNotFound
				},
				isSent: true,
			},
			{
				name: "indeterminate post-move re-verify restores",
				script: func(_ *w32RollbackQuarFs, _ int, _ string) (os.FileInfo, error) {
					return nil, sentinel
				},
				wantErr: "wedged",
			},
			{
				name: "nil post-move answer refuses typed and restores",
				script: func(_ *w32RollbackQuarFs, _ int, _ string) (os.FileInfo, error) {
					return nil, nil
				},
				wantErr: "not the verified regular file",
			},
			{
				name: "metadata post-move mismatch refuses typed and restores",
				script: func(fs *w32RollbackQuarFs, _ int, _ string) (os.FileInfo, error) {
					info, err := fs.Fs.Stat("/w32dl/foreign")
					return info, err
				},
				wantErr: "metadata differs",
			},
		}
		for _, a := range arms {
			t.Run(a.name, func(t *testing.T) {
				base := afero.NewMemMapFs()
				w32RollbackBackup(t, base, backup, "old")
				require.NoError(t, afero.WriteFile(base, "/w32dl/foreign", []byte("a much longer foreign payload"), 0o644))
				fs := &w32RollbackQuarFs{Fs: base}
				fs.lstat = func(call int, name string) (os.FileInfo, error) {
					return a.script(fs, call, name)
				}

				_, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w32 unit")
				require.Error(t, err)
				switch {
				case a.isSent:
					require.ErrorIs(t, err, errRollbackQuarantineVanished)
					require.Len(t, w32RollbackQuarNames(t, base, "/w32dl"), 1, "the wedge replayed the vanish only")
				case a.wantErr == "wedged":
					require.ErrorIs(t, err, sentinel)
					require.Equal(t, "old", string(readW31(t, base, backup)))
					require.Empty(t, w32RollbackQuarNames(t, base, "/w32dl"))
				default:
					require.Contains(t, err.Error(), a.wantErr)
					require.Equal(t, "old", string(readW31(t, base, backup)))
					require.Empty(t, w32RollbackQuarNames(t, base, "/w32dl"))
				}
			})
		}
	})
}

// quarantineAndRemoveVerifiedRollbackBackup composes the wave-32
// successors the way the removed one-shot removeRollbackBackup did for
// callers WITHOUT a destination re-gate: bind the occupant + verified
// quarantine move, then the quarantine unlink. Unit legs whose scenario
// never touches a destination exercise the successor chain through it.
// (installOverwriting's confirm-failure rollback holds the split explicitly
// in production — the post-quarantine destination re-gate sits between the
// two halves.)
func quarantineAndRemoveVerifiedRollbackBackup(fs afero.Fs, backup string, copiedFrom os.FileInfo, phase string) error {
	hold, err := quarantineRollbackBackupForRemoval(fs, backup, copiedFrom, phase)
	if err != nil {
		return err
	}
	return hold.removeVerified()
}

type w32ErrReader struct{ err error }

func (r w32ErrReader) Read([]byte) (int, error) { return 0, r.err }

// w32QuarClaimFs wedges the claim-time Lstat/open surfaces for quarantine
// candidate names.
type w32QuarClaimFs struct {
	afero.Fs
	lstatOccupiedAnswers int
	lstatErr             error
	openExistAnswers     int
	openErr              error
	closeErr             error
}

func (f *w32QuarClaimFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if strings.Contains(name, rollbackQuarantineSuffix) {
		if f.lstatErr != nil {
			return nil, false, f.lstatErr
		}
		if f.lstatOccupiedAnswers > 0 {
			f.lstatOccupiedAnswers--
			info, err := f.Fs.Stat("/")
			return info, false, err
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

type w32CloseFailFile struct {
	afero.File
	err error
}

func (f w32CloseFailFile) Close() error { return f.err }

func (f *w32QuarClaimFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, rollbackQuarantineSuffix) {
		if f.openErr != nil {
			return nil, f.openErr
		}
		if f.openExistAnswers > 0 {
			f.openExistAnswers--
			return nil, os.ErrExist
		}
		file, err := f.Fs.OpenFile(name, flag, perm)
		if err == nil && f.closeErr != nil {
			return w32CloseFailFile{File: file, err: f.closeErr}, nil
		}
		return file, err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// w32QuarMoveFailFs wedges the quarantining rename itself.
type w32QuarMoveFailFs struct {
	afero.Fs
	err error
}

func (f *w32QuarMoveFailFs) Rename(oldname, newname string) error {
	if strings.Contains(newname, rollbackQuarantineSuffix) {
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

// Compensation failure posture: with a racer occupying the journaled name,
// the move-back is refused and the verified object stays recoverable at the
// quarantine name.
func TestRollbackBackupQuarantineW32_CompensationCollisionKeepsDebris(t *testing.T) {
	const backup = "/w32dc/poster.jpg.dlbak.abcd"
	sentinel := errors.New("rollback quarantine unlink wedged")
	base := afero.NewMemMapFs()
	w32RollbackBackup(t, base, backup, "old")
	// A racer claims the journaled name inside the wedge window, replayed
	// through the Remove wedge closure itself.
	var planted bool
	fs := &w32RollbackQuarFs{Fs: base}
	fs.removeFn = func(name string) error {
		if !planted {
			planted = true
			require.NoError(t, afero.WriteFile(base, backup, []byte("racer occupant"), 0o644))
		}
		return sentinel
	}

	err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w32 unit")
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, "racer occupant", string(readW31(t, base, backup)),
		"the NO-REPLACE compensation never clobbers the racer")
	require.Len(t, w32RollbackQuarNames(t, base, "/w32dc"), 1,
		"the verified object stays recoverable at the quarantine name")
}

// R1 — the confirm-failure rollback pipeline: the destination diverges AFTER
// the backup was quarantined ⇒ the verified object moves back onto the
// journaled name, the destination is untouched, and the entry stays ARMED.
func TestInstallOverwritingW32_ConfirmRollbackPostQuarantineDivergenceRestoresBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, fs, old)

	calls := 0
	w31StubRestoredDestRecheck(t, func(fsys afero.Fs, target string, id installedDestIdentity) bool {
		calls++
		return calls == 1 // the pre-removal recheck passes; the post-quarantine re-gate fails
	})

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: errors.New("w32 confirmation failed"),
	}

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w32-post-quarantine-divergence", recorder: recorder,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "diverged after the backup was quarantined")
	require.False(t, skipped)
	require.True(t, replaced)
	require.Equal(t, 2, calls)

	records := recorder.get()
	require.Len(t, records, 1, "the journal entry is still armed")
	require.Equal(t, old, readW31(t, fs, records[0].backupPath),
		"the verified object moved back onto the journaled name")
	require.Equal(t, old, readW31(t, fs, dest), "the restored destination is untouched")
	require.Zero(t, recorder.releaseCalls)
	require.Empty(t, recorder.getPendings(), "the armed entry arbitates recovery, not a pending marker")
	requireNoDownloaderBackupW32Quar(t, fs, filepath.Dir(dest))
}

// R4 — the confirm-failure rollback's quarantine unlink answering ENOENT: the
// bytes vanished unownably, so NOTHING is consumed: the entry stays armed
// (never released), the destination keeps the restored pre-existing bytes.
func TestInstallOverwritingW32_ConfirmRollbackVanishedUnlinkKeepsArmed(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w32QuarVanishingFs{Fs: base}
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, fs, old)

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: errors.New("w32 confirmation failed"),
	}

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w32-vanished-unlink", recorder: recorder,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "backup cleanup failed")

	records := recorder.get()
	require.Len(t, records, 1, "the journal entry was NOT released — it stays armed")
	require.Zero(t, recorder.releaseCalls)
	require.Equal(t, old, readW31(t, fs, dest), "the rollback restore stands")
	_, statErr := fs.Stat(records[0].backupPath)
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"the verified object was moved aside and vanished (replayed)")
	require.Empty(t, w32RollbackQuarNames(t, base, filepath.Dir(dest)))
}

// w32QuarVanishingFs replays the unlink-time vanish on every ".dlq." removal:
// the object is REALLY removed and the Remove answer is ENOENT.
type w32QuarVanishingFs struct {
	afero.Fs
}

func (f *w32QuarVanishingFs) Remove(name string) error {
	if strings.Contains(name, rollbackQuarantineSuffix) {
		_ = f.Fs.Remove(name)
		return os.ErrNotExist
	}
	return f.Fs.Remove(name)
}

func requireNoDownloaderBackupW32Quar(t *testing.T, fs afero.Fs, dir string) {
	t.Helper()
	require.Empty(t, w32RollbackQuarNames(t, fs, dir), "no quarantine debris remains")
}
