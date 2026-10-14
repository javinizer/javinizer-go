package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestResidentPrimaryHardlinkUndoMissingOriginal(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeOrganize} {
		for _, phase := range []string{"complete", "failed", "checkpoint only"} {
			for _, move := range []bool{false, true} {
				t.Run(fmt.Sprint(mode, move, phase), func(t *testing.T) {
					fs := afero.NewOsFs()
					root := t.TempDir()
					library := filepath.Join(root, "library")
					dir := filepath.Join(library, "ABC-123")
					require.NoError(t, fs.MkdirAll(dir, 0755))
					source := filepath.Join(dir, "incoming.mp4")
					target := filepath.Join(dir, "ABC-123.mp4")
					require.NoError(t, afero.WriteFile(fs, source, []byte("resident video"), 0644))
					require.NoError(t, os.Link(source, target))
					original, err := fs.Stat(target)
					require.NoError(t, err)
					sub := filepath.Join(dir, "incoming.en.srt")
					require.NoError(t, afero.WriteFile(fs, sub, []byte("subtitle"), 0644))
					db, _ := pr260ArtifactDB(t)
					repo := database.NewBatchFileOperationRepository(db)
					movie := models.Movie{ID: "ABC-123", ContentID: "reviewer"}
					engine := template.NewEngine()
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, engine, nil)
					orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "reviewer", fs, nil, nil, nil), nil, nil)
					cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: move}, OperationMode: mode, PublicationFence: postPublishFence{movie: &movie}}
					if phase == "checkpoint only" {
						orch.revertLog = &residentCheckpointOnlyLog{RevertLog: orch.revertLog}
					}
					result, err := orch.Execute(context.Background(), cmd)
					require.NoError(t, err)
					require.False(t, result.OrganizeResult.Moved)
					if phase == "failed" {
						require.NoError(t, orch.revertLog.CompleteFailed(context.Background(), result.OperationID, result))
					}
					require.NoError(t, fs.Remove(source))
					reverted, err := history.NewReverter(fs, repo).RevertBatch(context.Background(), "reviewer")
					require.NoError(t, err)
					require.Zero(t, reverted.Failed)
					require.Equal(t, 1, reverted.Succeeded)
					_, err = fs.Stat(source)
					require.True(t, os.IsNotExist(err), "Undo must not relocate user-owned resident")
					got, err := fs.Stat(target)
					require.NoError(t, err)
					require.True(t, os.SameFile(original, got))
					body, err := afero.ReadFile(fs, sub)
					require.NoError(t, err)
					require.Equal(t, "subtitle", string(body))
				})
			}
		}
	}
}

// Admission drift between preparation and publication cannot authorize an alias
// even if the target still holds the originally admitted object.
func TestResidentPrimaryAliasAdmissionDrift(t *testing.T) {
	env := setupCopyIntentE2E(t, "resident-drift")
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	plan, err := env.org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest})
	require.NoError(t, err)
	require.NoError(t, env.fs.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
	require.NoError(t, os.Link(env.source, plan.TargetPath))
	stage, _, err := env.orch.prepareArtifact(t.Context(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	require.NoError(t, env.fs.Rename(env.source, env.source+".saved"))
	require.NoError(t, afero.WriteFile(env.fs, env.source, []byte("foreign source"), 0644))
	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource, FolderPath: filepath.Dir(stage.stagedSource)}}
	require.ErrorIs(t, stage.publish(t.Context(), env.orch, state, nil), errArtifactSourceChanged)
}

func TestResidentPrimaryAliasInPlaceRemainsRefused(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		t.Run(string(mode), func(t *testing.T) {
			env := setupCopyIntentE2E(t, "resident-inplace-boundary")
			incoming := filepath.Dir(env.source)
			target := filepath.Join(incoming, env.movie.ID+".mp4")
			require.NoError(t, os.Link(env.source, target))
			env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, template.NewEngine(), nil)
			cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), mode, false, true, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			_, err := env.orch.Execute(t.Context(), cmd)
			require.ErrorContains(t, err, "overwrite is disabled")
			info, err := env.fs.Stat(env.source)
			require.NoError(t, err)
			other, err := env.fs.Stat(target)
			require.NoError(t, err)
			require.True(t, os.SameFile(info, other))
		})
	}
}

type residentConfirmationFaultFS struct {
	afero.Fs
	target string
	armed  bool
}

func (f *residentConfirmationFaultFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if f.armed && path == f.target {
		f.armed = false
		return nil, true, os.ErrPermission
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

// The adjacent nonresident path must retain its fallible confirmation gate.
func TestResidentPrimaryNonresidentConfirmFailure(t *testing.T) {
	env := setupCopyIntentE2E(t, "resident-nonresident-confirm")
	fs := &residentConfirmationFaultFS{Fs: env.fs}
	env.orch.fs = fs
	env.orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	original := observeBoundPrimaryPublish
	t.Cleanup(func() { observeBoundPrimaryPublish = original })
	observeBoundPrimaryPublish = func(batch *downloader.ReplacementBatch, target string, id *fsutil.BoundInstallIdentity) error {
		err := original(batch, target, id)
		fs.target = target
		fs.armed = true
		return err
	}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	_, err := env.orch.Execute(t.Context(), cmd)
	require.ErrorContains(t, err, "inspect staged publication result")
	require.False(t, fs.armed, "confirmation exercised injected refusal")
}

// Simulate losing every completion after the durable pre-execution checkpoint.
type residentCheckpointOnlyLog struct {
	RevertLog
	calls int
}

func (l *residentCheckpointOnlyLog) Complete(ctx context.Context, id OperationID, result *ApplyResult) error {
	l.calls++
	if l.calls > 1 || result.OrganizeResult == nil || !result.OrganizeResult.PrimaryResident || result.OrganizeResult.NewPath != "" || len(result.OrganizeResult.Subtitles) != 0 {
		return nil
	}
	return l.RevertLog.Complete(ctx, id, result)
}

func TestResidentPrimaryCheckpointFailure(t *testing.T) {
	env := setupCopyIntentE2E(t, "resident-checkpoint-failure")
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	plan, err := env.org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest})
	require.NoError(t, err)
	require.NoError(t, env.fs.MkdirAll(filepath.Dir(plan.TargetPath), 0755))
	require.NoError(t, os.Link(env.source, plan.TargetPath))
	stage, _, err := env.orch.prepareArtifact(t.Context(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	log := &completeCallFaultLog{failAt: 1}
	env.orch.revertLog = log
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource, FolderPath: filepath.Dir(stage.stagedSource)}}
	require.ErrorContains(t, stage.publish(t.Context(), env.orch, state, nil), "persist resident primary before sidecar publication")
	require.Zero(t, log.intents)
	require.Zero(t, log.deletes)
	require.FileExists(t, env.source)
	require.FileExists(t, env.subtitle)
}
