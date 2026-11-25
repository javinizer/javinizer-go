package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// cleanupRootNoAccess deliberately has no OS or memory filesystem behind it.
// The root is a lexical input only; this test cannot access a real volume.
type cleanupRootNoAccess struct {
	afero.Fs
	t *testing.T
}

func (f cleanupRootNoAccess) Open(string) (afero.File, error) {
	f.t.Fatal("Undo cleanup attempted to read the filesystem root")
	return nil, os.ErrPermission
}

func (f cleanupRootNoAccess) Remove(string) error {
	f.t.Fatal("Undo cleanup attempted to remove the filesystem root")
	return os.ErrPermission
}

func TestPrimaryUndoWindowsCleanupRootRequiresNoFilesystemAccess(t *testing.T) {
	root := "X:" + string(filepath.Separator)
	if path := filepath.Clean(root); filepath.Dir(path) != path {
		t.Fatalf("fixture is not a native root: %q", path)
	}
	cleanupEmptyDirFS(cleanupRootNoAccess{t: t}, root, "")
}
