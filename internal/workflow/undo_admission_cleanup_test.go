package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestUndoAdmissionMixedBatchPreservesHeldAncestor(t *testing.T) {
	for _, scrape := range []bool{false, true} {
		t.Run(map[bool]string{false: "batch", true: "scrape"}[scrape], func(t *testing.T) {
			env := setupCopyIntentE2E(t, "held-ancestor")
			ancestor := filepath.Join(t.TempDir(), "empty-library")
			require.NoError(t, env.fs.MkdirAll(ancestor, 0755))
			for _, held := range []bool{false, true} {
				row := &models.BatchFileOperation{BatchJobID: env.jobID, MovieID: env.movie.ID, OriginalPath: env.source, NewPath: filepath.Join(ancestor, "movie", "movie.mp4"), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryResident: true, PublicationPending: held})}
				require.NoError(t, env.repo.Create(t.Context(), row))
			}
			reverter := history.NewReverter(env.fs, env.repo)
			var result *history.RevertBatchResult
			var err error
			if scrape {
				result, err = reverter.RevertScrape(t.Context(), env.jobID, env.movie.ID)
			} else {
				result, err = reverter.RevertBatch(t.Context(), env.jobID)
			}
			require.NoError(t, err)
			require.Equal(t, 1, result.Succeeded)
			require.Equal(t, 1, result.Failed)
			require.DirExists(t, ancestor, "a successful sibling must not authorize cleanup of a held row's ancestor")
		})
	}
}

func TestUndoAdmissionMergePreservesExclusion(t *testing.T) {
	prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{UndoStarted: true})
	for _, fresh := range []string{"", `{}`, `{"delete":["owned"]}`, `{"undo_started":false,"move_back":[{"original_path":"a","new_path":"b"}]}`} {
		for _, promote := range []bool{false, true} {
			ledger, err := models.ParseGeneratedFiles(mergeReplacementLedgerIntents(prior, fresh, promote))
			require.NoError(t, err)
			require.True(t, ledger.UndoStarted, "completion must never reopen publication")
		}
	}
	legacy, err := models.ParseGeneratedFiles(`{"delete":["legacy"]}`)
	require.NoError(t, err)
	require.False(t, legacy.UndoStarted)
}

// Undo wins after the proof-bound directory relocation but before the resident
// sidecar checkpoint. Apply must stop and must not resurrect the consumed inverse.
func TestUndoAdmissionInPlaceStopsSidecarPublication(t *testing.T) {
	env := setupCopyIntentE2E(t, "undo-before-inplace-checkpoint")
	env.dest = t.TempDir()
	dir := filepath.Join(env.dest, "incoming")
	require.NoError(t, env.fs.MkdirAll(dir, 0755))
	env.movie.ID = "ABC-123"
	env.source = filepath.Join(dir, "abc-123-source.mp4")
	env.subtitle = filepath.Join(dir, "abc-123-source.srt")
	require.NoError(t, afero.WriteFile(env.fs, env.source, []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(env.fs, env.subtitle, []byte("subtitle"), 0644))
	require.NoError(t, os.Link(env.source, filepath.Join(dir, env.movie.ID+".mp4")))
	originalIdentity, err := env.fs.Stat(env.source)
	require.NoError(t, err)
	beforeDev, beforeIno, beforeStrong := fsutil.BoundObjectIdentity(env.fs, env.source, originalIdentity)
	require.True(t, beforeStrong)
	env.match = models.FileMatchInfo{Path: env.source, Name: filepath.Base(env.source), Extension: ".mp4", MovieID: env.movie.ID}
	mt, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), mt)
	var afterUndo string
	log := &undoBeforeResidentCheckpointLog{RevertLog: NewDBRevertLog(env.repo, NewRevertLogConfig(true, nil), env.jobID, env.fs, nil, nil, nil), before: func() {
		result, e := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
		require.NoError(t, e)
		require.Equal(t, 1, result.Succeeded)
		rows, e := env.repo.FindByBatchJobID(t.Context(), env.jobID)
		require.NoError(t, e)
		require.Len(t, rows, 1)
		afterUndo = rows[0].GeneratedFiles
	}}
	orch := newApplyOrchestrator(env.fs, org, nil, nil, nil, ApplyConfig{}, template.NewEngine(), log, nil, nil)
	result, err := orch.Execute(t.Context(), ApplyCmd{Movie: &env.movie, Match: env.match, DestPath: env.dest, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeInPlace, PublicationFence: postPublishFence{movie: &env.movie}})
	require.ErrorContains(t, err, "admitted to Undo")
	require.True(t, log.called)
	ledger := p3Ledger(t, env.repo, result.OperationID)
	require.True(t, ledger.UndoStarted)
	require.False(t, ledger.PublicationPending)
	require.Empty(t, ledger.Delete)
	require.Empty(t, ledger.MoveBack)
	require.Empty(t, ledger.PlannedDeletes)
	require.NotNil(t, ledger.DirectoryMove)
	require.Equal(t, dir, ledger.DirectoryMove.Original)
	rows, e := env.repo.FindByBatchJobID(t.Context(), env.jobID)
	require.NoError(t, e)
	require.Len(t, rows, 1)
	require.Equal(t, models.RevertStatusReverted, rows[0].RevertStatus)
	require.JSONEq(t, afterUndo, rows[0].GeneratedFiles, "rejected completion must preserve the exact post-Undo ledger")
	for path, body := range map[string]string{env.source: "video", env.subtitle: "subtitle", filepath.Join(dir, env.movie.ID+".mp4"): "video"} {
		got, e := afero.ReadFile(env.fs, path)
		require.NoError(t, e)
		require.Equal(t, body, string(got))
	}
	for _, path := range []string{env.source, filepath.Join(dir, env.movie.ID+".mp4")} {
		after, e := env.fs.Stat(path)
		require.NoError(t, e)
		afterDev, afterIno, afterStrong := fsutil.BoundObjectIdentity(env.fs, path, after)
		require.True(t, afterStrong)
		require.Equal(t, beforeDev, afterDev)
		require.Equal(t, beforeIno, afterIno, "resident native identity must survive Undo-first arbitration")
	}
	require.NoDirExists(t, filepath.Join(env.dest, env.movie.ID))
}

type undoBeforeResidentCheckpointLog struct {
	RevertLog
	before func()
	called bool
}

func (l *undoBeforeResidentCheckpointLog) Complete(ctx context.Context, id OperationID, result *ApplyResult) error {
	if result != nil && result.PublicationPending {
		l.called = true
		l.before()
	}
	return l.RevertLog.Complete(ctx, id, result)
}

func TestUndoAdmissionPublicationRejectsMalformedJournal(t *testing.T) {
	f := newDurableAdmissionFixture(t, false)
	f.row.GeneratedFiles = "{invalid"
	require.NoError(t, f.repo.Update(t.Context(), f.row))
	err := f.log.Complete(t.Context(), f.id, &ApplyResult{PublicationPending: true, OrganizeResult: &organizer.OrganizeResult{PrimaryResident: true, OriginalPath: f.primary}})
	require.Error(t, err)
	row, e := f.repo.FindByID(t.Context(), f.row.ID)
	require.NoError(t, e)
	require.Equal(t, "{invalid", row.GeneratedFiles)
	body, e := afero.ReadFile(f.fs, f.artifact)
	require.NoError(t, e)
	require.Equal(t, "installed subtitle", string(body))
}
