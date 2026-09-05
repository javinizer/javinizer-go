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

// codex P2 (PR #241 F2) end-to-end pins for the reverter's operation-type
// legs. Production journals a copy-installed subtitle into the Delete list
// (workflow/revert_log.go: SubtitleMove.Copied → Delete, pinned at the
// journaling side by workflow's
// TestW9CompleteJournalsCopiedSubtitlesAsDelete) — and production only sets
// Copied when cmd.MoveFiles is false, so the row carrying that entry is a
// copy/hardlink/symlink op. The reverter's op-type gate used to reject
// exactly those rows BEFORE cleanupGeneratedFiles — the only consumer of
// Delete entries — leaving every copied subtitle unreachable. These tests
// run a full RevertBatch per mode: copy/link ops consume the Delete entry
// while retaining every source AND the installed primary (no move-back is
// ever attempted), and the move op keeps its MoveBack semantics untouched.

func TestRevertW241_CopyModeCopiedSubtitleRevertEndToEnd(t *testing.T) {
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

			srcDir := "/src/" + string(opType)
			dstDir := "/dst/" + string(opType) + "/WCP-100"
			copiedVideo := dstDir + "/WCP-100.mkv"
			copiedSub := dstDir + "/WCP-100.srt"
			destNFO := dstDir + "/WCP-100.nfo"
			require.NoError(t, fs.MkdirAll(dstDir, config.DirPerm))
			require.NoError(t, afero.WriteFile(fs, copiedVideo, []byte("video-bytes"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, copiedSub, []byte("dest-subtitle"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, destNFO, []byte("dest-nfo"), config.FilePerm))
			require.NoError(t, fs.MkdirAll(srcDir, config.DirPerm))
			require.NoError(t, afero.WriteFile(fs, srcDir+"/WCP-100.mkv", []byte("source-video"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, srcDir+"/WCP-100.srt", []byte("source-subtitle"), config.FilePerm))

			// The exact journal a non-move-mode Complete persists: the
			// copied subtitle and the NFO land in Delete; the op never
			// carries MoveBack because nothing ever left the source tree.
			op := &models.BatchFileOperation{
				BatchJobID:    "job-w241-e2e",
				MovieID:       "WCP-100",
				OriginalPath:  srcDir + "/WCP-100.mkv",
				NewPath:       copiedVideo,
				OperationType: opType,
				GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
					Delete: []string{destNFO, copiedSub},
				}),
				RevertStatus: models.RevertStatusApplied,
			}
			require.NoError(t, repo.Create(ctx, op))

			res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w241-e2e")
			require.NoError(t, err)
			require.Equal(t, 1, res.Succeeded, "copy/link ops revert via the non-destructive cleanup leg")
			require.Equal(t, models.RevertOutcomeReverted, res.Outcomes[0].Outcome)

			for _, gone := range []string{copiedSub, destNFO} {
				_, statErr := fs.Stat(gone)
				assert.True(t, os.IsNotExist(statErr), "journaled generated artifact %s is deleted", gone)
			}
			srcVideo, err := afero.ReadFile(fs, srcDir+"/WCP-100.mkv")
			require.NoError(t, err)
			assert.Equal(t, []byte("source-video"), srcVideo, "source retained byte-for-byte")
			srcSub, err := afero.ReadFile(fs, srcDir+"/WCP-100.srt")
			require.NoError(t, err)
			assert.Equal(t, []byte("source-subtitle"), srcSub, "the copied subtitle's source survives")
			// No move-back was attempted: the installed primary is still the
			// user's copy at the destination, and the source tree gained no
			// entries. Had the move leg ever run for this row it would have
			// failed closed as a destination conflict (OriginalPath still
			// exists in copy mode) — the Reverted outcome above proves the
			// move-back leg never executed.
			installed, err := afero.ReadFile(fs, copiedVideo)
			require.NoError(t, err)
			assert.Equal(t, []byte("video-bytes"), installed,
				"the installed primary copy is retained — copy-mode revert never moves back or unlinks it")
			srcEntries, err := afero.ReadDir(fs, srcDir)
			require.NoError(t, err)
			assert.Len(t, srcEntries, 2, "source tree unchanged — no move-back artifacts")

			row, err := repo.FindByID(ctx, op.ID)
			require.NoError(t, err)
			assert.Equal(t, models.RevertStatusReverted, row.RevertStatus)
		})
	}
}

// The move-mode counterpart: MOVE-installed subtitles keep full MoveBack
// semantics — the subtitle returns to its pre-apply source path alongside
// the primary, and the destination artifacts are gone.
func TestRevertW241_MoveModeSubtitleMoveBackSemanticsPreserved(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	srcDir := "/src-move"
	dstDir := "/dst-move/lib/MVB-100"
	movedVideo := dstDir + "/MVB-100.mkv"
	movedSub := dstDir + "/MVB-100.srt"
	origSub := srcDir + "/weekend-raw.srt"
	require.NoError(t, fs.MkdirAll(dstDir, config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, movedVideo, []byte("video-bytes"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, movedSub, []byte("moved-subtitle"), config.FilePerm))
	// The source directory survives the forward move (as on a real volume)
	// while both files themselves moved out.
	require.NoError(t, fs.MkdirAll(srcDir, config.DirPerm))

	op := &models.BatchFileOperation{
		BatchJobID:    "job-w241-move",
		MovieID:       "MVB-100",
		OriginalPath:  srcDir + "/MVB-100.mkv",
		NewPath:       movedVideo,
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			MoveBack: []models.FileMove{{OriginalPath: origSub, NewPath: movedSub}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w241-move")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)
	require.Equal(t, models.RevertOutcomeReverted, res.Outcomes[0].Outcome)

	video, err := afero.ReadFile(fs, srcDir+"/MVB-100.mkv")
	require.NoError(t, err)
	assert.Equal(t, []byte("video-bytes"), video, "the primary moved back to its original path")
	sub, err := afero.ReadFile(fs, origSub)
	require.NoError(t, err)
	assert.Equal(t, []byte("moved-subtitle"), sub, "the MoveBack entry is honored")
	for _, gone := range []string{movedVideo, movedSub} {
		_, statErr := fs.Stat(gone)
		assert.True(t, os.IsNotExist(statErr), "destination artifact %s is gone after move-back", gone)
	}

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RevertStatusReverted, row.RevertStatus)
}
