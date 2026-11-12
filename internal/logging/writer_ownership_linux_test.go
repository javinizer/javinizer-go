package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A rejected output list must release every file it opened, not merely avoid
// publishing the new logger. Inspect actual descriptors because Linux permits
// renaming and unlinking open files.
func TestRejectedOutputListReleasesAllFileDescriptors(t *testing.T) {
	if _, err := os.ReadDir("/proc/self/fd"); os.IsNotExist(err) || os.IsPermission(err) {
		t.Skipf("descriptor inspection requires accessible procfs: %v", err)
	} else {
		require.NoError(t, err)
	}
	for _, failure := range []string{"create directory", "open file"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			paths := []string{filepath.Join(root, "first.log"), filepath.Join(root, "second.log")}
			bad := filepath.Join(root, "blocked")
			want := "failed to open log file"
			if failure == "create directory" {
				require.NoError(t, os.WriteFile(bad, []byte("file"), 0600))
				bad = filepath.Join(bad, "output.log")
				want = "failed to create log directory"
			} else {
				require.NoError(t, os.Mkdir(bad, 0700))
			}
			init := newLoggerInit()
			for range 3 {
				err := init.Init(&Config{Level: "info", Format: "text", Output: strings.Join(paths, ",") + "," + bad})
				require.ErrorContains(t, err, want)
				require.Nil(t, init.current.Load(), "failed setup must not publish a logger")
				entries, err := os.ReadDir("/proc/self/fd")
				require.NoError(t, err)
				for _, path := range paths {
					fileInfo, err := os.Stat(path)
					require.NoError(t, err)
					for _, entry := range entries {
						fdInfo, err := os.Stat(filepath.Join("/proc/self/fd", entry.Name()))
						if os.IsNotExist(err) {
							continue // ReadDir's own descriptor is already closed.
						}
						require.NoError(t, err)
						require.False(t, os.SameFile(fileInfo, fdInfo), "failed setup leaked %s via descriptor %s", path, entry.Name())
					}
				}
			}
		})
	}
}
