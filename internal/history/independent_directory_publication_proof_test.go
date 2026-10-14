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

type independentDirectoryCleanupBlockFS struct {
	afero.Fs
	remove, rename string
	blocked        bool
}

func (f *independentDirectoryCleanupBlockFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(p)
}
func (f *independentDirectoryCleanupBlockFS) Remove(p string) error {
	if filepath.Clean(p) == f.remove {
		f.blocked = true
		return os.ErrPermission
	}
	return f.Fs.Remove(p)
}
func (f *independentDirectoryCleanupBlockFS) Rename(a, b string) error {
	if filepath.Clean(b) == f.rename || filepath.Clean(a) == f.remove {
		f.blocked = true
		return os.ErrPermission
	}
	return f.Fs.Rename(a, b)
}
func TestIndependentDirectoryPublicationProofRecovery(t *testing.T) {
	for _, kind := range []string{"delete", "move"} {
		for _, mutation := range []string{"owned-blocked", "foreign", "symlink"} {
			t.Run(kind+"/"+mutation, func(t *testing.T) {
				root := t.TempDir()
				base := afero.NewOsFs()
				old := filepath.Join(root, "incoming")
				cur := filepath.Join(root, "ABC-123")
				require.NoError(t, base.Mkdir(cur, 0755))
				published := filepath.Join(cur, "published.srt")
				require.NoError(t, afero.WriteFile(base, published, []byte("owned publication"), 0644))
				pi, e := statResidentTestIdentity(t, base, published)
				require.NoError(t, e)
				dev, ino, ok := fsutil.BoundObjectIdentity(base, published, pi)
				require.True(t, ok)
				di, e := statResidentTestIdentity(t, base, cur)
				require.NoError(t, e)
				ddev, dino, ok := fsutil.BoundObjectIdentity(base, cur, di)
				require.True(t, ok)
				mode := uint32(pi.Mode().Perm())
				proof := models.DeleteEntry{IdentityMode: &mode, Path: published, RecoveryPublication: true, IdentityStrong: true, IdentityPinned: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: pi.Size(), IdentityModUnix: pi.ModTime().Unix()}
				ledger := models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: &models.DirectoryMove{Original: old, Locations: []string{cur}, Device: ddev, Inode: dino}}
				if kind == "delete" {
					ledger.PlannedDeletes = []models.DeleteEntry{proof}
				} else {
					ledger.MoveBack = []models.FileMove{{OriginalPath: filepath.Join(old, "original.srt"), NewPath: published, Proof: &proof}}
				}
				var foreign os.FileInfo
				external := filepath.Join(root, "external.srt")
				if mutation != "owned-blocked" {
					require.NoError(t, base.Rename(published, published+".saved"))
					if mutation == "foreign" {
						require.NoError(t, afero.WriteFile(base, published, []byte("foreign publication"), 0644))
						foreign, e = statResidentTestIdentity(t, base, published)
						require.NoError(t, e)
					} else {
						require.NoError(t, afero.WriteFile(base, external, []byte("external"), 0644))
						if e := os.Symlink(external, published); e != nil {
							t.Skipf("symlink unavailable: %v", e)
						}
					}
				}
				dsn := filepath.Join(root, "ledger.sqlite")
				open := func() *database.DB {
					db, e := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
					require.NoError(t, e)
					require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
					return db
				}
				db := open()
				repo := database.NewBatchFileOperationRepository(db)
				op := &models.BatchFileOperation{BatchJobID: "directory-proof", MovieID: "ABC-123", OriginalPath: filepath.Join(old, "resident.mp4"), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusFailed, InPlaceRenamed: true, OriginalDirPath: old, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
				require.NoError(t, repo.Create(context.Background(), op))
				block := &independentDirectoryCleanupBlockFS{Fs: base}
				if mutation == "owned-blocked" {
					if kind == "delete" {
						block.remove = filepath.Join(old, "published.srt")
					} else {
						block.rename = filepath.Join(old, "original.srt")
					}
				}
				result, e := NewReverter(block, repo).RevertBatch(context.Background(), op.BatchJobID)
				require.NoError(t, e)
				t.Logf("initial Undo %+v", result)
				restored, e := statResidentTestIdentity(t, base, old)
				require.NoError(t, e)
				require.True(t, sameResidentTestIdentity(di, restored))
				nowPath := filepath.Join(old, "published.srt")
				if mutation == "owned-blocked" {
					require.True(t, block.blocked, "injected cleanup refusal must execute after directory return")
					require.Zero(t, result.Succeeded)
					require.Equal(t, 1, result.Failed)
					now, e := statResidentTestIdentity(t, base, nowPath)
					require.NoError(t, e)
					require.True(t, sameResidentTestIdentity(pi, now))
					row, e := repo.FindByID(context.Background(), op.ID)
					require.NoError(t, e)
					saved, e := models.ParseGeneratedFiles(row.GeneratedFiles)
					require.NoError(t, e)
					if kind == "delete" {
						require.Len(t, saved.PlannedDeletes, 1)
						require.True(t, saved.PlannedDeletes[0].RecoveryPublication)
					} else {
						require.Len(t, saved.MoveBack, 1)
						require.NotNil(t, saved.MoveBack[0].Proof)
						require.True(t, saved.MoveBack[0].Proof.RecoveryPublication)
					}
					require.NoError(t, db.Close())
					db = open()
					defer db.Close()
					repo = database.NewBatchFileOperationRepository(db)
					retry, e := NewReverter(base, repo).RevertBatch(context.Background(), op.BatchJobID)
					require.NoError(t, e)
					t.Logf("retry %+v", retry)
					require.Equal(t, 1, retry.Succeeded)
					require.Zero(t, retry.Failed)
					_, e = statResidentTestIdentity(t, base, nowPath)
					require.True(t, os.IsNotExist(e))
					if kind == "move" {
						now, e := statResidentTestIdentity(t, base, filepath.Join(old, "original.srt"))
						require.NoError(t, e)
						require.True(t, sameResidentTestIdentity(pi, now))
					}
				} else {
					defer db.Close()
					require.Equal(t, 1, result.Succeeded)
					require.Zero(t, result.Failed)
					if mutation == "foreign" {
						now, e := statResidentTestIdentity(t, base, nowPath)
						require.NoError(t, e)
						require.True(t, sameResidentTestIdentity(foreign, now))
						body, e := afero.ReadFile(base, nowPath)
						require.NoError(t, e)
						require.Equal(t, "foreign publication", string(body))
					} else {
						link, e := os.Readlink(nowPath)
						require.NoError(t, e)
						require.Equal(t, external, link)
						body, e := afero.ReadFile(base, external)
						require.NoError(t, e)
						require.Equal(t, "external", string(body))
					}
					_, e = statResidentTestIdentity(t, base, filepath.Join(old, "original.srt"))
					require.True(t, os.IsNotExist(e), fmt.Sprint("foreign successor must not be moved", e))
				}
			})
		}
	}
}
