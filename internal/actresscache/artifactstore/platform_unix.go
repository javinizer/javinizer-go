//go:build darwin || linux

package artifactstore

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func privateDirectory(info os.FileInfo) error {
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("store directory must not be group/other writable")
	}
	return nil
}
func syncDirectory(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
}
func replaceManifest(root *os.Root, oldname, newname string) error {
	return root.Rename(oldname, newname)
}
func (s *Store) lock() (*os.File, error) {
	const name = ".writer.lock"
	if info, err := s.root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe writer lock")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := s.ops.openFile(s.root, name, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := s.ops.lockHandle(file); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return file, nil
}

func lockHandle(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
