//go:build !windows

package history

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
)

func primaryUndoCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
func primaryUndoDurabilitySupported() error { return nil }
func primaryUndoSyncDir(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for parent := filepath.Dir(abs); ; parent = filepath.Dir(parent) {
		dir, err := os.Open(parent)
		if err != nil {
			return err
		}
		if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
			return err
		}
		if filepath.Dir(parent) == parent {
			return nil
		}
	}
}

// POSIX FileInfo eagerly carries the opened object's native dev/inode pair.
func primaryUndoObjectKey(_ afero.File, info os.FileInfo) (uint64, uint64, string, bool) {
	dev, ino, strong := fsutil.BoundObjectIdentity(nil, "", info)
	return dev, ino, "", strong
}

func primaryUndoPublishNoReplace(fs afero.Fs, source, dest string) error {
	return fsutil.PublishNoReplace(fs, source, dest)
}
