package history

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-10 (codex follow-up, P2): rearmReplacementBackup
// — the journal-consumption compensation that recreates a removed backup from
// the destination's bytes — used to copy dest→backup through a plain fs.Open.
// An attacker swapping the destination for a SYMLINK in the removal→re-arm
// window got a protected file (the link target) copied into the media-dir
// backup, armed for a later restore: a privilege-escalation primitive. The
// destination open now runs through openRearmSource — the same Lstat-first,
// regular-file-only, no-follow-open, identity-verified discipline the restore
// paths apply to backups (copyRestoreBytes) — and the copy streams from the
// verified handle.

// w10RearmSeam swaps the restoreOpenReplacementSource seam for one test.
func w10RearmSeam(t *testing.T, fn func(fsys afero.Fs, path string) (afero.File, error)) {
	t.Helper()
	prev := restoreOpenReplacementSource
	restoreOpenReplacementSource = fn
	t.Cleanup(func() { restoreOpenReplacementSource = prev })
}

// w10RearmFixture builds a dest file ready for re-arm through fs.
func w10RearmFixture(t *testing.T, fs afero.Fs, dest string) os.FileInfo {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current-bytes"), 0o640))
	info, err := lstatRestoreSource(fs, dest)
	require.NoError(t, err)
	require.NotNil(t, info)
	return info
}

// TestW10RearmReplacementBackup_SymlinkDestinationRefusedOnOsFs is the
// finding's regression (a): dest replaced by a symlink between the backup
// removal and the re-arm → the re-arm refuses, the backup is NOT recreated
// with the target's bytes, and a pre-existing backup is never overwritten
// with them either.
func TestW10RearmReplacementBackup_SymlinkDestinationRefusedOnOsFs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}

	root := t.TempDir()
	fs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak.0123456789abcdef"
	victim := filepath.Join(root, "victim.bin")
	require.NoError(t, os.WriteFile(victim, []byte("protected-bytes"), 0o600))
	require.NoError(t, os.Symlink(victim, dest))

	t.Run("absent backup is not created from target bytes", func(t *testing.T) {
		err := rearmReplacementBackup(fs, dest, backup, nil)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrRestoreSourceRefused, "symlink destination refused like a hostile backup")
		_, statErr := os.Lstat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist, "no backup materialized from the protected target")
		victimBytes, readErr := os.ReadFile(victim)
		require.NoError(t, readErr)
		require.Equal(t, "protected-bytes", string(victimBytes), "target untouched")
	})

	t.Run("pre-existing backup is not overwritten with target bytes", func(t *testing.T) {
		require.NoError(t, os.WriteFile(backup, []byte("prior-bytes"), 0o600))
		err := rearmReplacementBackup(fs, dest, backup, nil)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrRestoreSourceRefused)
		got, readErr := os.ReadFile(backup)
		require.NoError(t, readErr)
		require.Equal(t, "prior-bytes", string(got), "the armed backup kept its original bytes")
	})
}

// TestW10RearmReplacementBackup_RegularFileUnchanged pins (b): the ordinary
// regular-file re-arm is byte-for-byte and metadata identical to the wave-9
// behavior, and the bytes provably stream from the seam-opened handle.
func TestW10RearmReplacementBackup_RegularFileUnchanged(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W10-REARM/dest.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	modTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	info := w10RearmFixture(t, fs, dest)
	require.NoError(t, fs.Chtimes(dest, modTime, modTime))

	var openedPath string
	var openedFs afero.Fs
	w10RearmSeam(t, func(fsys afero.Fs, path string) (afero.File, error) {
		openedFs, openedPath = fsys, path
		return openRestoreSourceNoFollow(fsys, path)
	})

	require.NoError(t, rearmReplacementBackup(fs, dest, backup, info))
	require.Equal(t, dest, openedPath, "the destination opens through the installed no-follow seam")
	require.Same(t, fs, openedFs, "the caller's filesystem threads to the seam")

	got, err := afero.ReadFile(fs, backup)
	require.NoError(t, err)
	require.Equal(t, "current-bytes", string(got), "backup recreated with the destination's bytes")
	rearmed, err := fs.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), rearmed.Mode().Perm(), "captured permission bits restored")
	require.True(t, rearmed.ModTime().Equal(modTime), "captured mtime restored")

	// info == nil keeps wave-9's copy-only behavior.
	backup2 := backup + ".1"
	require.NoError(t, rearmReplacementBackup(fs, dest, backup2, nil))
	got2, err := afero.ReadFile(fs, backup2)
	require.NoError(t, err)
	require.Equal(t, "current-bytes", string(got2))

	// dest == backup is a no-op (CopyFileFs parity), even for missing paths.
	require.NoError(t, rearmReplacementBackup(fs, "/out/W10-REARM/same", "/out/W10-REARM/same", info))
	_, statErr := fs.Stat("/out/W10-REARM/same")
	require.ErrorIs(t, statErr, os.ErrNotExist, "the no-op must not create anything")
}

