package organizer

import (
	"os"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
)

// linker abstracts file materialization operations that afero.Fs cannot perform
// (hard links, symbolic links). The default OSLinker wraps os.Link/os.Symlink;
// memLinker provides in-memory simulation for tests.
type linker interface {
	// symlink creates a symbolic link from oldname to newname.
	symlink(oldname, newname string) error
	// hardlink creates a hard link from oldname to newname.
	hardlink(oldname, newname string) error
	// copyFile copies the file content from src to dst using the provided fs.
	copyFile(fs afero.Fs, src, dst string) error
}

// OSLinker wraps real OS link operations for production use.
type OSLinker struct{}

func (OSLinker) symlink(oldname, newname string) error  { return os.Symlink(oldname, newname) }
func (OSLinker) hardlink(oldname, newname string) error { return os.Link(oldname, newname) }
func (OSLinker) copyFile(fs afero.Fs, src, dst string) error {
	return fsutil.CopyFileFs(fs, src, dst)
}

// linkRecord records a link operation for test assertions.
type linkRecord struct {
	Kind    string // "hard" or "soft"
	OldName string
	NewName string
}

// MemLinker provides in-memory link simulation for testing.
type MemLinker struct {
	Links []linkRecord
}

//nolint:unparam // implements linker interface; always returns nil for test mock
func (m *MemLinker) symlink(oldname, newname string) error {
	m.Links = append(m.Links, linkRecord{Kind: "soft", OldName: oldname, NewName: newname})
	return nil
}

//nolint:unparam // implements linker interface; always returns nil for test mock
func (m *MemLinker) hardlink(oldname, newname string) error {
	m.Links = append(m.Links, linkRecord{Kind: "hard", OldName: oldname, NewName: newname})
	return nil
}

func (m *MemLinker) copyFile(fs afero.Fs, src, dst string) error {
	return fsutil.CopyFileFs(fs, src, dst)
}
