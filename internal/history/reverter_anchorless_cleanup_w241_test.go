package history

import (
	"context"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P2 (PR #241 F1): copy/hardlink/symlink cleanup must NOT be
// anchor-gated. The move/update legs address the primary (move-back, or
// regenerating artifacts at OriginalPath), so a missing anchor correctly
// skips them; but non-move rows never gave up their primary — the installed
// destination is the user's own copy — and a user-deleted installed primary
// previously made checkAnchor return anchor_missing BEFORE the
// GeneratedFiles cleanup ran, orphaning every journaled copied
// subtitle/NFO/download. These tests pin the anchorless cleanup leg: a copy
// row whose installed primary is already gone still reverts, cleans the
// surviving generated artifacts, and retains the original source.

func TestRevertW241_AnchorlessCopyCleanupWithDeletedPrimary(t *testing.T) {
	opTypes := []models.OperationTypeEnum{
		models.OperationTypeCopy,
		models.OperationTypeHardlink,
		models.OperationTypeSymlink,
	}
	for _, opType := range opTypes {
		t.Run(string(opType), func(t *testing.T) {
			fs := afero.NewMemMapFs()
			repo := newP3OpRepo()
			ctx := context.Background()

			srcDir := "/src-anchorless/" + string(opType)
			dstDir := "/dst-anchorless/" + string(opType) + "/WCP-200"
			copiedVideo := dstDir + "/WCP-200.mkv"
			copiedSub := dstDir + "/WCP-200.srt"
			destNFO := dstDir + "/WCP-200.nfo"
			destPoster := dstDir + "/WCP-200-poster.jpg"
			require.NoError(t, fs.MkdirAll(dstDir, config.DirPerm))
			// The generated artifacts SURVIVE; the user deleted ONLY the
			// installed primary copy after organizing.
			require.NoError(t, afero.WriteFile(fs, copiedSub, []byte("dest-subtitle"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, destNFO, []byte("dest-nfo"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, destPoster, []byte("poster-bytes"), config.FilePerm))
			require.NoError(t, fs.MkdirAll(srcDir, config.DirPerm))
			require.NoError(t, afero.WriteFile(fs, srcDir+"/WCP-200.mkv", []byte("source-video"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, srcDir+"/WCP-200.srt", []byte("source-subtitle"), config.FilePerm))

			op := &models.BatchFileOperation{
				BatchJobID:    "job-w241-anchorless",
				MovieID:       "WCP-200",
				OriginalPath:  srcDir + "/WCP-200.mkv",
				NewPath:       copiedVideo, // deleted on the FS: the missing anchor
				OperationType: opType,
				GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
					Delete: []string{destNFO, destPoster, copiedSub},
				}),
				RevertStatus: models.RevertStatusApplied,
			}
			require.NoError(t, repo.Create(ctx, op))

			res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w241-anchorless")
			require.NoError(t, err)
			require.Equal(t, 1, res.Succeeded,
				"a deleted installed primary must not strand copy-mode generated artifacts behind anchor_missing")
			require.Equal(t, 0, res.Skipped)
			require.Equal(t, models.RevertOutcomeReverted, res.Outcomes[0].Outcome)
			require.Empty(t, res.Outcomes[0].Reason)

			for _, gone := range []string{copiedSub, destNFO, destPoster} {
				_, statErr := fs.Stat(gone)
				assert.True(t, os.IsNotExist(statErr), "journaled generated artifact %s is cleaned despite the missing primary anchor", gone)
			}
			srcVideo, err := afero.ReadFile(fs, srcDir+"/WCP-200.mkv")
			require.NoError(t, err)
			assert.Equal(t, []byte("source-video"), srcVideo, "original source retained byte-for-byte")
			srcSub, err := afero.ReadFile(fs, srcDir+"/WCP-200.srt")
			require.NoError(t, err)
			assert.Equal(t, []byte("source-subtitle"), srcSub, "the copied subtitle's source survives")

			row, err := repo.FindByID(ctx, op.ID)
			require.NoError(t, err)
			assert.Equal(t, models.RevertStatusReverted, row.RevertStatus,
				"correct DB status: reverted (not silently left applied)")
		})
	}
}

// The contrasting pin: anchor semantics still apply to the move/update legs.
// A move row whose installed primary is gone moves nothing back and skips
// anchor_missing; an update row whose ORIGINAL file is gone skips likewise
// instead of regenerating artifacts into a nonexistent tree.
func TestRevertW241_MoveAndUpdateLegsRemainAnchorGated(t *testing.T) {
	ctx := context.Background()

	t.Run("move", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		srcDir := "/src-anchor-move"
		dstDir := "/dst-anchor-move/lib/WCP-201"
		movedSub := dstDir + "/WCP-201.srt"
		require.NoError(t, fs.MkdirAll(dstDir, config.DirPerm))
		// The installed primary is gone; a journaled subtitle still sits in
		// the ledger to prove the skip precedes any cleanup for move rows.
		require.NoError(t, afero.WriteFile(fs, movedSub, []byte("moved-subtitle"), config.FilePerm))
		require.NoError(t, fs.MkdirAll(srcDir, config.DirPerm))

		op := &models.BatchFileOperation{
			BatchJobID:    "job-w241-anchor-move",
			MovieID:       "WCP-201",
			OriginalPath:  srcDir + "/WCP-201.mkv",
			NewPath:       dstDir + "/WCP-201.mkv", // deleted on the FS
			OperationType: models.OperationTypeMove,
			GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
				MoveBack: []models.FileMove{{OriginalPath: srcDir + "/WCP-201.srt", NewPath: movedSub}},
			}),
			RevertStatus: models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(ctx, op))

		res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w241-anchor-move")
		require.NoError(t, err)
		require.Equal(t, 1, res.Skipped, "missing installed primary still anchor-skips the move leg")
		require.Equal(t, 0, res.Succeeded)
		require.Equal(t, models.RevertReasonAnchorMissing, res.Outcomes[0].Reason)

		sub, err := afero.ReadFile(fs, movedSub)
		require.NoError(t, err)
		assert.Equal(t, []byte("moved-subtitle"), sub,
			"the move leg's ledger is untouched — nothing moved back, nothing deleted")
		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		assert.Equal(t, models.RevertStatusApplied, row.RevertStatus, "skipped rows stay retriable")
	})

	t.Run("update", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		srcDir := "/src-anchor-update"
		require.NoError(t, fs.MkdirAll(srcDir, config.DirPerm))
		// OriginalPath itself is gone: nothing in place to regenerate at.

		op := &models.BatchFileOperation{
			BatchJobID:    "job-w241-anchor-update",
			MovieID:       "WCP-202",
			OriginalPath:  srcDir + "/WCP-202.mkv", // deleted on the FS
			NewPath:       srcDir + "/WCP-202.mkv",
			OperationType: models.OperationTypeUpdate,
			RevertStatus:  models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(ctx, op))

		res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w241-anchor-update")
		require.NoError(t, err)
		require.Equal(t, 1, res.Skipped)
		require.Equal(t, models.RevertReasonAnchorMissing, res.Outcomes[0].Reason)

		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		assert.Equal(t, models.RevertStatusApplied, row.RevertStatus)
	})
}