// w10RearmSrcFile wraps a real afero.File with controllable Stat/Read legs.
type w10RearmSrcFile struct {
	afero.File
	statInfo os.FileInfo
	statErr  error
	readErr  error
	closed   *bool
}

func (f *w10RearmSrcFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	if f.statInfo != nil {
		return f.statInfo, nil
	}
	return f.File.Stat()
}

func (f *w10RearmSrcFile) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.File.Read(p)
}

func (f *w10RearmSrcFile) Close() error {
	if f.closed != nil {
		*f.closed = true
	}
	return f.File.Close()
}

// TestW10OpenRearmSource_Legs drives (c): every refusal/error leg of the
// Lstat → regularity → seam-open → fstat → identity gate, each asserting the
// backup is never materialized.
func TestW10OpenRearmSource_Legs(t *testing.T) {
	newBase := func(t *testing.T) (afero.Fs, string, string) {
		t.Helper()
		base := afero.NewMemMapFs()
		dest := "/out/W10-LEGS/dest.jpg"
		backup := dest + ".dlbak.0123456789abcdef"
		require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(base, dest, []byte("current"), config.FilePerm))
		return base, dest, backup
	}

	t.Run("missing destination", func(t *testing.T) {
		base, _, backup := newBase(t)
		err := rearmReplacementBackup(base, "/out/W10-LEGS/never-existed.jpg", backup, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "re-arm source")
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("nil lstat info", func(t *testing.T) {
		base, dest, backup := newBase(t)
		err := rearmReplacementBackup(&restoreSourceNilLstatW22Fs{Fs: base}, dest, backup, nil)
		require.ErrorIs(t, err, ErrRestoreSourceRefused)
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("directory destination refused", func(t *testing.T) {
		base, _, backup := newBase(t)
		dir := "/out/W10-LEGS/dir-dest"
		require.NoError(t, base.Mkdir(dir, config.DirPerm))
		err := rearmReplacementBackup(base, dir, backup, nil)
		require.ErrorIs(t, err, ErrRestoreSourceRefused)
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("seam open error propagates", func(t *testing.T) {
		base, dest, backup := newBase(t)
		sentinel := errors.New("no-follow open wedged")
		w10RearmSeam(t, func(afero.Fs, string) (afero.File, error) { return nil, sentinel })
		err := rearmReplacementBackup(base, dest, backup, nil)
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "re-arm source")
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("opened-object stat error propagates and closes", func(t *testing.T) {
		base, dest, backup := newBase(t)
		sentinel := errors.New("fstat wedged")
		closed := false
		w10RearmSeam(t, func(fsys afero.Fs, path string) (afero.File, error) {
			f, oerr := fsys.Open(path)
			if oerr != nil {
				return nil, oerr
			}
			return &w10RearmSrcFile{File: f, statErr: sentinel, closed: &closed}, nil
		})
		err := rearmReplacementBackup(base, dest, backup, nil)
		require.ErrorIs(t, err, sentinel)
		require.True(t, closed, "the opened handle is closed on the stat-error leg")
	})

	t.Run("opened object non-regular refused and closed", func(t *testing.T) {
		base, dest, backup := newBase(t)
		closed := false
		dirInfo, err := base.Stat("/out/W10-LEGS")
		require.NoError(t, err)
		w10RearmSeam(t, func(fsys afero.Fs, path string) (afero.File, error) {
			f, oerr := fsys.Open(path)
			if oerr != nil {
				return nil, oerr
			}
			return &w10RearmSrcFile{File: f, statInfo: dirInfo, closed: &closed}, nil
		})
		err = rearmReplacementBackup(base, dest, backup, nil)
		require.ErrorIs(t, err, ErrRestoreSourceRefused)
		require.True(t, closed, "the opened handle is closed on the refusal leg")
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})
}

// TestW10OpenRearmSource_IdentityMismatchRefused drives the dev/ino identity
// leg on a filesystem that exposes it: the Lstat gate sees the destination,
// the seam opens a DIFFERENT regular file — the opened object no longer
// matches, so the re-arm refuses instead of arming foreign bytes.
func TestW10OpenRearmSource_IdentityMismatchRefused(t *testing.T) {
	root := t.TempDir()
	base := afero.NewOsFs()
	dest := filepath.Join(root, "dest.jpg")
	other := filepath.Join(root, "other.jpg")
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o640))
	require.NoError(t, os.WriteFile(other, []byte("foreign"), 0o640))

	destInfo, err := lstatRestoreSource(base, dest)
	require.NoError(t, err)
	if _, _, ok := restoreSourceIdentity(destInfo); !ok {
		t.Skip("filesystem does not expose a stable Dev/Ino identity")
	}

	w10RearmSeam(t, func(fsys afero.Fs, _ string) (afero.File, error) {
		return fsys.Open(other) // swaps the opened object behind the path
	})
	err = rearmReplacementBackup(base, dest, backup, nil)
	require.ErrorIs(t, err, ErrRestoreSourceRefused)
	_, statErr := os.Lstat(backup)
	require.ErrorIs(t, statErr, os.ErrNotExist, "foreign bytes never materialize as a backup")
}

// w10 copy-write legs: deterministic fs stubs for copyRearmSourceBytes.
type w10MkdirAllFailFs struct{ afero.Fs }

func (f *w10MkdirAllFailFs) MkdirAll(string, os.FileMode) error { return errors.New("mkdir wedged") }

type w10RenameFailFs struct{ afero.Fs }

func (f *w10RenameFailFs) Rename(string, string) error { return errors.New("rename wedged") }

type w10CloseFailFs struct{ afero.Fs }

func (f *w10CloseFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	// Wave-21: the re-arm stages through the exclusive `<backup>.dlrarm.<hex>`
	// name (CreateExclusiveStagingFile), so the wedge keys on its marker.
	if flag&os.O_CREATE != 0 && strings.Contains(name, rearmStagingSuffix+".") {
		return &w10CloseFailFile{File: file}, nil
	}
	return file, nil
}

type w10CloseFailFile struct{ afero.File }

func (w10CloseFailFile) Close() error { return errors.New("close wedged") }

// TestW10RearmCopyWriteLegs pins the temp-file write side: each failure
// returns the wrapped error and leaves the backup path untouched (the temp
// file is cleaned up on every leg).
func TestW10RearmCopyWriteLegs(t *testing.T) {
	newFixture := func(t *testing.T) (afero.Fs, string, string) {
		t.Helper()
		base := afero.NewMemMapFs()
		dest := "/out/W10-COPY/dest.jpg"
		backup := dest + ".dlbak.0123456789abcdef"
		require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(base, dest, []byte("current"), config.FilePerm))
		return base, dest, backup
	}
	assertNoBackup := func(t *testing.T, base afero.Fs, dest string) {
		t.Helper()
		entries, err := afero.ReadDir(base, filepath.Dir(dest))
		require.NoError(t, err)
		for _, e := range entries {
			require.Equal(t, filepath.Base(dest), e.Name(), "only the destination file may remain: %s", e.Name())
		}
	}

	t.Run("mkdir failure", func(t *testing.T) {
		base, dest, backup := newFixture(t)
		err := rearmReplacementBackup(&w10MkdirAllFailFs{Fs: base}, dest, backup, nil)
		require.ErrorContains(t, err, "mkdir wedged")
		assertNoBackup(t, base, dest)
	})

	t.Run("create backup failure (read-only fs)", func(t *testing.T) {
		base, dest, backup := newFixture(t)
		err := rearmReplacementBackup(afero.NewReadOnlyFs(base), dest, backup, nil)
		require.Error(t, err)
		assertNoBackup(t, base, dest)
	})

	t.Run("copy failure cleans the temp file", func(t *testing.T) {
		base, dest, backup := newFixture(t)
		sentinel := errors.New("read wedged")
		w10RearmSeam(t, func(fsys afero.Fs, path string) (afero.File, error) {
			f, oerr := fsys.Open(path)
			if oerr != nil {
				return nil, oerr
			}
			return &w10RearmSrcFile{File: f, readErr: sentinel}, nil
		})
		err := rearmReplacementBackup(base, dest, backup, nil)
		require.ErrorIs(t, err, sentinel)
		assertNoBackup(t, base, dest)
	})

	t.Run("close failure cleans the temp file", func(t *testing.T) {
		base, dest, backup := newFixture(t)
		err := rearmReplacementBackup(&w10CloseFailFs{Fs: base}, dest, backup, nil)
		require.ErrorContains(t, err, "close wedged")
		assertNoBackup(t, base, dest)
	})

	t.Run("rename failure cleans the temp file", func(t *testing.T) {
		base, dest, backup := newFixture(t)
		err := rearmReplacementBackup(&w10RenameFailFs{Fs: base}, dest, backup, nil)
		require.ErrorContains(t, err, "rename wedged")
		assertNoBackup(t, base, dest)
	})
}
