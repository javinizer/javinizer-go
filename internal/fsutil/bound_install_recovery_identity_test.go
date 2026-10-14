package fsutil

import (
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestBoundRecoveryIdentityUnavailable(t *testing.T) {
	var id *BoundInstallIdentity
	_, _, ok := id.ObjectIdentity()
	require.False(t, ok)
	_, _, ok = NewBoundInstallLinkIdentity(nil, "source").ObjectIdentity()
	require.False(t, ok)
}

func TestBoundRecoveryIdentityUsesStoredKernelKey(t *testing.T) {
	fs := afero.NewOsFs()
	path := filepath.Join(t.TempDir(), "owned")
	require.NoError(t, afero.WriteFile(fs, path, []byte("data"), 0600))
	info, err := fs.Stat(path)
	require.NoError(t, err)
	id := NewObservedBoundInstallIdentity(fs, path, info)
	dev, ino, ok := id.ObjectIdentity()
	require.True(t, ok)
	require.NoError(t, fs.Rename(path, path+".saved"))
	require.NoError(t, afero.WriteFile(fs, path, []byte("data"), 0600))
	gotDev, gotIno, gotOK := id.ObjectIdentity()
	require.True(t, gotOK)
	require.Equal(t, dev, gotDev)
	require.Equal(t, ino, gotIno)
	weak := NewWeakBoundInstallIdentity(info)
	_, _, _ = weak.ObjectIdentity()
}
