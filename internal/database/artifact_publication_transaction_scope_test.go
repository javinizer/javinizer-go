package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestApplyArtifactPublicationFenceAllowsDurableJournalWrites(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "journal-fence", 1, true)
	op := models.BatchFileOperation{BatchJobID: "fence-op", MovieID: "journal-fence", OperationType: models.OperationTypeCopy}
	require.NoError(t, db.Create(&op).Error)
	repo := NewBatchFileOperationRepository(db)

	err := artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(t.Context(), "journal-fence", 1, func(*models.Movie) error {
		return repo.UpdateJournalInTx(t.Context(), op.ID, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			return models.GeneratedFilesJSON{Delete: []string{"created"}}, true, nil
		})
	})
	require.NoError(t, err)

	var got models.BatchFileOperation
	require.NoError(t, db.First(&got, op.ID).Error)
	ledger, err := models.ParseGeneratedFiles(got.GeneratedFiles)
	require.NoError(t, err)
	require.Equal(t, []string{"created"}, ledger.Delete)
}
