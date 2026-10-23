//go:build darwin

package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func darwinKernelError(t *testing.T, err error) {
	t.Helper()
	prev := renameNoReplaceKernel
	renameNoReplaceKernel = func(string, string) error { return err }
	t.Cleanup(func() { renameNoReplaceKernel = prev })
}

func TestDarwinExclusiveRenamePreservesObject(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "dummy")
	dst := filepath.Join(dir, "moved")
	require.NoError(t, os.WriteFile(src, []byte("dummy source claim bytes"), 0600))
	before, err := os.Stat(src)
	require.NoError(t, err)
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(string, string) error { t.Fatal("native rename used hard links"); return syscall.EIO }
	t.Cleanup(func() { publishNoReplaceLink = prev })
	proof, _ := verifyProofOf(t, afero.NewOsFs(), src)
	require.NoError(t, MoveFileNoReplaceVerified(afero.NewOsFs(), src, dst, proof))
	after, err := os.Stat(dst)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	require.Equal(t, before.Mode(), after.Mode())
	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "dummy source claim bytes", string(data))
	_, err = os.Lstat(src)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDarwinExclusiveRenameOccupied(t *testing.T) {
	for _, kind := range []string{"file", "directory", "dangling", "late"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			dst := filepath.Join(dir, "dst")
			require.NoError(t, os.WriteFile(src, []byte("source"), 0600))
			plant := func() {
				switch kind {
				case "directory":
					require.NoError(t, os.Mkdir(dst, 0700))
				case "dangling":
					require.NoError(t, os.Symlink("absent", dst))
				default:
					require.NoError(t, os.WriteFile(dst, []byte("foreign"), 0600))
				}
			}
			if kind == "late" {
				prev := renameNoReplaceKernel
				renameNoReplaceKernel = func(s, d string) error { plant(); return prev(s, d) }
				t.Cleanup(func() { renameNoReplaceKernel = prev })
			} else {
				plant()
			}
			require.ErrorIs(t, PublishNoReplace(afero.NewOsFs(), src, dst), ErrPublishCollision)
			data, err := os.ReadFile(src)
			require.NoError(t, err)
			require.Equal(t, "source", string(data))
			switch kind {
			case "directory":
				info, err := os.Lstat(dst)
				require.NoError(t, err)
				require.True(t, info.IsDir())
			case "dangling":
				target, err := os.Readlink(dst)
				require.NoError(t, err)
				require.Equal(t, "absent", target)
			default:
				data, err := os.ReadFile(dst)
				require.NoError(t, err)
				require.Equal(t, "foreign", string(data))
			}
		})
	}
}

func TestDarwinExclusiveRenameErrors(t *testing.T) {
	for _, errno := range []error{syscall.EPERM, syscall.EACCES, syscall.EIO, syscall.EXDEV} {
		t.Run(errno.Error(), func(t *testing.T) {
			darwinKernelError(t, errno)
			prev := publishNoReplaceLink
			publishNoReplaceLink = func(string, string) error { t.Fatal("real error fell back"); return nil }
			t.Cleanup(func() { publishNoReplaceLink = prev })
			require.ErrorIs(t, publishNoReplaceOSFS("dummy", "unused"), errno)
		})
	}
}

