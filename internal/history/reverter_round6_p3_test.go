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

// codex P3 R6-1: per-destination sequences are not comparable ACROSS
// destinations — an op keyed high only on cover (seq 10) sorts ahead of an
// op with poster seq 2 and gets dependency-rejected by the newer poster
// entry; the run must retry it after the blocker is consumed.
func TestRevert_DependencyBlockedRetriesAfterBlockerConsumed(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	poster := "/dst/BLK/poster.jpg"
	cover := "/dst/BLK/cover.jpg"
	require.NoError(t, fs.MkdirAll("/dst/BLK", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, poster, []byte("poster-newest"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, cover, []byte("cover-newest"), config.FilePerm))
	// Older's backups; newer replaced only the poster.
	require.NoError(t, afero.WriteFile(fs, poster+".dlbak.1", []byte("poster-old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, poster+".dlbak.2", []byte("poster-mid"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, cover+".dlbak.10", []byte("cover-old"), config.FilePerm))

	mkOp := func(movieID string, entries []models.ReplacementEntry) *models.BatchFileOperation {
		newPath := "/dst/" + movieID + "/" + movieID + ".mkv"
		require.NoError(t, fs.MkdirAll("/dst/"+movieID, config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, newPath, []byte("video"), config.FilePerm))
		raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: entries})
		require.NoError(t, err)
		op := &models.BatchFileOperation{
			BatchJobID: "job-1", MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv", NewPath: newPath,
			OperationType: models.OperationTypeMove, GeneratedFiles: string(raw),
			RevertStatus: models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(ctx, op))
		return op
	}
	older := mkOp("BLK-OLD", []models.ReplacementEntry{
		{Destination: poster, Backup: poster + ".dlbak.1", DestSeq: 1},
		{Destination: cover, Backup: cover + ".dlbak.10", DestSeq: 10},
	})
	newer := mkOp("BLK-NEW", []models.ReplacementEntry{
		{Destination: poster, Backup: poster + ".dlbak.2", DestSeq: 2},
	})

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 2, res.Succeeded,
		"dependency-rejected op retried after its blocker's journal was consumed")
	require.Equal(t, "poster-old", string(mustRead2(t, fs, poster)),
		"full unwind: poster ends at the oldest bytes")
	require.Equal(t, "cover-old", string(mustRead2(t, fs, cover)))
	for _, op := range []*models.BatchFileOperation{older, newer} {
		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	}
}

// codex P3 R6-2: a deleted primary anchor must not strand the replacement
// journal — media still restores (copy-mode hasthe only recovery path).
func TestRevert_AnchorMissing_StillRestoresReplacementJournal(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	f := &p3Fixture{fs: fs, repo: repo}
	op, dest := f.addAppliedOp(t, "job-1", "ANC-001", false, "new-poster", p3Replacement{seq: 1, backupBytes: "original-poster"})
	// Sabotage: the video (anchor) was deleted out-of-band.
	require.NoError(t, fs.Remove(op.NewPath))

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Skipped, "anchor-missing still skips the row-level revert")
	require.Equal(t, "original-poster", string(mustRead2(t, fs, dest)),
		"the media journal restored anyway — never stranded by the anchor")
	exists, _ := afero.Exists(fs, dest+".dlbak.a")
	require.False(t, exists, "backup consumed")
}
