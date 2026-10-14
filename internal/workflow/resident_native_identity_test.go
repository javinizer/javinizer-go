package workflow

import (
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

// residentTestIdentity freezes the native key while the observed path still
// exists. os.SameFile can lazily reopen that path on Windows after a rename,
// losing the historical object or accidentally observing its replacement.
// FileInfo is retained only for existing metadata assertions and ledger setup.
type residentTestIdentity struct {
	os.FileInfo
	device, inode uint64
}

func statResidentTestIdentity(t *testing.T, fs afero.Fs, path string) (os.FileInfo, error) {
	t.Helper()
	info, err := fs.Stat(path)
	if err != nil {
		return nil, err
	}
	dev, ino, strong := fsutil.BoundObjectIdentity(fs, path, info)
	require.True(t, strong, "native fixture must expose an eager strong identity: %s", path)
	return residentTestIdentity{info, dev, ino}, nil
}

func sameResidentTestIdentity(a, b os.FileInfo) bool {
	x, xok := a.(residentTestIdentity)
	y, yok := b.(residentTestIdentity)
	return xok && yok && x.device == y.device && x.inode == y.inode
}

func TestResidentNativeIdentitySnapshotRejectsSameByteSuccessor(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			fs := afero.NewOsFs()
			original := filepath.Join(t.TempDir(), "original")
			create := func(path string) {
				if directory {
					require.NoError(t, fs.Mkdir(path, 0755))
					path = filepath.Join(path, "payload")
				}
				require.NoError(t, afero.WriteFile(fs, path, []byte("identical payload"), 0644))
			}
			create(original)
			before, err := statResidentTestIdentity(t, fs, original)
			require.NoError(t, err)
			saved := original + ".saved"
			require.NoError(t, fs.Rename(original, saved))
			create(original)
			require.NoError(t, fs.Chtimes(original, before.ModTime(), before.ModTime()))
			replacement, err := statResidentTestIdentity(t, fs, original)
			require.NoError(t, err)
			require.False(t, sameResidentTestIdentity(before, replacement), "same bytes and timestamp must not replace the historical native key")
			moved, err := statResidentTestIdentity(t, fs, saved)
			require.NoError(t, err)
			require.True(t, sameResidentTestIdentity(before, moved), "identity must survive native rename")
			require.False(t, sameResidentTestIdentity(before.(residentTestIdentity).FileInfo, moved), "uncaptured metadata must never count as identity proof")
		})
	}
}
