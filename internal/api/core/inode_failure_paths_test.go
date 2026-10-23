//go:build unix || darwin || linux || freebsd || netbsd || openbsd

package core

import (
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestFileIdentityRejectsUnsupportedMetadataAndClosedDescriptor(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/file", []byte("data"), 0600))
	info, err := fs.Stat("/file")
	require.NoError(t, err)
	identity, err := getFileIdentity(info)
	require.ErrorIs(t, err, apperrors.ErrInodeExtraction)
	require.Equal(t, fileIdentity{}, identity)
	file, err := os.CreateTemp(t.TempDir(), "identity")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	identity, err = getFileIdentityFromFd(file)
	require.ErrorIs(t, err, os.ErrClosed)
	require.Equal(t, fileIdentity{}, identity)
}
func TestExpandHomeDirUnavailableHomePreservesInput(t *testing.T) {
	t.Setenv("HOME", "")
	require.Equal(t, "~", ExpandHomeDir("~"))
	require.Equal(t, "~/movie", ExpandHomeDir("~/movie"))
}
