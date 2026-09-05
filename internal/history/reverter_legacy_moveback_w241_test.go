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

// codex P1 (PR #241) regression pins: rows journaled BEFORE the #224 phase-E
// mode distinction recorded EVERY successfully installed subtitle under
// MoveBack — including copy-installed ones whose originals never left the
// source tree. This branch made copy/hardlink/symlink rows reach
// cleanupGeneratedFiles for the first time, so those legacy entries would now
// run Rename(installedCopy, retainedOriginal): on POSIX that replaces the
// original (in place) — destroying edits the user may have made to it after
// the copy. The journal-read site therefore migrates a non-move row's
// MoveBack entries to the semantic today's journaler records for the same
// install (a plain Delete of the installed path): the installed copy is
// removed, the retained original is NEVER renamed over. Move-mode rows keep
// their rename-back untouched (pinned by
// TestRevertW241_MoveModeSubtitleMoveBackSemanticsPreserved) and new-format
// copy rows carry Delete entries only (pinned by
// TestRevertW241_CopyModeCopiedSubtitleRevertEndToEnd).

// Legacy-shaped row end-to-end: copy/link-mode operation whose subtitle was
// journaled the pre-#224-E way — MoveBack only, never Delete. The revert must
// delete the installed copy at its journal-installed path and leave the
// retained original byte-identical, even though both exist.
func TestRevertW241_LegacyMoveBackCopyLinkRowNeverOverwritesOriginal(t *testing.T) {
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

			srcDir := "/src-legacy/" + string(opType)
			dstDir := "/dst-legacy/" + string(opType) + "/LEG-100"
			installedVideo := dstDir + "/LEG-100.mkv"
			installedSub := dstDir + "/LEG-100.srt"
			goneSub := dstDir + "/LEG-100-alt.srt"
			destNFO := dstDir + "/LEG-100.nfo"
			originalSub := srcDir + "/LEG-100.srt"
			originalGoneSub := srcDir + "/LEG-100-alt.srt"
			require.NoError(t, fs.MkdirAll(dstDir, config.DirPerm))
			require.NoError(t, afero.WriteFile(fs, installedVideo, []byte("video-bytes"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, installedSub, []byte("installed-copied-subtitle"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, destNFO, []byte("dest-nfo"), config.FilePerm))
			require.NoError(t, fs.MkdirAll(srcDir, config.DirPerm))
			require.NoError(t, afero.WriteFile(fs, srcDir+"/LEG-100.mkv", []byte("source-video"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, originalSub, []byte("user-edited-original-subtitle"), config.FilePerm))
			require.NoError(t, afero.WriteFile(fs, originalGoneSub, []byte("second-retained-original"), config.FilePerm))
			// goneSub deliberately absent: the user already deleted that
			// installed copy — the best-effort delete must tolerate it.

			// The exact LEGACY journal shape: successfully copied subtitles
			// sat under move_back, and Delete carried the NFO/downloads only —
			// the installed subtitle copies appear nowhere else.
			op := &models.BatchFileOperation{
				BatchJobID:    "job-w241-legacy",
				MovieID:       "LEG-100",
				OriginalPath:  srcDir + "/LEG-100.mkv",
				NewPath:       installedVideo,
				OperationType: opType,
				GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
					Delete: []string{destNFO},
					MoveBack: []models.FileMove{
						{OriginalPath: originalSub, NewPath: installedSub},
						{OriginalPath: originalGoneSub, NewPath: goneSub},
					},
				}),
				RevertStatus: models.RevertStatusApplied,
			}
			require.NoError(t, repo.Create(ctx, op))

			res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w241-legacy")
			require.NoError(t, err)
			require.Equal(t, 1, res.Succeeded, "legacy copy/link rows revert through the cleanup leg")
			require.Equal(t, models.RevertOutcomeReverted, res.Outcomes[0].Outcome)

			// The installed copies are deleted at their journal-installed
			// paths only — the ONLY places a non-destructive install
			// introduced bytes at.
			for _, gone := range []string{installedSub, destNFO, goneSub} {
				_, statErr := fs.Stat(gone)
				assert.True(t, os.IsNotExist(statErr), "journal-installed artifact %s is deleted", gone)
			}

			// The retained originals are NEVER renamed over: byte-for-byte
			// user edits survive even though the installed copy existed.
			sub, err := afero.ReadFile(fs, originalSub)
			require.NoError(t, err)
			assert.Equal(t, []byte("user-edited-original-subtitle"), sub,
				"POSIX rename(new, original) would have clobbered the user's edited original")
			sub2, err := afero.ReadFile(fs, originalGoneSub)
			require.NoError(t, err)
			assert.Equal(t, []byte("second-retained-original"), sub2)
			srcVideo, err := afero.ReadFile(fs, srcDir+"/LEG-100.mkv")
			require.NoError(t, err)
			assert.Equal(t, []byte("source-video"), srcVideo, "source retained byte-for-byte")

			installed, err := afero.ReadFile(fs, installedVideo)
			require.NoError(t, err)
			assert.Equal(t, []byte("video-bytes"), installed,
				"the installed primary copy is retained — non-move revert never unlinks it")

			row, err := repo.FindByID(ctx, op.ID)
			require.NoError(t, err)
			assert.Equal(t, models.RevertStatusReverted, row.RevertStatus)
		})
	}
}

