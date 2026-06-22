package history

import (
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// --- Additional miss-line coverage for reverter.go ---
// Focuses on error paths inside revertFile that are hard to trigger
// with plain MemMapFs.

// nfoWriteErrorFs wraps afero.Fs and blocks writes to .nfo files
type nfoWriteErrorFs struct {
	afero.Fs
}

func (fs *nfoWriteErrorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if len(name) >= 4 && name[len(name)-4:] == ".nfo" && flag&os.O_CREATE != 0 {
		return nil, fmt.Errorf("write error: NFO file blocked")
	}
	return fs.Fs.OpenFile(name, flag, perm)
}
