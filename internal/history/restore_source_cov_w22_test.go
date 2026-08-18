package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestRestoreSourceW22_ExplicitSymlinkRefusedAndJournalRetained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}

	root := t.TempDir()
	fs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	victim := filepath.Join(root, "victim.bin")
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))
	require.NoError(t, os.WriteFile(victim, []byte("protected"), 0o644))
	require.NoError(t, os.Symlink(victim, backup))

	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-w22-explicit", "W22-EXPLICIT", dest, backup, 1, models.RevertStatusApplied)
	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	var refused *RestoreSourceRefusedError
	require.ErrorAs(t, err, &refused)
	require.ErrorIs(t, err, ErrRestoreSourceRefused)
	require.Empty(t, restored)

	got, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	require.Equal(t, "current", string(got))
	info, statErr := os.Lstat(backup)
	require.NoError(t, statErr)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
	victimBytes, readErr := os.ReadFile(victim)
	require.NoError(t, readErr)
	require.Equal(t, "protected", string(victimBytes))

	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.Replacements, 1, "refused restore leaves the entry armed")
}

func TestRestoreSourceW22_SweepSymlinkRefusedAndJournalRetained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}

	root := t.TempDir()
	fs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexB
	victim := filepath.Join(root, "victim.bin")
	require.NoError(t, os.WriteFile(victim, []byte("protected"), 0o644))
	require.NoError(t, os.Symlink(victim, backup))

	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-w22-sweep", "W22-SWEEP", dest, backup, 1, models.RevertStatusApplied)
	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Zero(t, healed)

	_, statErr := os.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	victimBytes, readErr := os.ReadFile(victim)
	require.NoError(t, readErr)
	require.Equal(t, "protected", string(victimBytes))
	info, statErr := os.Lstat(backup)
	require.NoError(t, statErr)
	require.NotZero(t, info.Mode()&os.ModeSymlink)

	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.Replacements, 1, "sweep refusal leaves the entry armed")
}

func TestRestoreSourceW22_DirectoryBackupRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W22-DIR/poster.jpg"
	backup := dest + ".dlbak." + p3HexC
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), config.FilePerm))
	require.NoError(t, fs.Mkdir(backup, config.DirPerm))

	err := copyRestoreBytes(fs, backup, dest)
	require.ErrorIs(t, err, ErrRestoreSourceRefused)
	require.Equal(t, "current", string(mustRead2(t, fs, dest)))
	info, statErr := fs.Stat(backup)
	require.NoError(t, statErr)
	require.True(t, info.IsDir())
}

type restoreSourceFlagsW22Fs struct {
	afero.Fs
	backup string
	flags  int
}

func (f *restoreSourceFlagsW22Fs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == f.backup {
		f.flags = flag
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func TestRestoreSourceW22_OsFsNoFollowFlagAndIdentityCheck(t *testing.T) {
	if restoreSourceNoFollow == 0 {
		t.Skip("target has no portable O_NOFOLLOW flag")
	}

	root := t.TempDir()
	backup := filepath.Join(root, "backup.bin")
	dest := filepath.Join(root, "dest.bin")
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))
	fs := &restoreSourceFlagsW22Fs{Fs: afero.NewOsFs(), backup: backup}
	require.NoError(t, copyRestoreBytes(fs, backup, dest))
	require.NotZero(t, fs.flags&restoreSourceNoFollow)
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
}

type restoreSourceSwapW22Fs struct {
	afero.Fs
	backup      string
	replacement string
	swapped     bool
}

