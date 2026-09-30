package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// openVanishFs models a planned-delete destination that vanishes between the
// no-follow occupancy probe and the read-back: LstatIfPossible still reports
// the pinned REGULAR file (a stale probe raced ahead of an unlink) while Open
// answers ENOENT for exactly that path. Like statDenyFS, only the named
// path's legs are intercepted; everything else delegates to the wrapped fs.
// A nil info lets the base fs answer the no-follow lookup itself.
type openVanishFs struct {
	afero.Fs
	path string
	info os.FileInfo
}

func (f *openVanishFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Clean(name) == filepath.Clean(f.path) && f.info != nil {
		return f.info, true, nil
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *openVanishFs) Open(name string) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.path) {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	return f.Fs.Open(name)
}

// A planned delete whose destination disappears between the lstat probe and
// the open is consumed exactly like an absent path (reverter.go: the Open
// ENOENT leg marks dirsToCheck and continues): the entry never reaches the
// hash-and-remove arm — no removal fires even though the pin WOULD have
// matched the reported bytes — and the now-empty parent directory is pruned
// as with any consumed delete. The prune is the discriminative observable:
// the lstat succeeded, so only this leg could have marked the directory (the
// generic open-error leg beneath it deliberately does not).
func TestCleanupGeneratedFilesFS_PlannedDeleteOpenVanishes(t *testing.T) {
	const dst = "/dst-w161h/lib/W161H-001-tray.jpg"
	newOp := func() *models.BatchFileOperation {
		return &models.BatchFileOperation{
			OperationType:  models.OperationTypeMove,
			GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PlannedDeletes: []models.DeleteEntry{{Path: dst, SHA256: sha256HexOf([]byte("published"))}}}),
		}
	}

	t.Run("stale probe of a vanished file consumes the entry and prunes the empty parent", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o777))
		require.NoError(t, afero.WriteFile(base, "/probe-template", []byte("x"), 0o666))
		templateInfo, err := base.Stat("/probe-template")
		require.NoError(t, err)
		fs := &openVanishFs{Fs: base, path: dst, info: templateInfo}
		cleanupGeneratedFilesFS(fs, newOp(), "/dst-w161h")
		if _, err := base.Stat(filepath.Dir(dst)); !os.IsNotExist(err) {
			t.Fatalf("the consumed entry's empty parent was pruned: %v", err)
		}
		info, err := base.Stat("/dst-w161h")
		require.NoError(t, err, "the stopAt boundary itself is retained")
		assert.True(t, info.IsDir())
	})

	t.Run("pinned bytes under a failing open are never removed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o777))
		require.NoError(t, afero.WriteFile(base, dst, []byte("published"), 0o666))
		fs := &openVanishFs{Fs: base, path: dst}
		cleanupGeneratedFilesFS(fs, newOp(), "/dst-w161h")
		got, err := afero.ReadFile(base, dst)
		require.NoError(t, err)
		assert.Equal(t, "published", string(got),
			"the open-vanish leg consumed the entry before the hash-and-remove arm — a matching pin alone never deletes")
		info, err := base.Stat(filepath.Dir(dst))
		require.NoError(t, err, "the still-occupied parent is never pruned")
		assert.True(t, info.IsDir())
	})
}