// The update leg runs the same cleanup path; an update row never gave up a
// source either, so a legacy MoveBack entry there is subject to the same
// delete-only migration.
func TestRevertW241_LegacyMoveBackUpdateRowNeverOverwritesOriginal(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	srcDir := "/src-legacy-upd/UPD-100"
	installedSub := srcDir + "/UPD-100.en.srt"
	originalSub := srcDir + "/UPD-100.en.srt.src" // retained pre-copy original
	require.NoError(t, fs.MkdirAll(srcDir, config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, srcDir+"/UPD-100.mkv", []byte("in-place-video"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, installedSub, []byte("downloaded-subtitle"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, originalSub, []byte("user-curated-original"), config.FilePerm))

	op := &models.BatchFileOperation{
		BatchJobID:   "job-w241-legacy-upd",
		MovieID:      "UPD-100",
		OriginalPath: srcDir + "/UPD-100.mkv",
		// update rows keep NewPath empty — the primary never relocated.
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			MoveBack: []models.FileMove{{OriginalPath: originalSub, NewPath: installedSub}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w241-legacy-upd")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	_, statErr := fs.Stat(installedSub)
	assert.True(t, os.IsNotExist(statErr), "the installed subtitle copy is deleted")
	orig, err := afero.ReadFile(fs, originalSub)
	require.NoError(t, err)
	assert.Equal(t, []byte("user-curated-original"), orig, "the retained original is never renamed over")
	video, err := afero.ReadFile(fs, srcDir+"/UPD-100.mkv")
	require.NoError(t, err)
	assert.Equal(t, []byte("in-place-video"), video, "the in-place primary is untouched")
}

// Best-effort pin: a wedged delete of the installed copy surfaces nothing and
// — crucially — never falls back to the rename-over leg as a "recovery".
func TestCleanupGeneratedFilesFS_LegacyMoveBackDeleteFailureIsBestEffort(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/dst/LEG-200", 0777))
	require.NoError(t, afero.WriteFile(base, "/dst/LEG-200/LEG-200.srt", []byte("installed-copy"), 0666))
	require.NoError(t, base.MkdirAll("/src", 0777))
	require.NoError(t, afero.WriteFile(base, "/src/LEG-200.srt", []byte("retained-original"), 0666))

	fs := &removeFailFs{Fs: base, victim: "/dst/LEG-200/LEG-200.srt"}
	op := &models.BatchFileOperation{
		OperationType: models.OperationTypeCopy,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			MoveBack: []models.FileMove{{OriginalPath: "/src/LEG-200.srt", NewPath: "/dst/LEG-200/LEG-200.srt"}},
		}),
	}
	cleanupGeneratedFilesFS(fs, op, "/dst")

	exists, err := afero.Exists(base, "/dst/LEG-200/LEG-200.srt")
	require.NoError(t, err)
	assert.True(t, exists, "best-effort remove failure leaves the installed copy in place")
	orig, err := afero.ReadFile(base, "/src/LEG-200.srt")
	require.NoError(t, err)
	assert.Equal(t, []byte("retained-original"), orig,
		"original never overwritten — not even as a fallback for the failed delete")
}
