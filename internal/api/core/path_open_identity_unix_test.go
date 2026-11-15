//go:build unix || darwin || linux || freebsd || netbsd || openbsd

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/stretchr/testify/require"
)

func TestOpenValidatedDirectoryRejectsReplacement(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "validated")
	replacement := filepath.Join(root, "replacement")
	require.NoError(t, os.Mkdir(dir, 0700))
	require.NoError(t, os.Mkdir(replacement, 0700))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	_, err = getFileIdentity(info)
	require.NoError(t, err, "Unix must provide pre-open identity for swap detection")

	var opened *os.File
	open := func(path string) (*os.File, error) {
		require.Equal(t, dir, path)
		// Interleave a real directory replacement exactly between the pre-open
		// stat and open. No goroutine, sleep, or race is needed to model a swap.
		require.NoError(t, os.Rename(dir, filepath.Join(root, "original")))
		require.NoError(t, os.Rename(replacement, dir))
		var err error
		opened, err = os.Open(path)
		if opened != nil {
			t.Cleanup(func() { _ = opened.Close() })
		}
		return opened, err
	}
	f, path, err := openValidatedDirectory(dir, os.Stat, open)
	require.Nil(t, f)
	require.Empty(t, path)
	require.ErrorIs(t, err, apperrors.ErrInodeMismatch)
	var pathErr *apperrors.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, dir, pathErr.Path)
	require.NotNil(t, opened)
	_, err = opened.Stat()
	require.ErrorIs(t, err, os.ErrClosed, "rejected replacement descriptor must be closed")
}
