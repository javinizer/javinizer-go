//go:build !windows

package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func sweepKeyMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

func stubSweepKeyChmod(t *testing.T, fn func(*os.File, os.FileMode) error) *int {
	t.Helper()
	calls := new(int)
	old := artifactSweepKeyChmod
	artifactSweepKeyChmod = func(f *os.File, mode os.FileMode) error {
		*calls++
		if fn == nil {
			return old(f, mode)
		}
		return fn(f, mode)
	}
	t.Cleanup(func() { artifactSweepKeyChmod = old })
	return calls
}

func TestReadArtifactSweepSecret_POSIXPolicy(t *testing.T) {
	valid := []byte(strings.Repeat("k", artifactStageSecretBytes))

	t.Run("owner-only key needs no chmod", func(t *testing.T) {
		calls := stubSweepKeyChmod(t, nil)
		key, err := readArtifactSweepSecret(writeSweepKeyFixture(t, valid))
		require.NoError(t, err)
		require.Equal(t, valid, key)
		require.Zero(t, *calls)
	})

	t.Run("wide key is tightened to owner-only", func(t *testing.T) {
		calls := stubSweepKeyChmod(t, nil)
		path := writeSweepKeyFixture(t, valid)
		require.NoError(t, os.Chmod(path, 0o644))
		key, err := readArtifactSweepSecret(path)
		require.NoError(t, err)
		require.Equal(t, valid, key)
		require.Equal(t, 1, *calls)
		require.Equal(t, os.FileMode(0o600), sweepKeyMode(t, path))
	})

	t.Run("filesystem refusing chmod keeps the ACL-governed key", func(t *testing.T) {
		for _, errno := range []error{syscall.EPERM, syscall.ENOTSUP, syscall.EOPNOTSUPP} {
			calls := stubSweepKeyChmod(t, func(f *os.File, _ os.FileMode) error {
				return &os.PathError{Op: "chmod", Path: f.Name(), Err: errno}
			})
			path := writeSweepKeyFixture(t, valid)
			require.NoError(t, os.Chmod(path, 0o660))
			t.Cleanup(func() { artifactSweepKeyWarns.Delete(path) })
			for i := 0; i < 2; i++ {
				key, err := readArtifactSweepSecret(path)
				require.NoError(t, err)
				require.Equal(t, valid, key)
			}
			require.Equal(t, 2, *calls)
			_, warned := artifactSweepKeyWarns.Load(path)
			require.True(t, warned)
			require.Equal(t, os.FileMode(0o660), sweepKeyMode(t, path))
		}
	})

	t.Run("unexpected chmod failure refuses the key", func(t *testing.T) {
		stubSweepKeyChmod(t, func(*os.File, os.FileMode) error { return syscall.EIO })
		path := writeSweepKeyFixture(t, valid)
		require.NoError(t, os.Chmod(path, 0o644))
		_, err := readArtifactSweepSecret(path)
		require.ErrorIs(t, err, syscall.EIO)
		require.ErrorContains(t, err, "restrict artifact sweep key")
	})

	t.Run("foreign-owned key is refused", func(t *testing.T) {
		old := artifactSweepGeteuid
		t.Cleanup(func() { artifactSweepGeteuid = old })
		artifactSweepGeteuid = func() int { return os.Geteuid() + 1 }
		_, err := readArtifactSweepSecret(writeSweepKeyFixture(t, valid))
		require.ErrorContains(t, err, "is owned by uid")
	})

	t.Run("symlinked key is never followed or replaced", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "attacker.key")
		require.NoError(t, os.WriteFile(target, valid, 0o600))
		path := filepath.Join(dir, "Javinizer", artifactStageSecretName)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.Symlink(target, path))

		_, err := readArtifactSweepSecret(path)
		require.Error(t, err)
		require.False(t, os.IsNotExist(err))

		oldPath := artifactSweepSecretPath
		t.Cleanup(func() { artifactSweepSecretPath = oldPath })
		artifactSweepSecretPath = func() (string, error) { return path, nil }
		_, err = defaultArtifactSweepSecretKey()
		require.Error(t, err, "a symlinked key fails closed instead of being trusted")
		info, lerr := os.Lstat(path)
		require.NoError(t, lerr)
		require.NotZero(t, info.Mode()&os.ModeSymlink, "the planted symlink is left in place")
		body, rerr := os.ReadFile(target)
		require.NoError(t, rerr)
		require.Equal(t, valid, body)
	})
}

func TestArtifactSweepKeyModeRefused(t *testing.T) {
	require.True(t, artifactSweepKeyModeRefused(&os.PathError{Err: syscall.EPERM}))
	require.False(t, artifactSweepKeyModeRefused(errors.New("other")))
}
