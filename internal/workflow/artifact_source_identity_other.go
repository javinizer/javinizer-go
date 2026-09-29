//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package workflow

import (
	"os"

	"github.com/spf13/afero"
)

// Neither the POSIX Stat_t identity nor the Windows handle identity is
// available on this target: the identity degrades to the size+modtime legs
// exactly like the downloader/history constructions.
func artifactSourceDevIno(_ afero.Fs, _ string, _ os.FileInfo) (device, inode uint64, ok bool) {
	return 0, 0, false
}
