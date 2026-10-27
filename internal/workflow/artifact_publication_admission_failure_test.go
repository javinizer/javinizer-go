package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

type publicationGuardPlanRecorder struct {
	*pr260PublicationFaultOrganizer
	commands      []organizer.OrganizeCmd
	beforeFailure func(organizer.OrganizeCmd)
}

func (o *publicationGuardPlanRecorder) PlanOrganize(ctx context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	o.commands = append(o.commands, cmd)
	if o.planCalls+1 == o.failPlanAt {
		o.beforeFailure(cmd)
	}
	return o.pr260PublicationFaultOrganizer.PlanOrganize(ctx, cmd)
}

func requirePublicationClaimsReleased(t *testing.T, fs afero.Fs, tracker *organizer.DuplicateTracker, source, target string) {
	t.Helper()
	release, err := fsutil.AcquireReplacementBusy(fs, target)
	require.NoError(t, err, "failed publication must release its durable destination claim")
	require.NotNil(t, release)
	release()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	other := source + ".next"
	_, duplicate := tracker.ObserveClaim(ctx, other, target, true)
	defer tracker.ReleaseClaim(other, target)
	require.NoError(t, ctx.Err(), "failed publication must unblock duplicate claim waiters")
	require.False(t, duplicate, "failed publication must not settle a duplicate claim")
}

func requirePublicationInputsUnchanged(t *testing.T, env *copyIntentE2EEnv) {
	t.Helper()
	for path, want := range map[string]string{env.source: "video", env.subtitle: "subtitle", env.multipart: "part two", env.unrelated: "unrelated"} {
		got, err := afero.ReadFile(env.fs, path)
		require.NoError(t, err)
		require.Equal(t, want, string(got), path)
	}
}

func TestApplyGuardReplanFailureRestoresDisplacedVideo(t *testing.T) {
	env := setupCopyIntentE2E(t, "guard-replan-failure")
	tracker := organizer.NewDuplicateTracker(false)
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	cmd.Organize.DuplicateTracker = tracker
	plan, err := env.org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, MoveFiles: true})
	require.NoError(t, err)
	require.NoError(t, env.fs.MkdirAll(plan.TargetDir, 0755))
	require.NoError(t, afero.WriteFile(env.fs, plan.TargetPath, []byte("old destination video"), 0644))
	// Prepare's claim plan, the pipeline's plan, and publication's replan all
	// precede the guarded replan. Fail only after the old occupant is journaled.
	guard := &publicationGuardPlanRecorder{pr260PublicationFaultOrganizer: &pr260PublicationFaultOrganizer{Organizer: env.org, failPlanAt: 4}}
	guard.beforeFailure = func(cmd organizer.OrganizeCmd) {
		require.False(t, cmd.ForceUpdate, "the publisher must surrender overwrite authority before replanning")
		_, err := env.fs.Stat(plan.TargetPath)
		require.True(t, os.IsNotExist(err), "the old destination must already be set aside")
		rows, err := env.repo.FindByBatchJobID(t.Context(), env.jobID)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		ledger, err := models.ParseGeneratedFiles(rows[0].GeneratedFiles)
		require.NoError(t, err)
		require.Len(t, ledger.Replacements, 1)
		require.Equal(t, plan.TargetPath, ledger.Replacements[0].Destination)
		saved, err := afero.ReadFile(env.fs, ledger.Replacements[0].Backup)
		require.NoError(t, err)
		require.Equal(t, "old destination video", string(saved))
	}
	env.orch.organizer = guard
	result, err := env.orch.Execute(t.Context(), cmd)
	require.ErrorContains(t, err, "guard staged video publication: publication plan denied")
	require.NotNil(t, result)
	require.NotEmpty(t, result.OperationID)
	require.Len(t, guard.commands, 4)
	for _, previous := range guard.commands[:3] {
		require.True(t, previous.ForceUpdate)
	}
	require.False(t, guard.commands[3].ForceUpdate)
	requirePublicationInputsUnchanged(t, env)
	body, err := afero.ReadFile(env.fs, plan.TargetPath)
	require.NoError(t, err)
	require.Equal(t, "old destination video", string(body))
	requirePublicationClaimsReleased(t, env.fs, tracker, env.source, plan.TargetPath)
	require.Empty(t, p3Ledger(t, env.repo, result.OperationID).Replacements, "successful rollback consumes the displaced-video journal entry")
	pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))
}

