package history

import (
	"context"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type independentUndoCrashFS struct {
	afero.Fs
	current, original     string
	afterRestore, tripped bool
}

func (f *independentUndoCrashFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(p)
}
func (f *independentUndoCrashFS) Rename(a, b string) error {
	e := f.Fs.Rename(a, b)
	if e == nil && !f.tripped && ((!f.afterRestore && a == f.current) || (f.afterRestore && b == f.original)) {
		f.tripped = true
		panic("synthetic interrupted Undo directory rename")
	}
	return e
}
func TestIndependentResidentInterruptedDirectoryUndo(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			root := t.TempDir()
			old := filepath.Join(root, "incoming")
			cur := filepath.Join(root, "ABC-123")
			base := afero.NewOsFs()
			require.NoError(t, base.Mkdir(cur, 0755))
			require.NoError(t, afero.WriteFile(base, filepath.Join(cur, "poster.jpg"), []byte("owned poster"), 0644))
			require.NoError(t, afero.WriteFile(base, filepath.Join(cur, "ABC-123.mp4"), []byte("resident"), 0644))
			primary, e := statResidentTestIdentity(t, base, filepath.Join(cur, "ABC-123.mp4"))
			require.NoError(t, e)
			info, e := statResidentTestIdentity(t, base, cur)
			require.NoError(t, e)
			dev, ino, ok := fsutil.BoundObjectIdentity(base, cur, info)
			require.True(t, ok)
			fs := &independentUndoCrashFS{Fs: base, current: cur, original: old, afterRestore: after}
			dsn := filepath.Join(root, "db.sqlite")
			open := func() *database.DB {
				db, e := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
				require.NoError(t, e)
				require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
				return db
			}
			db := open()
			repo := database.NewBatchFileOperationRepository(db)
			move := &models.DirectoryMove{Original: old, Locations: []string{cur}, Device: dev, Inode: ino}
			op := &models.BatchFileOperation{BatchJobID: "undo-crash", MovieID: "ABC-123", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, OriginalPath: filepath.Join(old, "abc-123-source.mp4"), OriginalDirPath: old, InPlaceRenamed: true, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: move, Delete: []string{filepath.Join(cur, "poster.jpg")}})}
			require.NoError(t, repo.Create(context.Background(), op))
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Logf("crash %v", p)
					}
				}()
				_, e = NewReverter(fs, repo).RevertBatch(context.Background(), op.BatchJobID)
			}()
			require.True(t, fs.tripped)
			require.NoError(t, db.Close())
			db = open()
			defer db.Close()
			repo = database.NewBatchFileOperationRepository(db)
			result, e := NewReverter(base, repo).RevertBatch(context.Background(), op.BatchJobID)
			require.NoError(t, e)
			require.Equal(t, 1, result.Succeeded)
			require.Zero(t, result.Failed)
			now, e := statResidentTestIdentity(t, base, old)
			require.NoError(t, e)
			require.True(t, sameResidentTestIdentity(info, now))
			now, e = statResidentTestIdentity(t, base, filepath.Join(old, "ABC-123.mp4"))
			require.NoError(t, e)
			require.True(t, sameResidentTestIdentity(primary, now))
			_, e = statResidentTestIdentity(t, base, filepath.Join(old, "abc-123-source.mp4"))
			require.True(t, os.IsNotExist(e))
			_, e = statResidentTestIdentity(t, base, filepath.Join(old, "poster.jpg"))
			require.True(t, os.IsNotExist(e))
		})
	}
}
