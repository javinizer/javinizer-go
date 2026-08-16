package history

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// codex P3 R3-1: interleaved replacement chains (op A owns seqs 1+3, op B
// owns seq 2 on the same destination) must NOT unwind at operation
// granularity — restoring A would cross B's still-applied bytes. Safe answer:
// reject with an instruction and leave every row Applied.
func TestRevert_InterleavedChain_RejectedSafely(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/dst/INT/poster.jpg"
	require.NoError(t, fs.MkdirAll("/dst/INT", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("A-final"), config.FilePerm))
	// Backups on disk: seq1→original, seq2→post-A1, seq3→post-B2.
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.1", []byte("original"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.2", []byte("post-A1"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.3", []byte("post-B2"), config.FilePerm))

	mkOp := func(movieID string, seqs ...int64) *models.BatchFileOperation {
		newPath := "/dst/" + movieID + "/" + movieID + ".mkv"
		require.NoError(t, fs.MkdirAll("/dst/"+movieID, config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, newPath, []byte("video"), config.FilePerm))
		gf := models.GeneratedFilesJSON{}
		for _, s := range seqs {
			gf.Replacements = append(gf.Replacements, models.ReplacementEntry{
				Destination: dest, Backup: dest + ".dlbak." + []string{"zero", "1", "2", "3"}[s], DestSeq: s,
			})
		}
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
	opA := mkOp("INT-AAA", 1, 3)
	opB := mkOp("INT-BBB", 2)

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 2, res.Failed, "interleaved chains refuse op-granular unwind")

	require.Equal(t, "A-final", string(mustRead2(t, fs, dest)), "destination bytes untouched")
	for _, op := range []*models.BatchFileOperation{opA, opB} {
		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		require.Equal(t, models.RevertStatusApplied, row.RevertStatus)
	}
	for _, n := range []string{"1", "2", "3"} {
		exists, _ := afero.Exists(fs, dest+".dlbak."+n)
		require.True(t, exists, "backup %s retained for a later ordered resolution", n)
	}
}

// codex P3 R3-3: the Begin-seeded discovery ROOT alone must make a
// pre-journal crash-window backup findable — no replacements, no delete
// ledger rows exist yet at that point.
func TestSweep_RootSeededDiscovery_PreloadBackupRestored(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/ROOTED/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/ROOTED", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("pre-journal"), config.FilePerm))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(backup, old, old))

	raw, err := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/ROOTED"}})
	require.NoError(t, err)
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "ROOT-1", OriginalPath: "/src/root.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "pre-journal", string(mustRead2(t, fs, dest)),
		"root-seeded discovery restores the pre-journal crash-window backup")

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, "ROOT-1", row.MovieID)
}
