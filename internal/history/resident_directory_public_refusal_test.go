package history

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Public Undo must report a directory-location refusal before touching either
// the admitted directory or an unrelated object occupying its original name.
func TestResidentDirectoryPublicUndoRefusesOccupiedOriginal(t *testing.T) {
	root := t.TempDir()
	original, current := filepath.Join(root, "original"), filepath.Join(root, "current")
	fs := afero.NewOsFs()
	require.NoError(t, fs.Mkdir(current, 0755))
	require.NoError(t, fs.Mkdir(original, 0755))
	owned := filepath.Join(current, "video.mp4")
	foreign := filepath.Join(original, "foreign.txt")
	require.NoError(t, afero.WriteFile(fs, owned, []byte("owned video"), 0600))
	require.NoError(t, afero.WriteFile(fs, foreign, []byte("foreign content"), 0600))
	admitted, err := fs.Stat(current)
	require.NoError(t, err)
	dev, ino, strong := fsutil.BoundObjectIdentity(fs, current, admitted)
	require.True(t, strong)
	foreignInfo, err := fs.Stat(original)
	require.NoError(t, err)
	foreignDev, foreignIno, foreignStrong := fsutil.BoundObjectIdentity(fs, original, foreignInfo)
	require.True(t, foreignStrong)
	dbPath := filepath.Join(root, "history.sqlite")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repo := database.NewBatchFileOperationRepository(db)
	ledger := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		PrimaryResident: true,
		DirectoryMove:   &models.DirectoryMove{Original: original, Locations: []string{current}, Device: dev, Inode: ino},
	})
	op := &models.BatchFileOperation{BatchJobID: "occupied-directory", MovieID: "GOOD-701", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, OriginalPath: filepath.Join(original, "video.mp4"), NewPath: owned, OriginalDirPath: original, InPlaceRenamed: true, GeneratedFiles: ledger}
	require.NoError(t, repo.Create(t.Context(), op))
	result, err := NewReverter(fs, repo).RevertBatch(t.Context(), op.BatchJobID)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, 1, result.Failed)
	require.Zero(t, result.Succeeded)
	require.Zero(t, result.Skipped)
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, op.ID, result.Outcomes[0].OperationID)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcomes[0].Outcome)
	require.Equal(t, models.RevertReasonDestinationConflict, result.Outcomes[0].Reason)
	require.Contains(t, result.Outcomes[0].Error, "directory recovery destination conflict")
	require.Contains(t, result.Outcomes[0].Error, original)
	// Refusal and the untouched inverse must survive a fresh database handle.
	require.NoError(t, db.Close())
	db, err = database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "error"})
	require.NoError(t, err)
	repo = database.NewBatchFileOperationRepository(db)
	stored, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusFailed, stored.RevertStatus)
	requireOnlyUndoAdmissionChanged(t, ledger, stored.GeneratedFiles)
	after, err := fs.Stat(current)
	require.NoError(t, err)
	afterDev, afterIno, afterStrong := fsutil.BoundObjectIdentity(fs, current, after)
	require.True(t, afterStrong)
	require.Equal(t, dev, afterDev)
	require.Equal(t, ino, afterIno)
	foreignAfter, err := fs.Stat(original)
	require.NoError(t, err)
	foreignAfterDev, foreignAfterIno, foreignAfterStrong := fsutil.BoundObjectIdentity(fs, original, foreignAfter)
	require.True(t, foreignAfterStrong)
	require.Equal(t, foreignDev, foreignAfterDev)
	require.Equal(t, foreignIno, foreignAfterIno)
	content, err := afero.ReadFile(fs, owned)
	require.NoError(t, err)
	require.Equal(t, "owned video", string(content))
	content, err = afero.ReadFile(fs, foreign)
	require.NoError(t, err)
	require.Equal(t, "foreign content", string(content))
}
