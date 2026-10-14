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

func TestIndependentResidentCleanupKeepsEmptyRootAndOutsideParents(t *testing.T) {
	for _, moved := range []bool{false, true} {
		for _, untracked := range []bool{false, true} {
			t.Run(fmt.Sprintf("moved=%v/untracked=%v", moved, untracked), func(t *testing.T) {
				fs := afero.NewOsFs()
				temp := t.TempDir()
				original := filepath.Join(temp, "resident")
				current := original
				if moved {
					current = filepath.Join(temp, "renamed")
				}
				require.NoError(t, fs.Mkdir(original, 0755))
				before, e := statResidentTestIdentity(t, fs, original)
				require.NoError(t, e)
				dev, ino, strong := fsutil.BoundObjectIdentity(fs, original, before)
				require.True(t, strong)
				if moved {
					require.NoError(t, fs.Rename(original, current))
				}
				owned := filepath.Join(current, "generated", "deep", "image.jpg")
				require.NoError(t, fs.MkdirAll(filepath.Dir(owned), 0755))
				require.NoError(t, afero.WriteFile(fs, owned, []byte("owned"), 0644))
				if untracked {
					require.NoError(t, fs.Mkdir(filepath.Join(current, "untracked-empty"), 0755))
				}
				// A separately journaled file may be removed, but its outside parent chain
				// must not be pruned using a lexical prefix of the resident root.
				outside := original + "-other"
				outsideFile := filepath.Join(outside, "deep", "owned.jpg")
				require.NoError(t, fs.MkdirAll(filepath.Dir(outsideFile), 0755))
				require.NoError(t, afero.WriteFile(fs, outsideFile, []byte("separately owned"), 0644))
				ledger := models.GeneratedFilesJSON{PrimaryResident: true, Delete: []string{owned, outsideFile}}
				if moved {
					ledger.DirectoryMove = &models.DirectoryMove{Original: original, Locations: []string{current}, Device: dev, Inode: ino}
				}
				dsn := filepath.Join(temp, "db.sqlite")
				db, e := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
				require.NoError(t, e)
				require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
				repo := database.NewBatchFileOperationRepository(db)
				op := &models.BatchFileOperation{BatchJobID: "empty-resident-root", MovieID: "ABC-123", OriginalPath: filepath.Join(original, "missing-primary.mp4"), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, InPlaceRenamed: moved, OriginalDirPath: original, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
				require.NoError(t, repo.Create(context.Background(), op))
				require.NoError(t, db.Close())
				db, e = database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
				require.NoError(t, e)
				defer db.Close()
				repo = database.NewBatchFileOperationRepository(db)
				undo, e := NewReverter(fs, repo).RevertBatch(context.Background(), op.BatchJobID)
				require.NoError(t, e)
				require.Equal(t, 1, undo.Succeeded)
				require.Zero(t, undo.Failed)
				after, e := statResidentTestIdentity(t, fs, original)
				require.NoError(t, e, "resident directory must survive even when now empty")
				require.True(t, sameResidentTestIdentity(before, after))
				_, e = fs.Stat(filepath.Join(original, "generated"))
				require.True(t, os.IsNotExist(e))
				_, e = fs.Stat(filepath.Join(original, "missing-primary.mp4"))
				require.True(t, os.IsNotExist(e))
				_, e = fs.Stat(outsideFile)
				require.True(t, os.IsNotExist(e))
				require.DirExists(t, filepath.Dir(outsideFile), "outside parent chain is outside cleanup authority")
				if untracked {
					require.DirExists(t, filepath.Join(original, "untracked-empty"))
				} else {
					entries, e := afero.ReadDir(fs, original)
					require.NoError(t, e)
					require.Empty(t, entries)
				}
			})
		}
	}
}
