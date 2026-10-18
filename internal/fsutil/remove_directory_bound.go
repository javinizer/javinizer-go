package fsutil

import (
	"fmt"
	"os"

	"github.com/spf13/afero"
)

type boundDirectoryRootInterface interface {
	openDirectory() (afero.File, error)
	RemoveAll(string) error
	Close() error
}

type nativeBoundDirectoryRoot struct{ *os.Root }

func (r nativeBoundDirectoryRoot) openDirectory() (afero.File, error) { return r.Open(".") }

var openBoundDirectoryRoot = func(path string) (boundDirectoryRootInterface, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return nativeBoundDirectoryRoot{Root: root}, nil
}

var removeBoundDirectoryChild = func(root boundDirectoryRootInterface, child string) error {
	return root.RemoveAll(child)
}

func removeDirectoryContentsBound(fs afero.Fs, path string, device, inode uint64) error {
	root, err := openBoundDirectoryRoot(path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	directory, err := root.openDirectory()
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	dev, ino, ok := streamedHandleIdentity(directory, info)
	if !ok || dev != device || ino != inode || !info.IsDir() {
		return fmt.Errorf("bound directory %s no longer names the admitted object: %w", path, ErrTakeAsideForeign)
	}
	named, err := fs.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = named.Close() }()
	namedInfo, err := named.Stat()
	if err != nil {
		return err
	}
	dev, ino, ok = streamedHandleIdentity(named, namedInfo)
	if !ok || dev != device || ino != inode || !namedInfo.IsDir() {
		return fmt.Errorf("filesystem directory %s diverged from the pinned root: %w", path, ErrTakeAsideForeign)
	}
	current, err := asideLstat(fs, path)
	if err != nil {
		return err
	}
	dev, ino, ok = BoundObjectIdentity(fs, path, current)
	if !ok || dev != device || ino != inode || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("directory namespace %s changed before bound removal: %w", path, ErrTakeAsideForeign)
	}
	children, err := directory.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := removeBoundDirectoryChild(root, child); err != nil {
			return err
		}
	}
	return nil
}
