//go:build darwin || linux

package actress

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestOpenProvisionInputFilesystemInterleavings(t *testing.T) {
	const source = "source"
	const changed = "target" // Equal size isolates inode replacement from size checks.
	const identityError = "dump identity, type or size changed while opening"
	remove := func(t *testing.T, path string) {
		t.Helper()
		require.NoError(t, os.Remove(path))
	}
	grow := func(t *testing.T, path string) {
		t.Helper()
		writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		require.NoError(t, err)
		t.Cleanup(func() { _ = writer.Close() })
		n, err := writer.WriteString("!")
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.NoError(t, writer.Close())
	}
	replace := func(kind string) func(*testing.T, string) {
		return func(t *testing.T, path string) {
			t.Helper()
			// Keep the original inode alive so its inode number cannot be reused.
			require.NoError(t, os.Rename(path, path+".original"))
			switch kind {
			case "regular":
				require.NoError(t, os.WriteFile(path, []byte(changed), 0600))
			case "symlink":
				require.NoError(t, os.Symlink(path+".target", path))
			case "symlink-original":
				require.NoError(t, os.Symlink(path+".original", path))
			case "directory":
				require.NoError(t, os.Mkdir(path, 0700))
			case "fifo":
				require.NoError(t, unix.Mkfifo(path, 0600))
			default:
				t.Fatalf("unknown replacement kind %q", kind)
			}
		}
	}

	for _, tc := range []struct {
		name       string
		beforeOpen func(*testing.T, string)
		afterOpen  func(*testing.T, string)
		wantErr    error
		wantOp     string
		wantChange bool
	}{
		{name: "unchanged input at exact size limit"},
		{name: "disappears before open", beforeOpen: remove, wantErr: os.ErrNotExist, wantOp: "open"},
		{name: "symlink before open", beforeOpen: replace("symlink"), wantErr: unix.ELOOP, wantOp: "open"},
		{name: "regular replacement before open", beforeOpen: replace("regular"), wantChange: true},
		{name: "directory before open", beforeOpen: replace("directory"), wantChange: true},
		{name: "fifo before open", beforeOpen: replace("fifo"), wantChange: true},
		{name: "grows before open", beforeOpen: grow, wantChange: true},
		{name: "disappears after open", afterOpen: remove, wantErr: os.ErrNotExist, wantOp: "lstat"},
		{name: "regular replacement after open", afterOpen: replace("regular"), wantChange: true},
		{name: "symlink after open", afterOpen: replace("symlink"), wantChange: true},
		{name: "symlink back to opened inode", afterOpen: replace("symlink-original"), wantChange: true},
		{name: "directory after open", afterOpen: replace("directory"), wantChange: true},
		{name: "fifo after open", afterOpen: replace("fifo"), wantChange: true},
		{name: "grows after open", afterOpen: grow, wantChange: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "dump.sql")
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			require.NoError(t, os.WriteFile(path+".target", []byte(changed), 0600))
			initial, err := os.Lstat(path)
			require.NoError(t, err)

			var opened *os.File
			var openErr error
			openCalls := 0
			input, err := openProvisionInputWithOpenFile(path, int64(len(source)), func(name string, flag int, perm os.FileMode) (*os.File, error) {
				openCalls++
				require.Equal(t, 1, openCalls, "admission must not retry a changed input")
				require.Equal(t, path, name)
				// Check flags before opening a substituted FIFO, so a regression
				// fails instead of hanging the test on a blocking open.
				require.Equal(t, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, flag)
				require.Zero(t, perm)
				if tc.beforeOpen != nil {
					tc.beforeOpen(t, path)
				}
				opened, openErr = os.OpenFile(name, flag, perm)
				if opened != nil {
					t.Cleanup(func() { _ = opened.Close() })
				}
				if openErr != nil {
					return opened, openErr
				}
				if tc.afterOpen != nil {
					tc.afterOpen(t, path)
				}
				return opened, nil
			})
			require.Equal(t, 1, openCalls)

			if tc.wantErr != nil || tc.wantChange {
				require.Nil(t, input, "rejected input must not escape to the converter")
				if tc.wantErr != nil {
					require.ErrorIs(t, err, tc.wantErr, "preserve the real OS error through admission")
					var pathErr *os.PathError
					require.ErrorAs(t, err, &pathErr)
					require.Equal(t, path, pathErr.Path)
					require.Equal(t, tc.wantOp, pathErr.Op)
				} else {
					require.EqualError(t, err, identityError)
				}
				if opened != nil {
					_, statErr := opened.Stat()
					require.ErrorIs(t, statErr, os.ErrClosed, "every post-open rejection must close its owned descriptor")
				} else {
					require.ErrorIs(t, err, openErr, "open failure must be returned unchanged")
				}
			} else {
				require.NoError(t, err)
				require.Same(t, opened, input, "success transfers the admitted descriptor to its caller")
				info, err := input.Stat()
				require.NoError(t, err, "success must leave the descriptor open")
				require.True(t, os.SameFile(initial, info))
				data, err := io.ReadAll(input)
				require.NoError(t, err)
				require.Equal(t, source, string(data))
				require.NoError(t, input.Close())
			}
			data, err := os.ReadFile(path + ".target")
			require.NoError(t, err)
			require.Equal(t, changed, string(data), "admission must not modify a substitution target")
		})
	}
}

func TestOpenProvisionInputPreflightDoesNotOpenRejectedLeaf(t *testing.T) {
	for _, kind := range []string{"missing", "symlink", "directory", "fifo", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "dump.sql")
			switch kind {
			case "symlink":
				require.NoError(t, os.WriteFile(path+".target", []byte("data"), 0600))
				require.NoError(t, os.Symlink(path+".target", path))
			case "directory":
				require.NoError(t, os.Mkdir(path, 0700))
			case "fifo":
				require.NoError(t, unix.Mkfifo(path, 0600))
			case "oversized":
				require.NoError(t, os.WriteFile(path, []byte("too large"), 0600))
			}
			input, err := openProvisionInputWithOpenFile(path, 4, func(string, int, os.FileMode) (*os.File, error) {
				t.Fatal("preflight must reject the leaf before trying to open it")
				return nil, errors.New("unexpected open")
			})
			require.Nil(t, input)
			if kind == "missing" {
				require.ErrorIs(t, err, os.ErrNotExist)
			} else {
				require.EqualError(t, err, "dump must be a bounded regular file, not a symlink or special file")
			}
		})
	}
}
