package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type wrapperCopyIdentityFS struct {
	afero.Fs
	destination string
	swap        bool
}

func (f *wrapperCopyIdentityFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}
func (f *wrapperCopyIdentityFS) Rename(source, destination string) error {
	if err := f.Fs.Rename(source, destination); err != nil {
		return err
	}
	if f.swap && destination == f.destination {
		info, err := f.Fs.Stat(destination)
		if err != nil {
			return err
		}
		body, err := afero.ReadFile(f.Fs, destination)
		if err != nil {
			return err
		}
		if err = f.Fs.Rename(destination, destination+".saved"); err != nil {
			return err
		}
		if err = afero.WriteFile(f.Fs, destination, body, info.Mode()); err != nil {
			return err
		}
		return f.Fs.Chtimes(destination, info.ModTime(), info.ModTime())
	}
	return nil
}

func TestWrapperCopyCarriesProducerIdentity(t *testing.T) {
	for _, digest := range []bool{false, true} {
		for _, swap := range []bool{false, true} {
			t.Run(fmt.Sprintf("digest=%v/swap=%v", digest, swap), func(t *testing.T) {
				fs := &wrapperCopyIdentityFS{Fs: afero.NewOsFs(), destination: filepath.Join(t.TempDir(), "output"), swap: swap}
				source := filepath.Join(t.TempDir(), "source")
				require.NoError(t, afero.WriteFile(fs, source, []byte("same bytes"), 0600))
				file, err := fs.Open(source)
				require.NoError(t, err)
				defer file.Close()
				var identity *BoundInstallIdentity
				if digest {
					_, identity, err = copyStreamNoReplaceDigestReproof(fs, file, fs.destination, nil, 0600)
				} else {
					identity, err = copyStreamNoReplaceInstallReproof(fs, file, fs.destination, nil)
				}
				if swap {
					require.Error(t, err)
					require.True(t, PublishSuccessorUnproven(err))
					require.True(t, PublishCompleted(err))
					require.Nil(t, identity)
					require.FileExists(t, fs.destination)
					require.FileExists(t, fs.destination+".saved")
				} else {
					require.NoError(t, err)
					dev, ino, strong := identity.ObjectIdentity()
					require.True(t, strong, "native-backed wrapper must retain producer handle key")
					info, err := fs.Stat(fs.destination)
					require.NoError(t, err)
					currentDev, currentIno, currentStrong := BoundObjectIdentity(fs, fs.destination, info)
					require.True(t, currentStrong)
					require.Equal(t, dev, currentDev)
					require.Equal(t, ino, currentIno)
				}
			})
		}
	}
}

func TestWrapperCopyVirtualIdentityRemainsWeak(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "source", []byte("virtual"), 0600))
	file, err := fs.Open("source")
	require.NoError(t, err)
	defer file.Close()
	identity, err := copyStreamNoReplaceInstallReproof(fs, file, "output", nil)
	require.NoError(t, err)
	_, _, strong := identity.ObjectIdentity()
	require.False(t, strong)
	require.Equal(t, int64(7), identity.FileInfo().Size())
}
