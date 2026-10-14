package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type boundLinkLstatOnlyFS struct{ afero.Fs }

func (f *boundLinkLstatOnlyFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

type boundLinkReadErrorFS struct {
	*boundLinkLstatOnlyFS
	err error
}

func (f *boundLinkReadErrorFS) ReadlinkIfPossible(string) (string, error) { return "", f.err }

func TestObserveVerifiedInstalledLink(t *testing.T) {
	base := afero.NewOsFs()
	dir := t.TempDir()
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "link")
	require.NoError(t, afero.WriteFile(base, source, []byte("own source"), 0o600))
	if err := os.Symlink(source, target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	info, err := os.Lstat(target)
	require.NoError(t, err)
	installed := NewBoundInstallLinkIdentity(nil, source)
	require.Nil(t, installed.FileInfo())
	require.Nil(t, NewBoundInstallLinkIdentity(nil, ""))
	require.NotNil(t, NewBoundInstallLinkIdentity(info, source))
	observed, err := ObserveVerifiedInstall(base, target, installed)
	require.NoError(t, err)
	require.NotNil(t, observed)
	require.NotZero(t, observed.Mode()&os.ModeSymlink)

	limited := &boundLinkLstatOnlyFS{Fs: base}
	_, err = ObserveVerifiedInstall(limited, target, installed)
	require.ErrorIs(t, err, ErrPublishCompleted)
	require.NotErrorIs(t, err, ErrPublishSuccessorUnproven)
	fault := errors.New("injected readlink fault")
	_, err = ObserveVerifiedInstall(&boundLinkReadErrorFS{boundLinkLstatOnlyFS: limited, err: fault}, target, installed)
	require.ErrorIs(t, err, fault)
	require.ErrorIs(t, err, ErrPublishCompleted)
	require.NoError(t, os.Remove(target))
	require.NoError(t, os.Symlink(source+".foreign", target))
	_, err = ObserveVerifiedInstall(base, target, installed)
	require.ErrorIs(t, err, ErrPublishSuccessorUnproven)
	require.NoError(t, os.Remove(target))
	require.NoError(t, afero.WriteFile(base, target, []byte("foreign output"), 0o600))
	_, err = ObserveVerifiedInstall(base, target, installed)
	require.ErrorIs(t, err, ErrPublishSuccessorUnproven)
	require.ErrorIs(t, err, ErrPublishCompleted)
}
