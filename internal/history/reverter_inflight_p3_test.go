package history

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// codex P3 round 1 (F2): in a batch revert, a NEWER operation whose own
// restore FAILED must still block older operations — the destination still
// holds the newer bytes and the older backup must not be restored over it.
func TestRevert_NewerFailedInBatch_BlocksOlderFromClimbing(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	// Shared destination across two operations: newer (seq 2) currently owns
	// the bytes; older (seq 1) holds a valid backup.
	dest := "/dst/CHAIN/poster.jpg"
	require.NoError(t, fs.MkdirAll("/dst/CHAIN", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("newer-bytes"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.b", []byte("older-bytes"), config.FilePerm))
	// The newer op's OWN backup is missing (sabotaged) — its restore fails.

	mkOp := func(movieID string, seq int64, backup string) *models.BatchFileOperation {
		newPath := "/dst/" + movieID + "/" + movieID + ".mkv"
		require.NoError(t, fs.MkdirAll("/dst/"+movieID, config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, newPath, []byte("video"), config.FilePerm))
		gf := models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
			{Destination: dest, Backup: backup, DestSeq: seq},
		}}
		raw, err := json.Marshal(gf)
		require.NoError(t, err)
		op := &models.BatchFileOperation{
			BatchJobID: "job-1", MovieID: movieID,
			OriginalPath: "/src/" + movieID + ".mkv", NewPath: newPath,
			OperationType: models.OperationTypeMove, GeneratedFiles: string(raw),
			RevertStatus: models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(ctx, op))
		return op
	}
	older := mkOp("CHN-OLD", 1, dest+".dlbak.b")
	newer := mkOp("CHN-NEW", 2, dest+".dlbak.missing")

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 2, res.Failed, "newer restore fails AND older must be refused")

	require.Equal(t, "newer-bytes", string(mustRead2(t, fs, dest)),
		"the destination still belongs to the failed newer op — the older backup must NOT climb over it")

	for _, op := range []*models.BatchFileOperation{older, newer} {
		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		require.Equal(t, models.RevertStatusApplied, row.RevertStatus)
		gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
		require.NoError(t, err)
		require.Len(t, gf.Replacements, 1, "journal entries intact for both rows")
	}
	// The older op's intact backup survives for a future ordered retry.
	require.Equal(t, "older-bytes", string(mustRead2(t, fs, dest+".dlbak.b")))
}
