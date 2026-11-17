//go:build windows

package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestNoClobberProbeWindowsBypass(t *testing.T) {
	for _, fs := range []afero.Fs{afero.NewOsFs(), afero.NewMemMapFs()} {
		require.NoError(t, ProbeNoClobberPublish(fs, `Z:\missing\directory`))
	}
}

func TestNoClobberDirIdentityWindowsUsesPathOnlyCache(t *testing.T) {
	// Windows deliberately has no inode identity discriminator. The same
	// directory path must keep the documented path-only cache behavior,
	// including before it exists and after its contents change.
	dir := filepath.Join(t.TempDir(), "probe")
	require.Empty(t, noClobberDirIdentity(dir))
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.Empty(t, noClobberDirIdentity(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "entry"), []byte("data"), 0o600))
	require.Empty(t, noClobberDirIdentity(dir))
}