func TestApplyCopiedSubtitleConfirmFailureRollsBackOwnedOutputs(t *testing.T) {
	env := setupCopyIntentE2E(t, "sidecar-confirm-failure")
	fs := &residentConfirmationFaultFS{Fs: env.fs}
	real := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	env.orch.fs, env.orch.organizer = fs, real
	tracker := organizer.NewDuplicateTracker(false)
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.DuplicateTracker = tracker
	plan, err := real.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest})
	require.NoError(t, err)
	subtitles := real.PlanSubtitleMoves(plan)
	require.Len(t, subtitles, 1)
	target := subtitles[0].NewPath
	require.NoError(t, env.fs.MkdirAll(plan.TargetDir, 0755))
	foreign := filepath.Join(plan.TargetDir, "keep-user-file.txt")
	require.NoError(t, afero.WriteFile(env.fs, foreign, []byte("user owned"), 0644))
	original := observeBoundSidecarPublish
	t.Cleanup(func() { observeBoundSidecarPublish = original })
	observations := 0
	observeBoundSidecarPublish = func(batch *downloader.ReplacementBatch, destination string, identity *fsutil.BoundInstallIdentity) error {
		observations++
		require.Equal(t, target, destination)
		require.NotNil(t, identity, "use the real copied sidecar's installed identity")
		err := original(batch, destination, identity)
		require.NoError(t, err, "the real bound observation must precede the confirmation fault")
		fs.target, fs.armed = destination, true
		return err
	}
	result, err := env.orch.Execute(t.Context(), cmd)
	require.ErrorIs(t, err, os.ErrPermission)
	require.ErrorContains(t, err, "inspect staged publication result")
	require.Equal(t, 1, observations)
	require.False(t, fs.armed, "ConfirmPublish must consume the one-shot Lstat failure")
	require.NotNil(t, result)
	require.NotEmpty(t, result.OperationID)
	requirePublicationInputsUnchanged(t, env)
	require.NoFileExists(t, plan.TargetPath)
	require.NoFileExists(t, target)
	got, err := afero.ReadFile(env.fs, foreign)
	require.NoError(t, err)
	require.Equal(t, "user owned", string(got))
	requirePublicationClaimsReleased(t, env.fs, tracker, env.source, plan.TargetPath)
	release, err := fsutil.AcquireReplacementBusy(fs, target)
	require.NoError(t, err, "sidecar confirmation failure must release its claim too")
	require.NotNil(t, release)
	release()
	reopenPublicationRecoveryDB(t, env)
	ledger := p3Ledger(t, env.repo, result.OperationID)
	require.Empty(t, ledger.Delete, "an unconfirmed sidecar must not graduate into unconditional deletion")
	require.Empty(t, ledger.MoveBack, "copy mode must not invent move ownership")
	subtitleDigest, err := artifactDigest(env.fs, env.subtitle)
	require.NoError(t, err)
	videoInfo, videoDigest, err := fsutil.PartialCopyDigest(env.fs, env.source)
	require.NoError(t, err)
	require.ElementsMatch(t, []models.DeleteEntry{
		{Path: target, SHA256: subtitleDigest},
		{Path: plan.TargetPath, CopySize: videoInfo.Size(), CopyPartialSHA256: videoDigest},
	}, ledger.PlannedDeletes, "the reopened journal retains only the real pre-publication payload pins")
	rows, err := env.repo.FindByBatchJobID(t.Context(), env.jobID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, models.RevertStatusNoOp, rows[0].RevertStatus, "successful rollback is durably terminal rather than an anchor-missing failure")
	reverted, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	require.Zero(t, reverted.Failed, "durable failed-apply bookkeeping must remain recoverable after reopen")
	require.Zero(t, reverted.Skipped, "rollback must not leave an unresolved Undo anchor")
	requirePublicationInputsUnchanged(t, env)
	got, err = afero.ReadFile(env.fs, foreign)
	require.NoError(t, err)
	require.Equal(t, "user owned", string(got))
	pr260AssertStageGone(t, env.fs, filepath.Dir(env.dest))
}
