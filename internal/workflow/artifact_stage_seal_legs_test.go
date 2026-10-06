package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultArtifactSweepSecretPath_Legs(t *testing.T) {
	// happy path: cache dir answers
	oldCache, oldHome := artifactSweepUserCacheDir, artifactSweepUserHomeDir
	t.Cleanup(func() { artifactSweepUserCacheDir, artifactSweepUserHomeDir = oldCache, oldHome })

	artifactSweepUserCacheDir = func() (string, error) { return "/cache", nil }
	artifactSweepUserHomeDir = func() (string, error) { return "/home", nil }
	got, err := defaultArtifactSweepSecretPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/cache", "Javinizer", artifactStageSecretName), got)

	// cache-dir lookup fails -> home fallback
	artifactSweepUserCacheDir = func() (string, error) { return "", errors.New("no cache dir") }
	got, err = defaultArtifactSweepSecretPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home", ".javinizer", "Javinizer", artifactStageSecretName), got)

	// cache dir empty -> home fallback also exercised
	artifactSweepUserCacheDir = func() (string, error) { return "", nil }
	got, err = defaultArtifactSweepSecretPath()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, "/home"))

	// both fail -> wrapped composite error
	artifactSweepUserCacheDir = func() (string, error) { return "", errors.New("cache gone") }
	artifactSweepUserHomeDir = func() (string, error) { return "", errors.New("home gone") }
	_, err = defaultArtifactSweepSecretPath()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache gone")
	assert.Contains(t, err.Error(), "home gone")
}

type fakeKeyFile struct {
	writeErr error
	closeErr error
	closed   bool
}

func (f *fakeKeyFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeKeyFile) Close() error {
	f.closed = true
	return f.closeErr
}

func TestArtifactSweepSecretLoader_FailureLegs(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Javinizer", artifactStageSecretName)
	fresh := func() (string, error) { return target, nil }

	oldPath, oldMkdir, oldRand, oldCreate := artifactSweepSecretPath, artifactSweepMkdirAll, artifactSweepRand, artifactSweepCreateKey
	t.Cleanup(func() {
		artifactSweepSecretPath, artifactSweepMkdirAll, artifactSweepRand, artifactSweepCreateKey = oldPath, oldMkdir, oldRand, oldCreate
	})

	// mkdirAll failure
	artifactSweepSecretPath = fresh
	artifactSweepMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir denied") }
	_, err := defaultArtifactSweepSecretKey()
	require.ErrorContains(t, err, "create artifact sweep key dir")

	// rand draw failure
	artifactSweepMkdirAll = oldMkdir
	require.NoError(t, os.RemoveAll(dir))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	artifactSweepRand = func(b []byte) (int, error) { return 0, errors.New("entropy denied") }
	_, err = defaultArtifactSweepSecretKey()
	require.ErrorContains(t, err, "draw artifact sweep key")

	// generic create failure (not IsExist)
	artifactSweepRand = oldRand
	artifactSweepCreateKey = func(string) (artifactSweepKeyFile, error) { return nil, errors.New("create denied") }
	_, err = defaultArtifactSweepSecretKey()
	require.ErrorContains(t, err, "create artifact sweep key")

	// write failure — handle is closed on the way out
	writeFail := &fakeKeyFile{writeErr: errors.New("write denied")}
	artifactSweepCreateKey = func(string) (artifactSweepKeyFile, error) { return writeFail, nil }
	_, err = defaultArtifactSweepSecretKey()
	require.ErrorContains(t, err, "write artifact sweep key")
	assert.True(t, writeFail.closed, "failed write still closes the handle")

	// close failure
	closeFail := &fakeKeyFile{closeErr: errors.New("close denied")}
	artifactSweepCreateKey = func(string) (artifactSweepKeyFile, error) { return closeFail, nil }
	_, err = defaultArtifactSweepSecretKey()
	require.ErrorContains(t, err, "close artifact sweep key")
}

func TestArtifactSweepSecretLoader_ConcurrentCreateWonKey(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "race", artifactStageSecretName)

	oldPath, oldCreate := artifactSweepSecretPath, artifactSweepCreateKey
	t.Cleanup(func() { artifactSweepSecretPath, artifactSweepCreateKey = oldPath, oldCreate })
	artifactSweepSecretPath = func() (string, error) { return target, nil }
	artifactSweepCreateKey = func(path string) (artifactSweepKeyFile, error) {
		// A concurrent Javinizer process wins the create and writes its key
		// first; our create reports ErrExist and we re-read the winner's key.
		winner := []byte("0123456789abcdef0123456789abcdef") // 32 bytes per artifactStageSecretBytes
		_ = os.WriteFile(path, winner, 0o600)
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrExist}
	}
	got, err := defaultArtifactSweepSecretKey()
	require.NoError(t, err)
	assert.Len(t, got, artifactStageSecretBytes, "the concurrent winner's key is what every process uses")
}

func TestArtifactStageSealed_NegativeLegs(t *testing.T) {
	manifest := &artifactStageManifest{Seal: strings.Repeat("ab", 32)}

	oldKey, oldMarshal := artifactSweepSecretKey, artifactSweepMarshal
	t.Cleanup(func() { artifactSweepSecretKey, artifactSweepMarshal = oldKey, oldMarshal })

	// key load failure -> false
	artifactSweepSecretKey = func() ([]byte, error) { return nil, errors.New("key store denied") }
	assert.False(t, artifactStageSealed(manifest))
	artifactSweepSecretKey = oldKey

	// marshal failure of the recomputed canonical form -> false
	require.NoError(t, sealArtifactStageManifest(manifest), "seal with the pinned test key")
	artifactSweepMarshal = func(any) ([]byte, error) { return nil, errors.New("encode denied") }
	assert.False(t, artifactStageSealed(manifest))
}