func (f *restoreSourceSwapW22Fs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == f.backup && !f.swapped {
		f.swapped = true
		if err := f.Fs.Rename(f.backup, f.backup+".old"); err != nil {
			return nil, err
		}
		if err := f.Fs.Rename(f.replacement, f.backup); err != nil {
			return nil, err
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func TestRestoreSourceW22_IdentitySwapRefused(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, "backup.bin")
	replacement := filepath.Join(root, "replacement.bin")
	dest := filepath.Join(root, "dest.bin")
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(replacement, []byte("other"), 0o640))
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))

	// The swap refusal relies on a stable Dev/Ino identity. Windows and
	// identity-less afero filesystems still enforce their regular-file/no-follow
	// contract, but must not be made to pretend they have POSIX inode values.
	base := afero.NewOsFs()
	sourceInfo, sourceStatErr := lstatRestoreSource(base, backup)
	require.NoError(t, sourceStatErr)
	if _, _, ok := restoreSourceIdentity(sourceInfo); !ok {
		t.Skip("filesystem does not expose a stable Dev/Ino identity")
	}

	fs := &restoreSourceSwapW22Fs{Fs: base, backup: backup, replacement: replacement}

	err := copyRestoreBytes(fs, backup, dest)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRestoreSourceRefused)
	require.Equal(t, "current", string(mustRead2(t, fs, dest)))
	require.Equal(t, "other", string(mustRead2(t, fs, backup)))
	_, statErr := fs.Stat(dest + ".rstr.1")
	require.True(t, errors.Is(statErr, os.ErrNotExist))
}

type restoreSourceOpenW22Fs struct {
	afero.Fs
	backup   string
	openErr  error
	statErr  error
	statInfo os.FileInfo
}

func (f *restoreSourceOpenW22Fs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name != f.backup {
		return f.Fs.OpenFile(name, flag, perm)
	}
	if f.openErr != nil {
		return nil, f.openErr
	}
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return file, err
	}
	return &restoreSourceStatW22File{File: file, err: f.statErr, info: f.statInfo}, nil
}

type restoreSourceStatW22File struct {
	afero.File
	err  error
	info os.FileInfo
}

func (f *restoreSourceStatW22File) Stat() (os.FileInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.info != nil {
		return f.info, nil
	}
	return f.File.Stat()
}

type restoreSourceNilLstatW22Fs struct{ afero.Fs }

func (f *restoreSourceNilLstatW22Fs) LstatIfPossible(string) (os.FileInfo, bool, error) {
	return nil, true, nil
}

func TestRestoreSourceW22_ValidationFailureLegs(t *testing.T) {
	newFixture := func(t *testing.T) (afero.Fs, string, string) {
		t.Helper()
		base := afero.NewMemMapFs()
		dest := "/out/W22-VALIDATE/dest.bin"
		backup := "/out/W22-VALIDATE/backup.bin"
		require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(base, dest, []byte("current"), config.FilePerm))
		require.NoError(t, afero.WriteFile(base, backup, []byte("old"), config.FilePerm))
		return base, backup, dest
	}

	t.Run("nil lstat info", func(t *testing.T) {
		base, backup, dest := newFixture(t)
		err := copyRestoreBytes(&restoreSourceNilLstatW22Fs{Fs: base}, backup, dest)
		require.ErrorIs(t, err, ErrRestoreSourceRefused)
	})

	t.Run("open error", func(t *testing.T) {
		base, backup, dest := newFixture(t)
		sentinel := errors.New("restore source open wedged")
		fs := &restoreSourceOpenW22Fs{Fs: base, backup: backup, openErr: sentinel}
		err := copyRestoreBytes(fs, backup, dest)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("opened stat error", func(t *testing.T) {
		base, backup, dest := newFixture(t)
		sentinel := errors.New("restore source fstat wedged")
		fs := &restoreSourceOpenW22Fs{Fs: base, backup: backup, statErr: sentinel}
		err := copyRestoreBytes(fs, backup, dest)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("opened object non-regular", func(t *testing.T) {
		base, backup, dest := newFixture(t)
		dir := "/out/W22-VALIDATE/opened-dir"
		require.NoError(t, base.Mkdir(dir, config.DirPerm))
		info, statErr := base.Stat(dir)
		require.NoError(t, statErr)
		fs := &restoreSourceOpenW22Fs{Fs: base, backup: backup, statInfo: info}
		err := copyRestoreBytes(fs, backup, dest)
		require.ErrorIs(t, err, ErrRestoreSourceRefused)
	})
}
