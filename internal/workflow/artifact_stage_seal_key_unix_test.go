//go:build !windows

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func sweepKeyMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

type sweepKeyInfoWithoutOwner struct{ os.FileInfo }

func (sweepKeyInfoWithoutOwner) Sys() any { return nil }

func TestReadArtifactSweepSecret_POSIXPolicy(t *testing.T) {
	valid := []byte(strings.Repeat("k", artifactStageSecretBytes))

	t.Run("owner-only key", func(t *testing.T) {
		key, err := readArtifactSweepSecret(writeSweepKeyFixture(t, valid))
		require.NoError(t, err)
		require.Equal(t, valid, key)
	})

	t.Run("wide key is rejected without chmod or rotation", func(t *testing.T) {
		for _, mode := range []os.FileMode{0o644, 0o660, 0o666, 0o601, 0o610} {
			path := writeSweepKeyFixture(t, valid)
			require.NoError(t, os.Chmod(path, mode))
			oldPath := artifactSweepSecretPath
			artifactSweepSecretPath = func() (string, error) { return path, nil }
			key, err := defaultArtifactSweepSecretKey()
			artifactSweepSecretPath = oldPath
			require.ErrorContains(t, err, "accessible beyond its owner")
			require.Nil(t, key)
			require.Equal(t, mode, sweepKeyMode(t, path))
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, valid, body)
		}
	})

	t.Run("unknown ownership fails closed", func(t *testing.T) {
		path := writeSweepKeyFixture(t, valid)
		info := mustSweepStat(t, path)
		require.ErrorContains(t, secureArtifactSweepSecretPOSIX(path, sweepKeyInfoWithoutOwner{info}), "cannot verify")
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

func TestReadArtifactSweepSecret_FIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), artifactStageSecretName)
	require.NoError(t, syscall.Mkfifo(path, 0o600))
	done := make(chan error, 1)
	go func() { _, err := readArtifactSweepSecret(path); done <- err }()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "not a regular file")
	case <-time.After(2 * time.Second):
		// Unblock a broken implementation so the test does not leak a goroutine.
		f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = f.Close()
		}
		t.Fatal("key FIFO open blocked before validation")
	}
}

func TestArtifactSweepSecretCreate_POSIXValidationBeforeWrite(t *testing.T) {
	for _, test := range []string{"unsafe mode", "foreign owner", "unknown owner"} {
		t.Run(test, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), artifactStageSecretName)
			oldPath, oldCreate, oldUID := artifactSweepSecretPath, artifactSweepCreateKey, artifactSweepGeteuid
			t.Cleanup(func() {
				artifactSweepSecretPath, artifactSweepCreateKey, artifactSweepGeteuid = oldPath, oldCreate, oldUID
			})
			artifactSweepSecretPath = func() (string, error) { return path, nil }
			var created *os.File
			artifactSweepCreateKey = func(path string) (artifactSweepKeyFile, error) {
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				require.NoError(t, err)
				created = f
				switch test {
				case "unsafe mode":
					require.NoError(t, f.Chmod(0o660)) // Simulate storage overriding requested 0600.
				case "foreign owner":
					artifactSweepGeteuid = func() int { return os.Geteuid() + 1 }
				case "unknown owner":
					return &sweepKeyUnknownOwnerFile{f}, nil
				}
				return f, nil
			}
			key, err := defaultArtifactSweepSecretKey()
			require.Error(t, err)
			require.Nil(t, key)
			info, err := os.Stat(path)
			require.NoError(t, err, "failed creation is retained, never silently deleted")
			require.Zero(t, info.Size(), "no secret bytes before validation")
			_, err = created.Write([]byte("closed?"))
			require.Error(t, err, "rejected handle must be closed")
			// A later attempt must reject the retained file, not create a new key.
			artifactSweepCreateKey = func(string) (artifactSweepKeyFile, error) { t.Fatal("must not rotate"); return nil, nil }
			key, err = defaultArtifactSweepSecretKey()
			require.Error(t, err)
			require.Nil(t, key)
		})
	}
}

type sweepKeyUnknownOwnerFile struct{ *os.File }

func (f *sweepKeyUnknownOwnerFile) Stat() (os.FileInfo, error) {
	info, err := f.File.Stat()
	return sweepKeyInfoWithoutOwner{info}, err
}

func TestSweepArtifactStaging_UnsafeKeyRetainsSealedRoot(t *testing.T) {
	fs := afero.NewMemMapFs()
	setSweepSeams(t, fsutil.ProcessDead, nil)
	path := writeSweepKeyFixture(t, []byte(strings.Repeat("k", artifactStageSecretBytes)))
	oldPath, oldKey := artifactSweepSecretPath, artifactSweepSecretKey
	t.Cleanup(func() { artifactSweepSecretPath, artifactSweepSecretKey = oldPath, oldKey })
	artifactSweepSecretPath = func() (string, error) { return path, nil }
	artifactSweepSecretKey = defaultArtifactSweepSecretKey
	root := seedStagingRoot(t, fs, "/lib", func(m *artifactStageManifest) {
		m.CompletedUnixNano = time.Now().UTC().UnixNano()
	})
	require.NoError(t, os.Chmod(path, 0o660))
	sweepArtifactStaging(fs, "/lib")
	exists, err := afero.DirExists(fs, root)
	require.NoError(t, err)
	require.True(t, exists, "even a previously valid sealed root must survive unsafe key validation")
	require.Equal(t, os.FileMode(0o660), sweepKeyMode(t, path), "do not silently repair or rotate")
}
