package history

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type independentDirectorySuccessorFS struct {
	afero.Fs
	target  string
	saved   string
	swapped bool
}

func (f *independentDirectorySuccessorFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(p)
}
func (f *independentDirectorySuccessorFS) Rename(a, b string) error {
	if e := f.Fs.Rename(a, b); e != nil {
		return e
	}
	if b == f.target && !f.swapped {
		f.swapped = true
		if e := f.Fs.Rename(b, f.saved); e != nil {
			return e
		}
		if e := f.Fs.MkdirAll(b, 0755); e != nil {
			return e
		}
		return afero.WriteFile(f.Fs, filepath.Join(b, "poster.jpg"), []byte("foreign poster"), 0644)
	}
	return nil
}
func TestIndependentDirectoryUndoPublishSuccessor(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "incoming")
	cur := filepath.Join(root, "ABC-123")
	base := afero.NewOsFs()
	require.NoError(t, base.Mkdir(cur, 0755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(cur, "poster.jpg"), []byte("owned poster"), 0644))
	info, e := base.Stat(cur)
	require.NoError(t, e)
	dev, ino, ok := fsutil.BoundObjectIdentity(base, cur, info)
	require.True(t, ok)
	fs := &independentDirectorySuccessorFS{Fs: base, target: old, saved: old + ".saved"}
	db, e := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "db.sqlite"), LogLevel: "error"})
	require.NoError(t, e)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := database.NewBatchFileOperationRepository(db)
	move := &models.DirectoryMove{Original: old, Locations: []string{cur}, Device: dev, Inode: ino}
	op := &models.BatchFileOperation{BatchJobID: "directory-successor", MovieID: "ABC-123", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, OriginalPath: filepath.Join(old, "abc-123.mp4"), OriginalDirPath: old, InPlaceRenamed: true, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: move, Delete: []string{filepath.Join(cur, "poster.jpg")}})}
	require.NoError(t, repo.Create(context.Background(), op))
	result, e := NewReverter(fs, repo).RevertBatch(context.Background(), op.BatchJobID)
	require.NoError(t, e)
	require.True(t, fs.swapped, "injected post-publish successor must run")
	t.Logf("Undo %+v", result)
	body, e := afero.ReadFile(base, filepath.Join(old, "poster.jpg"))
	require.NoError(t, e, "Undo must not delete foreign successor poster")
	require.Equal(t, "foreign poster", string(body))
	require.Zero(t, result.Succeeded)
}
