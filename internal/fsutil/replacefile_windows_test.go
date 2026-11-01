//go:build windows

package fsutil

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceFileWindows_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.tmp")
	dst := filepath.Join(dir, "destination.jpg")
	fs := afero.NewOsFs()
	require.NoError(t, afero.WriteFile(fs, src, []byte("new"), 0644))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("old"), 0644))

	require.NoError(t, ReplaceFile(fs, src, dst))
	got, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)
}

// Invalid Windows path encoding is a public error contract, not a simulated
// kernel failure. Real files at each valid prefix prove a NUL cannot truncate
// a caller path and accidentally publish or replace those files.
func TestNativeWindowsInvalidNULPublishContracts(t *testing.T) {
	type snapshot struct {
		info os.FileInfo
		data []byte
	}
	capture := func(t *testing.T, path string) snapshot {
		t.Helper()
		file, err := os.Open(path)
		require.NoError(t, err)
		data, readErr := io.ReadAll(file)
		info, statErr := file.Stat() // Eager descriptor identity on Windows.
		closeErr := file.Close()     // Never retain a handle across the operation.
		require.NoError(t, readErr)
		require.NoError(t, statErr)
		require.NoError(t, closeErr)
		return snapshot{info: info, data: data}
	}
	unchanged := func(t *testing.T, path string, before snapshot) {
		t.Helper()
		after := capture(t, path)
		require.True(t, os.SameFile(before.info, after.info), "file identity changed: %s", path)
		require.Equal(t, before.data, after.data)
		require.Equal(t, before.info.Mode().Perm(), after.info.Mode().Perm())
		require.Equal(t, before.info.ModTime(), after.info.ModTime())
	}
	for _, operation := range []struct {
		name    string
		publish func(afero.Fs, string, string) error
		replace bool
	}{
		{name: "replace", publish: ReplaceFile, replace: true},
		{name: "no_replace", publish: PublishNoReplace},
	} {
		t.Run(operation.name, func(t *testing.T) {
			for _, invalidArgument := range []string{"source", "destination"} {
				t.Run(invalidArgument, func(t *testing.T) {
					dir, err := filepath.Abs(t.TempDir())
					require.NoError(t, err)
					src := filepath.Join(dir, "source.tmp")
					dst := filepath.Join(dir, "destination.jpg")
					foreign := filepath.Join(dir, "unrelated.keep")
					fs := afero.NewOsFs()
					require.NoError(t, afero.WriteFile(fs, src, []byte("owned staged bytes"), 0o600))
					require.NoError(t, afero.WriteFile(fs, dst, []byte("existing destination"), 0o600))
					require.NoError(t, afero.WriteFile(fs, foreign, []byte("unrelated bytes"), 0o600))
					before := map[string]snapshot{src: capture(t, src), dst: capture(t, dst), foreign: capture(t, foreign)}
					entries, err := os.ReadDir(dir)
					require.NoError(t, err)
					badSrc, badDst := src, dst
					if invalidArgument == "source" {
						badSrc += "\x00ignored-suffix"
					} else {
						badDst += "\x00ignored-suffix"
					}
					err = operation.publish(fs, badSrc, badDst)
					require.ErrorIs(t, err, syscall.EINVAL)
					require.NotErrorIs(t, err, ErrPublishCollision)
					require.NotErrorIs(t, err, os.ErrNotExist)
					for path, old := range before {
						unchanged(t, path, old)
					}
					afterEntries, err := os.ReadDir(dir)
					require.NoError(t, err)
					require.Equal(t, len(entries), len(afterEntries), "invalid path left a residue")
					for i := range entries {
						require.Equal(t, entries[i].Name(), afterEntries[i].Name())
					}

					// Corrected inputs still obey the selected operation's authority:
					// replace may replace the owned destination; no-replace may not.
					retryDst := dst
					if !operation.replace {
						require.ErrorIs(t, operation.publish(fs, src, dst), ErrPublishCollision)
						for path, old := range before {
							unchanged(t, path, old)
						}
						retryDst = filepath.Join(dir, "vacant-retry.jpg")
					}
					require.NoError(t, operation.publish(fs, src, retryDst))
					published := capture(t, retryDst)
					require.Equal(t, before[src].data, published.data)
					require.True(t, os.SameFile(before[src].info, published.info))
					_, err = os.Stat(src)
					require.ErrorIs(t, err, os.ErrNotExist)
					unchanged(t, foreign, before[foreign])
					if !operation.replace {
						unchanged(t, dst, before[dst])
					}
				})
			}
		})
	}
}