func TestDarwinExclusiveRenameUnsupported(t *testing.T) {
	for _, errno := range []error{syscall.ENOSYS, syscall.EINVAL, syscall.ENOTSUP, syscall.EOPNOTSUPP} {
		t.Run(errno.Error(), func(t *testing.T) {
			darwinKernelError(t, errno)
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			dst := filepath.Join(dir, "dst")
			require.NoError(t, os.WriteFile(src, []byte("source"), 0600))
			require.NoError(t, publishNoReplaceOSFS(src, dst))
			require.NoError(t, os.Symlink(dst, src))
			err := publishNoReplaceOSFS(src, filepath.Join(dir, "other"))
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
			require.False(t, isCrossDeviceError(err))
			target, err := os.Readlink(src)
			require.NoError(t, err)
			require.Equal(t, dst, target)
		})
	}
	t.Run("missing source preserves existing destination", func(t *testing.T) {
		darwinKernelError(t, syscall.EINVAL)
		dir := t.TempDir()
		src := filepath.Join(dir, "missing")
		dst := filepath.Join(dir, "existing")
		require.NoError(t, os.WriteFile(dst, []byte("foreign destination"), 0600))
		before, err := os.Stat(dst)
		require.NoError(t, err)
		err = publishNoReplaceOSFS(src, dst)
		require.ErrorIs(t, err, os.ErrNotExist)
		require.NotErrorIs(t, err, syscall.EINVAL)
		require.False(t, isCrossDeviceError(err))
		after, err := os.Stat(dst)
		require.NoError(t, err)
		require.True(t, os.SameFile(before, after))
		require.Equal(t, before.Mode(), after.Mode())
		data, err := os.ReadFile(dst)
		require.NoError(t, err)
		require.Equal(t, "foreign destination", string(data))
	})

}

func TestDarwinUnsupportedEINVALDoesNotTriggerCopy(t *testing.T) {
	darwinKernelError(t, syscall.EINVAL)
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(string, string) error { return syscall.EPERM }
	t.Cleanup(func() { publishNoReplaceLink = prev })
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	require.NoError(t, os.WriteFile(src, []byte("source"), 0600))
	err := publishNoReplaceOSFS(src, filepath.Join(dir, "dst"))
	require.ErrorIs(t, err, syscall.EPERM)
	require.NotErrorIs(t, err, syscall.EINVAL)
	require.False(t, isCrossDeviceError(err))
}

func TestDarwinExclusiveRenameCaseAlias(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Dummy")
	dst := filepath.Join(dir, "dummy")
	require.NoError(t, os.WriteFile(src, []byte("source"), 0600))
	before, err := os.Stat(src)
	require.NoError(t, err)
	alias, err := os.Stat(dst)
	if os.IsNotExist(err) {
		t.Skip("case-sensitive filesystem")
	}
	require.NoError(t, err)
	require.True(t, os.SameFile(before, alias))
	err = publishNoReplaceOSFS(src, dst)
	if err != nil {
		require.ErrorIs(t, err, ErrPublishCollision)
	}
	after, err := os.Stat(src)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
}

func TestDarwinVerifiedSubstitutionAndRestore(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "restore collision"}[collision], func(t *testing.T) {
			fs := afero.NewOsFs()
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			dst := filepath.Join(dir, "dst")
			foreign := filepath.Join(dir, "foreign")
			require.NoError(t, os.WriteFile(src, []byte("admitted"), 0600))
			proof, _ := verifyProofOf(t, fs, src)
			require.NoError(t, os.WriteFile(foreign, []byte("foreign replacement"), 0600))
			require.NoError(t, os.Rename(foreign, src))
			var claim string
			refusing := VerifiedSourceProof(func(path string, info os.FileInfo) error {
				claim = path
				if collision {
					require.NoError(t, os.WriteFile(src, []byte("late source"), 0600))
				}
				return proof(path, info)
			})
			err := MoveFileNoReplaceVerified(fs, src, dst, refusing)
			require.ErrorIs(t, err, ErrTakeAsideForeign)
			if collision {
				require.ErrorIs(t, err, ErrTakeAsideRestoreFailed)
				data, err := os.ReadFile(claim)
				require.NoError(t, err)
				require.Equal(t, "foreign replacement", string(data))
				data, err = os.ReadFile(src)
				require.NoError(t, err)
				require.Equal(t, "late source", string(data))
			} else {
				data, err := os.ReadFile(src)
				require.NoError(t, err)
				require.Equal(t, "foreign replacement", string(data))
				_, err = os.Lstat(claim)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
			_, err = os.Lstat(dst)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
