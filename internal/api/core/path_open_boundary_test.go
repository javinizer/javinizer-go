package core

import (
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/stretchr/testify/require"
)

func TestOpenValidatedDirectoryFilesystemFailures(t *testing.T) {
	for _, stage := range []string{"stat", "open"} {
		for _, failure := range []struct {
			name string
			err  error
			want *apperrors.PathError
		}{
			{"missing", os.ErrNotExist, apperrors.ErrPathNotExist},
			{"permission", os.ErrPermission, apperrors.ErrPathInvalid},
		} {
			t.Run(stage+"/"+failure.name, func(t *testing.T) {
				dir := t.TempDir()
				var calls []string
				stat := func(path string) (os.FileInfo, error) {
					calls = append(calls, "stat")
					require.Equal(t, dir, path)
					if stage == "stat" {
						return nil, &os.PathError{Op: stage, Path: path, Err: failure.err}
					}
					return os.Stat(path)
				}
				open := func(path string) (*os.File, error) {
					calls = append(calls, "open")
					require.Equal(t, dir, path)
					return nil, &os.PathError{Op: stage, Path: path, Err: failure.err}
				}

				f, path, err := openValidatedDirectory(dir, stat, open)
				require.Nil(t, f, "failed acquisition must not transfer a descriptor")
				require.Empty(t, path)
				require.ErrorIs(t, err, failure.want)
				var pathErr *apperrors.PathError
				require.ErrorAs(t, err, &pathErr)
				require.Equal(t, failure.want.Code, pathErr.Code)
				require.Equal(t, failure.want.HTTPStatus, pathErr.HTTPStatus)
				if failure.want == apperrors.ErrPathNotExist {
					require.Equal(t, dir, pathErr.Path)
				} else {
					require.Same(t, apperrors.ErrPathInvalid, err)
				}
				wantCalls := []string{"stat"}
				if stage == "open" {
					wantCalls = append(wantCalls, "open")
				}
				require.Equal(t, wantCalls, calls)
			})
		}
	}
}

// Metadata without a native Sys payload models platforms that cannot extract
// pre-open identity, while retaining the real file's other metadata.
type pathInfoWithoutIdentity struct{ os.FileInfo }

func (pathInfoWithoutIdentity) Sys() any { return nil }

func TestOpenValidatedDirectoryWithoutPreOpenIdentity(t *testing.T) {
	for _, regularFile := range []bool{false, true} {
		name := "directory_remains_open_for_caller"
		if regularFile {
			name = "non_directory_is_rejected_and_closed"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			info, err := os.Stat(dir)
			require.NoError(t, err)
			stat := func(path string) (os.FileInfo, error) {
				require.Equal(t, dir, path)
				return pathInfoWithoutIdentity{info}, nil
			}
			var opened *os.File
			open := func(path string) (*os.File, error) {
				require.Equal(t, dir, path)
				if regularFile {
					// With no pre-open identity, the final descriptor type check
					// must still reject a directory replaced by a regular file.
					require.NoError(t, os.Remove(dir))
					require.NoError(t, os.WriteFile(dir, []byte("replacement"), 0600))
				}
				var err error
				opened, err = os.Open(path)
				if opened != nil {
					t.Cleanup(func() { _ = opened.Close() })
				}
				return opened, err
			}

			f, path, err := openValidatedDirectory(dir, stat, open)
			require.NotNil(t, opened)
			if regularFile {
				require.Nil(t, f)
				require.Empty(t, path)
				require.ErrorIs(t, err, apperrors.ErrPathNotDir)
				var pathErr *apperrors.PathError
				require.ErrorAs(t, err, &pathErr)
				require.Equal(t, dir, pathErr.Path)
				// Stat may expose a native invalid-handle error on Windows.
				// A second Close proves the rejection already closed the file.
				require.ErrorIs(t, opened.Close(), os.ErrClosed)
				return
			}
			require.NoError(t, err)
			require.Same(t, opened, f)
			require.Equal(t, dir, path)
			info, err = f.Stat()
			require.NoError(t, err, "success transfers an open descriptor to the caller")
			require.True(t, info.IsDir())
			require.NoError(t, f.Close())
		})
	}
}

func TestOpenValidatedDirectoryRejectsUnusableDescriptor(t *testing.T) {
	dir := t.TempDir()
	open := func(path string) (*os.File, error) {
		require.Equal(t, dir, path)
		f, err := os.Open(path)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		return f, nil
	}

	// An unavailable post-open identity does not itself reject a descriptor;
	// the final Stat must still run and reject an unusable handle.
	f, path, err := openValidatedDirectory(dir, os.Stat, open)
	require.Nil(t, f)
	require.Empty(t, path)
	require.Same(t, apperrors.ErrPathInvalid, err)
}
