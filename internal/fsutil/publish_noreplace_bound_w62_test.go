//go:build !windows

package fsutil

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Wave-62 coverage for publishNoReplaceRemoveBound: two SameFile-bound proofs
// then the unlink; every doubt leg preserves the occupant.

func TestPublishNoReplaceRemoveBoundW62_Legs(t *testing.T) {
	errStaged := errors.New("w62 staged stat wedged")
	errLink := errors.New("w62 link wedged")

	t.Run("first proof indeterminate surfaces verbatim", func(t *testing.T) {
		file := "/src-w62-a"
		prev := publishNoReplaceStagedVerify
		publishNoReplaceStagedVerify = func(string, os.FileInfo) (bool, error) { return false, errStaged }
		defer func() { publishNoReplaceStagedVerify = prev }()
		err := publishNoReplaceRemoveBound(file, nil)
		require.ErrorIs(t, err, errStaged, "stat wedge surfaces so the caller retries rather than touches the name")
	})

	t.Run("first proof mismatch refuses (foreign preserved)", func(t *testing.T) {
		file := "/src-w62-b"
		prev := publishNoReplaceStagedVerify
		publishNoReplaceStagedVerify = func(string, os.FileInfo) (bool, error) { return false, nil }
		defer func() { publishNoReplaceStagedVerify = prev }()
		err := publishNoReplaceRemoveBound(file, nil)
		require.ErrorIs(t, err, ErrTakeAsideForeign)
	})

	t.Run("second proof indeterminate surfaces verbatim", func(t *testing.T) {
		file := "/src-w62-c"
		calls := 0
		prev := publishNoReplaceStagedVerify
		publishNoReplaceStagedVerify = func(string, os.FileInfo) (bool, error) {
			calls++
			if calls == 2 {
				return false, errLink
			}
			return true, nil
		}
		defer func() { publishNoReplaceStagedVerify = prev }()
		err := publishNoReplaceRemoveBound(file, nil)
		require.ErrorIs(t, err, errLink)
	})

	t.Run("second proof divergence refuses", func(t *testing.T) {
		file := "/src-w62-d"
		calls := 0
		prev := publishNoReplaceStagedVerify
		publishNoReplaceStagedVerify = func(string, os.FileInfo) (bool, error) {
			calls++
			return calls == 1, nil
		}
		defer func() { publishNoReplaceStagedVerify = prev }()
		err := publishNoReplaceRemoveBound(file, nil)
		require.ErrorIs(t, err, ErrTakeAsideForeign, "second-proof divergence is the same foreign-preservation leg")
	})

	t.Run("both proofs pass unlinks", func(t *testing.T) {
		dir := t.TempDir()
		f := dir + "/staged"
		require.NoError(t, os.WriteFile(f, []byte("staged-bytes"), 0o644))
		info, err := os.Lstat(f)
		require.NoError(t, err)
		require.NoError(t, publishNoReplaceRemoveBound(f, info))
		_, lerr := os.Lstat(f)
		require.ErrorIs(t, lerr, os.ErrNotExist, "verified source removed")
	})
}
