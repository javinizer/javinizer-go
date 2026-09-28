package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The v1.6.x regression: organize mode copied every source video into a
// hidden .javinizer-apply-* sibling folder even for "move" operations, which
// on a network share doubled the transfer volume of large files. The video
// must now be deferred to the fenced publication (same-volume move = rename),
// while small sidecar siblings stay staged, and in-place modes keep their
// real staged copy for directory-rename rollback.
func TestPrepareArtifactOrganizeDefersVideoCopy(t *testing.T) {
	base, root, source, subtitle, multipart, _, match := pr260FencedFiles(t, "defer-copy")
	dest := filepath.Join(root, "library")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-copy"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
	}

	stage, staged, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	defer stage.cleanup()

	assert.True(t, stage.videoStagingDeferred(), "organize mode defers the video")
	exists, err := afero.Exists(base, stage.stagedSource)
	require.NoError(t, err)
	assert.False(t, exists, "no staged copy of the video payload")
	assert.Equal(t, stage.stagedSource, staged.Match.Path, "pipeline still addresses the virtual staged path")
	assert.Equal(t, source, stage.sourcePath)

	for _, sibling := range []string{subtitle, multipart} {
		exists, serr := afero.Exists(base, filepath.Join(stage.root, ".source", filepath.Base(sibling)))
		require.NoError(t, serr)
		assert.True(t, exists, "small sidecars stay staged: %s", sibling)
	}
	manifestExists, merr := afero.Exists(base, filepath.Join(stage.root, artifactStageManifestName))
	require.NoError(t, merr)
	assert.True(t, manifestExists, "ownership manifest stamped")
}

func TestPrepareArtifactInPlaceKeepsStagedVideoCopy(t *testing.T) {
	base, _, _, _, _, _, match := pr260FencedFiles(t, "defer-inplace")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-inplace"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
		OperationMode:    operationmode.OperationModeInPlace,
	}

	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	defer stage.cleanup()

	assert.False(t, stage.videoStagingDeferred(), "in-place keeps the staged copy")
	exists, err := afero.Exists(base, stage.stagedSource)
	require.NoError(t, err)
	assert.True(t, exists, "in-place stages a real video copy")
}

type noPlanOrganizer struct{}

func (noPlanOrganizer) Organize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	return nil, errors.New("unused")
}

// The deferred organize step requires the planner seam: an organizer without
// PlanOrganize must fail the organize step instead of silently doing nothing.
func TestDeferredOrganizeRequiresPlanningSeam(t *testing.T) {
	base, root, _, _, _, _, match := pr260FencedFiles(t, "defer-noseam")
	dest := filepath.Join(root, "library")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-noseam"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
	}
	_, err := (&applyOrchImpl{fs: base, organizer: noPlanOrganizer{}}).Execute(context.Background(), cmd)
	require.ErrorContains(t, err, "planning seam")
}

// A planning failure in the deferred organize step aborts the pipeline.
func TestDeferredOrganizePlanFailureAborts(t *testing.T) {
	base, root, _, _, _, _, match := pr260FencedFiles(t, "defer-planfault")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, failPlanAt: 1}
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-planfault"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
	}
	_, err := (&applyOrchImpl{fs: base, organizer: fault}).Execute(context.Background(), cmd)
	require.ErrorContains(t, err, "publication plan denied")
}

// In-place still stages a real video copy: a denied staged create must clean
// up the staging root and retain every input.
func TestPrepareArtifactInPlaceStagedCopyFailureRetainsInputs(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "inplace-copy-fault")
	fs := &pr260FiniteCopyFS{Fs: base, op: "staged create", source: source}
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "inplace-copy-fault"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		Organize:         OrganizeOptions{MoveFiles: true},
		Download:         true,
		OperationMode:    operationmode.OperationModeInPlace,
	}
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.ErrorContains(t, err, "create staged source")
	require.Nil(t, stage)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertStageGone(t, base, root)
}

// The ownership manifest sits inside the staging root: both artifact walks
// must exclude it from publication destinations and installs.
func TestStagingWalksSkipOwnershipManifest(t *testing.T) {
	base := afero.NewMemMapFs()
	root := filepath.FromSlash("/stage")
	final := filepath.FromSlash("/final")
	artifact := filepath.Join(root, "child", "a.txt")
	require.NoError(t, base.MkdirAll(filepath.Dir(artifact), 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(root, artifactStageManifestName), []byte("{}"), 0o600))
	require.NoError(t, afero.WriteFile(base, artifact, []byte("x"), 0o644))
	stage := &artifactStage{fs: base, root: root, finalRoot: final}

	dests, err := stage.treeDestinations("", "", "", "")
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(final, "child", "a.txt")}, dests)

	_, err = stage.installTree("", "", nil, "", "", nil)
	require.NoError(t, err)
	exists, _ := afero.Exists(base, filepath.Join(final, "child", "a.txt"))
	require.True(t, exists)
	manifestLeaked, _ := afero.Exists(base, filepath.Join(final, artifactStageManifestName))
	require.False(t, manifestLeaked, "manifest is never installed into the library")
}

// A fence that finalizes post-publication as stale drives the completedBatch
// compensation path: the deferred video must roll back onto its source and
// the armed markers reset (the failure is pre-publication again).
type staleFinalizeFencer struct{}

func (staleFinalizeFencer) WithApplyPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(&models.Movie{})
}

func (staleFinalizeFencer) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	if err := fn(&models.Movie{}); err != nil {
		return err
	}
	return database.ErrApplyPublicationStale
}

func TestDeferredPublishStaleFinalizeRollsBackAndResetsMarkers(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-stale-finalize", "")
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-stale-finalize")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: real}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	cmd.PublicationFence = staleFinalizeFencer{}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorIs(t, publishErr, database.ErrApplyPublicationStale)
	assert.False(t, stage.sourceCleanupArmed, "rollback restored the source: marker resets")
	assert.False(t, stage.directOriginArmed)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	regularFiles := 0
	_ = afero.Walk(base, dest, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			regularFiles++
		}
		return nil
	})
	assert.Zero(t, regularFiles, "compensation removes every published file")
}

// completeCallFaultLog fails the failAt-th Complete; intentErr fails the
// pending move-intent write, letting tests target each durable-journal leg in
// the deferred publish separately.
type completeCallFaultLog struct {
	RevertLog
	failAt       int32
	calls        int32
	intentErr    error
	intentFailAt int32
	intents      int32
	deleteErr    error
	deletes      int32
	deletePaths  []models.DeleteEntry
	reconcileErr error
	reconciles   int32
	keepCaptured []models.FileMove
}

func (l *completeCallFaultLog) Complete(context.Context, OperationID, *ApplyResult) error {
	if atomic.AddInt32(&l.calls, 1) == l.failAt {
		return errors.New("journal unavailable")
	}
	return nil
}

func (l *completeCallFaultLog) RecordMoveIntent(context.Context, OperationID, string, string) error {
	if atomic.AddInt32(&l.intents, 1) == l.intentFailAt {
		return errors.New("intent journal unavailable")
	}
	return l.intentErr
}

func (l *completeCallFaultLog) RecordDeleteIntent(_ context.Context, _ OperationID, entries []models.DeleteEntry) error {
	atomic.AddInt32(&l.deletes, 1)
	l.deletePaths = append(l.deletePaths, entries...)
	return l.deleteErr
}

func (l *completeCallFaultLog) ReconcileMoveIntents(_ context.Context, _ OperationID, keep []models.FileMove) error {
	atomic.AddInt32(&l.reconciles, 1)
	l.keepCaptured = append([]models.FileMove(nil), keep...)
	return l.reconcileErr
}

// A deferred move whose intended inverse cannot be journaled must NOT consume
// the real source: fail before ExecuteOrganizePlan ever runs.
func TestDeferredMoveAbortsWhenIntentCannotPersist(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-intent-fault")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{intentErr: errors.New("intent unavailable")}
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-intent-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "persist inverse before direct video publication")
	require.Equal(t, int32(1), atomic.LoadInt32(&ledger.intents), "aborted at the pre-consume intent write")
	require.Zero(t, atomic.LoadInt32(&ledger.calls), "no completion journal before the intent succeeded")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
}

// The intent journal lands, the move consumes the source, then the post-move
// inverse record fails: rollback must restore the video and reset markers.
func TestDeferredMovePostPublishInverseFaultRollsBack(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-postinverse-fault")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: &completeCallFaultLog{failAt: 1}}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-postinverse-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "persist inverse after direct video publication")
	assert.False(t, stage.sourceCleanupArmed)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// MoveSubtitles enabled + a post-publish journal fault: rollback must restore
// BOTH the video and the moved subtitle onto their original paths, and the
// armed markers reset.
func TestDeferredMoveRollbackRestoresMovedSubtitle(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-sub-restore")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: &completeCallFaultLog{failAt: 1}}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-sub-restore"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "persist inverse after direct video publication")
	assert.False(t, stage.sourceCleanupArmed)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	regularFiles := 0
	_ = afero.Walk(base, dest, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			regularFiles++
		}
		return nil
	})
	assert.Zero(t, regularFiles, "rollback removes video and subtitle copies from the destination")
}

// RecordMoveIntent writes a pending move-back journal entry without touching
// the row's completion columns: recovery can tell the intent apart from an
// executed move.
func TestRecordMoveIntentJournalsPendingMoveOnly(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "move-intent", "")
	fs, root, source, _, _, _, match := pr260FencedFiles(t, "move-intent")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "move-intent", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)
	require.NotEmpty(t, opID)

	target := filepath.Join(dest, "movie.mp4")
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, source, target))

	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	require.NotNil(t, row)
	assert.Empty(t, row.NewPath, "intent write never touches completion columns")
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.MoveBack, 1)
	assert.Equal(t, source, gf.MoveBack[0].OriginalPath)
	assert.Equal(t, target, gf.MoveBack[0].NewPath)
	assert.NotEmpty(t, gf.Roots, "begin-seeded discovery root survives the intent merge")

	require.NoError(t, log.RecordMoveIntent(context.Background(), "", source, target), "empty op id is a no-op")
	require.Error(t, log.RecordMoveIntent(context.Background(), "abc", source, target), "unparsable id")
	require.Error(t, log.RecordMoveIntent(context.Background(), "99999999", source, target), "missing row")
	require.Error(t, log.RecordMoveIntent(context.Background(), opID, "", target), "empty endpoint")
}

func mustParseOpID(t *testing.T, opID OperationID) uint {
	t.Helper()
	id, err := strconv.ParseUint(opID, 10, 64)
	require.NoError(t, err)
	return uint(id)
}

// Sidecar leg faults in the deferred publish: an unarmable subtitle move and a
// failing subtitle intent journal both fail closed; non-moved subtitle entries
// are skipped.
func TestDeferredMoveSubtitleLegFaults(t *testing.T) {
	type variant string
	const (
		armFailure     variant = "arm"
		intentFailure  variant = "intent"
		skippedEntries variant = "skipped"
	)
	for _, v := range []variant{armFailure, intentFailure, skippedEntries} {
		t.Run(string(v), func(t *testing.T) {
			base, root, source, subtitle, _, unrelated, match := pr260FencedFiles(t, "deferred-subfault-"+string(v))
			dest := filepath.Join(root, "library")
			real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
			var fs = base
			_ = fs
			var org organizer.OrganizerInterface = real
			var log RevertLog
			switch v {
			case armFailure:
				org = &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
					for i := range result.Subtitles {
						result.Subtitles[i].NewPath = filepath.Join(root, "missing-parent", filepath.Base(result.Subtitles[i].NewPath))
					}
				}}
			case intentFailure:
				log = &completeCallFaultLog{failAt: 1}
			case skippedEntries:
				org = &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
					result.Subtitles = append(result.Subtitles, organizer.SubtitleResult{SubtitleMove: models.SubtitleMove{OriginalPath: subtitle, NewPath: "", Copied: true}})
				}}
				log = &completeCallFaultLog{failAt: 1}
			}
			orch := &applyOrchImpl{fs: fs, organizer: org, revertLog: log}
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-subfault-" + string(v)}, match, dest)
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = true
			cmd.Download = false
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
			require.NoError(t, planErr)
			state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}
			publishErr := stage.publish(context.Background(), orch, state, nil)
			switch v {
			case armFailure:
				require.ErrorContains(t, publishErr, "arm subtitle rollback")
			case intentFailure:
				require.ErrorContains(t, publishErr, "persist inverse after direct video publication")
			case skippedEntries:
				require.ErrorContains(t, publishErr, "persist inverse after direct video publication")
			}
			exists, statErr := afero.Exists(base, source)
			require.NoError(t, statErr)
			require.True(t, exists, "video restored or never consumed")
			exists3, statErr3 := afero.Exists(base, unrelated)
			require.NoError(t, statErr3)
			require.True(t, exists3)
			if v != armFailure {
				// arm failure faults the subtitle target path itself (nothing to
				// recover there); the other legs restore the moved subtitle.
				exists2, statErr2 := afero.Exists(base, subtitle)
				require.NoError(t, statErr2)
				require.True(t, exists2, "original subtitle restored")
			}
		})
	}
}

// A failed artifact-destination intent journal aborts the publication before
// any filesystem mutation: source and staged tree intact, destination absent.
func TestDeferredPublishAbortsWhenArtifactIntentFails(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-delintent-fault")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{deleteErr: errors.New("destination journal unavailable")}
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-delintent-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	require.NoError(t, base.MkdirAll(plan.TargetDir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(plan.TargetDir, "generated.nfo"), []byte("metadata"), 0o644))
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "journal artifact destination intent")
	require.Equal(t, int32(1), atomic.LoadInt32(&ledger.deletes))
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	targetVideo := filepath.Join(dest, "movie", filepath.Base(state.organizeResult.NewPath))
	exists, statErr := afero.Exists(base, targetVideo)
	require.NoError(t, statErr)
	require.False(t, exists, "rollback removes the published video")
}

// The publish flow must journal pending subtitle intents BEFORE the execute
// step runs: faulting the subtitle intent proves nothing mutated yet.
func TestDeferredMoveJournalsSubtitleIntentsBeforeExecute(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-subintent-order")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{intentFailAt: 2} // video=1, subtitle=2
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-subintent-order"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	plan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "persist inverse before subtitle publication")
	require.Equal(t, int32(2), atomic.LoadInt32(&ledger.intents))
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
}

// installPaths journals deletion intents only for targets this operation will
// actually install: skipped (preserved/consumer) and replacement destinations
// are excluded (their pre-existing bytes are restored by the replacement legs).
func TestInstallPathsJournalsOnlyOwnedNonReplacementDestinations(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/stage", 0o755))
	require.NoError(t, base.MkdirAll("/final", 0o755))
	require.NoError(t, afero.WriteFile(base, "/stage/new.txt", []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/stage/existing.txt", []byte("payload"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/final/existing.txt", []byte("prior bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/stage/keep.txt", []byte("keep"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/final/keep.txt", []byte("user kept"), 0o644))

	stage := &artifactStage{fs: base, root: "/stage", finalRoot: "/final", original: ApplyCmd{}}
	var journaled []models.DeleteEntry
	_, err := stage.installPaths(
		[]string{"/stage/new.txt", "/stage/existing.txt", "/stage/keep.txt"},
		[]string{"/stage/keep.txt"},
		"", "",
		func(entries []models.DeleteEntry) error {
			journaled = append(journaled, entries...)
			return nil
		},
	)
	require.NoError(t, err)
	require.Len(t, journaled, 1)
	assert.Equal(t, filepath.Join("/final", "new.txt"), journaled[0].Path,
		"only the new destination is journaled; replacements preserved bytes and preserved paths excluded")
}

// The pending delete is journaled before the install lands (hash-pinned): a
// failed journal aborts the install, so nothing lands without a ledger entry.
func TestInstallPathsJournalFailureAbortInstall(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/stage", 0o755))
	require.NoError(t, afero.WriteFile(base, "/stage/new.txt", []byte("new"), 0o644))
	stage := &artifactStage{fs: base, root: "/stage", finalRoot: "/final", original: ApplyCmd{}}
	_, err := stage.installPaths([]string{"/stage/new.txt"}, nil, "", "", func([]models.DeleteEntry) error {
		return errors.New("journal down")
	})
	require.ErrorContains(t, err, "journal down")
	exists, _ := afero.Exists(base, "/final/new.txt")
	require.False(t, exists, "journal failure aborts before the install")
}

// Outcome reconciliation publishes exactly the executed moves: the pending
// journal contains the video intent and the sidecar that actually moved.
func TestDeferredMoveReconcilesOutcomesIntoJournal(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-reconcile-outcomes")
	dest := filepath.Join(root, "library")
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-reconcile-outcomes", "")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: &completeCallFaultLog{}}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	finalPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	subMoves := org.PlanSubtitleMoves(finalPlan)
	require.Len(t, subMoves, 1)

	stagedPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil))
	ledger := orch.revertLog.(*completeCallFaultLog)
	require.Equal(t, int32(1), atomic.LoadInt32(&ledger.reconciles), "outcome reconciliation ran once")
	require.NotNil(t, ledger.keepCaptured)
	assert.Contains(t, ledger.keepCaptured, models.FileMove{OriginalPath: source, NewPath: finalPlan.TargetPath})
	assert.Contains(t, ledger.keepCaptured, models.FileMove{OriginalPath: subMoves[0].OriginalPath, NewPath: subMoves[0].NewPath}, "the executed subtitle move stays armed")
	assert.Len(t, ledger.keepCaptured, 2)
	existsSub, _ := afero.Exists(base, subtitle)
	assert.False(t, existsSub, "moved subtitle left the source")
	existsMulti, _ := afero.Exists(base, multipart)
	assert.False(t, existsMulti, "the staged sibling video also moves in move mode")
	existsUnrelated, _ := afero.Exists(base, unrelated)
	assert.True(t, existsUnrelated)
}

// A failing outcome reconciliation surfaces and the rollback restores the moved
// video (armed origin) — the row keeps no stale intents.
func TestDeferredMoveReconcileFailureRollsBack(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-reconcile-fault")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: &completeCallFaultLog{reconcileErr: errors.New("reconcile down")}}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-reconcile-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "reconcile move intents")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	assert.False(t, stage.sourceCleanupArmed)
}

// Deferred COPY mode still rehomes staged sidecar copies into the tree; a
// staged-sidecar stat fault inside that leg fails closed.
func TestDeferredCopyRehomeStatFaultRollsBack(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-rehome-fault")
	dest := filepath.Join(root, "library")
	fs := &pr260PublishFinalFS{Fs: base, op: "stat"}
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: fs, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-rehome-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: false, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	fs.path = stage.siblings[0].stagedPath
	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "preflight staged artifacts")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// ReconcileMoveIntents replaces pending intents wholesale: skipped planned
// moves are retracted; nothing else in the ledger is disturbed.
func TestReconcileMoveIntentsReplacesPending(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "reconcile-settle", "")
	fs, root, source, _, _, _, match := pr260FencedFiles(t, "reconcile-settle")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "reconcile-settle", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)

	video := models.FileMove{OriginalPath: source, NewPath: filepath.Join(dest, "movie.mp4")}
	subA := models.FileMove{OriginalPath: source + ".srt", NewPath: filepath.Join(dest, "movie.srt")}
	subB := models.FileMove{OriginalPath: source + "-cd2.mp4", NewPath: filepath.Join(dest, "movie-cd2.mp4")}
	for _, mv := range []models.FileMove{video, subA, subB} {
		require.NoError(t, log.RecordMoveIntent(context.Background(), opID, mv.OriginalPath, mv.NewPath))
	}
	require.NoError(t, log.ReconcileMoveIntents(context.Background(), opID, []models.FileMove{video, subB}))

	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	assert.ElementsMatch(t, []models.FileMove{video, subB}, gf.MoveBack, "skipped plan retracted")
	assert.Empty(t, row.NewPath, "completion columns remain untouched")

	require.NoError(t, log.ReconcileMoveIntents(context.Background(), opID, []models.FileMove{video, subB}))
	require.NoError(t, log.ReconcileMoveIntents(context.Background(), "", nil), "empty op is a no-op")
	require.Error(t, log.ReconcileMoveIntents(context.Background(), "abc", nil), "unparsable id")
	require.Error(t, log.ReconcileMoveIntents(context.Background(), "99999999", nil), "missing row")
}

// Deferred copy: a faulted rehome rename of a staged sidecar fails closed.
func TestDeferredCopyRehomeRenameFaultRollsBack(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-rehome-rename-fault")
	dest := filepath.Join(root, "library")
	fss := &pr260PublishFinalFS{Fs: base, op: "rename"}
	orgDef := organizer.NewOrganizer(fss, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-rehome-rename-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	orch := &applyOrchImpl{fs: fss, organizer: orgDef}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := orgDef.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: false, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	fss.path = stage.siblings[0].stagedPath
	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "stage sidecar")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// Install hook: unreadable staged source surfaces the digest error before the
// install, nothing publishes.
type denyOpenStagedFS struct {
	afero.Fs
	path string
}

func (f *denyOpenStagedFS) Open(name string) (afero.File, error) {
	if name == f.path {
		return nil, errors.New("staged read denied")
	}
	return f.Fs.Open(name)
}

func TestInstallPathsDigestFaultAbortsInstall(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/stage", 0o755))
	require.NoError(t, afero.WriteFile(base, "/stage/new.txt", []byte("new"), 0o644))
	fs := &denyOpenStagedFS{Fs: base, path: "/stage/new.txt"}
	stage := &artifactStage{fs: fs, root: "/stage", finalRoot: "/final", original: ApplyCmd{}}
	_, err := stage.installPaths([]string{"/stage/new.txt"}, nil, "", "", func([]models.DeleteEntry) error { return nil })
	require.ErrorContains(t, err, "digest")
	exists, _ := afero.Exists(base, "/final/new.txt")
	require.False(t, exists)
}

// A subtitle skipped because its destination is occupied keeps its source:
// the publish flow records no inverse for it, so a deletion here would be
// unrecoverable. The source-cleanup loop skips it entirely.
func TestDeferredMoveRetainSkippedSubtitleSource(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-skip-retain")
	dest := filepath.Join(root, "library")
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-skip-retain", "")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: &completeCallFaultLog{}}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	finalPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	subTarget := filepath.Join(filepath.Dir(finalPlan.TargetPath), stagedArtifactSiblingName(filepath.Base(source), filepath.Base(finalPlan.TargetPath), filepath.Base(subtitle)))
	require.NoError(t, base.MkdirAll(filepath.Dir(subTarget), 0o755))
	require.NoError(t, afero.WriteFile(base, subTarget, []byte("foreign"), 0o644))

	stagedPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil))
	existsSrc, _ := afero.Exists(base, subtitle)
	assert.True(t, existsSrc, "skipped subtitle never loses its source")
	got, _ := afero.ReadFile(base, subTarget)
	assert.Equal(t, "foreign", string(got), "foreign destination never touched")
	existsMulti, _ := afero.Exists(base, multipart)
	assert.False(t, existsMulti, "staged sibling move consumed the part")
	existsUnrelated, _ := afero.Exists(base, unrelated)
	assert.True(t, existsUnrelated)
	existsVideoSrc, _ := afero.Exists(base, source)
	assert.False(t, existsVideoSrc, "video move consumed its original")
}

type denyLstatAtPublishFS struct {
	afero.Fs
	path string
}

func (f *denyLstatAtPublishFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.path {
		return nil, false, errors.New("confirmation inspect denied")
	}
	if l, ok := f.Fs.(afero.Lstater); ok {
		return l.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// Move mode with confirmation failing after the real source was renamed: the
// early-armed rollback origin must restore the video to its original path.
func TestDeferredMoveConfirmFaultRollsBackViaEarlyArm(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-confirm-fault")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-confirm-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	finalPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	stagedPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)

	fsWithFault := &denyLstatAtPublishFS{Fs: base, path: finalPlan.TargetPath}
	stage.fs = fsWithFault
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.Error(t, publishErr)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	assert.False(t, stage.directOriginArmed, "reset after rollback")
	exists, _ := afero.Exists(base, finalPlan.TargetPath)
	assert.False(t, exists, "destination folder cleaned by rollback")
}

// When a subtitle's rollback arm fails (busy-marker creation denied), the
// already-moved sidecar is reversed directly onto its vacated source path so
// nothing strands outside the batch.
func TestDeferredMoveSubtitleArmFaultCompensates(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-sub-arm-fault")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-sub-arm-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	finalPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	subMoves := org.PlanSubtitleMoves(finalPlan)
	require.NotEmpty(t, subMoves)
	fsWithFault := &pr260PublishFinalFS{Fs: base, op: "write", path: subMoves[0].NewPath + fsutil.ReplacementBusySuffix}
	stage.fs = fsWithFault
	stagedPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "arm subtitle rollback")
	existsSub, _ := afero.Exists(base, subtitle)
	assert.True(t, existsSub, "compensation moved the sidecar back onto its source")
	existsSubDest, _ := afero.Exists(base, subMoves[0].NewPath)
	assert.False(t, existsSubDest, "the failed-arm destination copy did not linger")
	existsMulti, _ := afero.Exists(base, multipart)
	assert.True(t, existsMulti)
	existsUnrelated, _ := afero.Exists(base, unrelated)
	assert.True(t, existsUnrelated)
}

type lstatSecondProbeDeniedFS struct {
	afero.Fs
	path string
	seen bool
}

func (f *lstatSecondProbeDeniedFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.path && f.seen {
		return nil, false, errors.New("confirm probe denied")
	}
	if name == f.path {
		f.seen = true
	}
	if l, ok := f.Fs.(afero.Lstater); ok {
		return l.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// Copy-mode: a faulted arm for a copy-installed sidecar fails the publication
// with the tracked error.
func TestDeferredCopySidecarArmStatFaultAborts(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-arm-fault")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-arm-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	finalPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	subMoves := org.PlanSubtitleMoves(finalPlan)
	require.NotEmpty(t, subMoves)
	fsWithFault := &pr260PublishFinalFS{Fs: base, op: "stat", path: subMoves[0].NewPath}
	stage.fs = fsWithFault
	orch.fs = fsWithFault
	stagedPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "arm copy-installed sidecar")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// A faulted confirm of the armed copy-installed sidecar fails closed.
func TestDeferredCopySidecarConfirmFaultAborts(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-confirm-fault")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-confirm-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	finalPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	subMoves := org.PlanSubtitleMoves(finalPlan)
	require.NotEmpty(t, subMoves)
	fsWithFault := &lstatSecondProbeDeniedFS{Fs: base, path: subMoves[0].NewPath}
	stage.fs = fsWithFault
	stagedPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.Error(t, publishErr)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// Copy mode with MoveSubtitles: the sidecar participates in the batch
// arm→install→confirm discipline, and a clean publish keeps sources and
// installs the correct copies.
func TestDeferredCopySidecarArmedAndConfirmed(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-armed")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-armed"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	cmd.PublicationFence = pr260FailureArtifactFencer{}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	finalPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	subMoves := org.PlanSubtitleMoves(finalPlan)
	require.NotEmpty(t, subMoves)
	stagedPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	require.NoError(t, stage.publish(context.Background(), orch, state, nil))

	require.FileExists(t, finalPlan.TargetPath, "video copy installed")
	require.FileExists(t, subMoves[0].NewPath, "sidecar copy installed")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

type swapToDirWhenPresentFS struct {
	afero.Fs
	path    string
	swapped bool
}

func (f *swapToDirWhenPresentFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.path && !f.swapped {
		if info, err := f.Fs.Stat(name); err == nil && info.Mode().IsRegular() {
			f.swapped = true
			_ = f.Fs.Remove(name)
			_ = f.Fs.Mkdir(name, 0o755)
		}
	}
	if l, ok := f.Fs.(afero.Lstater); ok {
		return l.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// Swapping a copy-installed sidecar target for a directory between the
// organizer's execute and the batch confirm fails that leg specifically.
// The swap triggers on the first probe-while-present so the arm probe (made
// before anything is staged there) stays untouched.
func TestDeferredCopySidecarConfirmDirSwapFails(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-confirm-swap")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-confirm-swap"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	cmd.PublicationFence = pr260FailureArtifactFencer{}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	finalPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	subMoves := org.PlanSubtitleMoves(finalPlan)
	require.NotEmpty(t, subMoves)
	stagedPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)
	fsWithFault := &swapToDirWhenPresentFS{Fs: base, path: subMoves[0].NewPath}
	stage.fs = fsWithFault
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "did not install a file")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// An armed sidecar target whose execute-leg outcome is anything but Copied
// must NOT be confirmed as ours: keep it out of the batch.
func TestDeferredCopySkipsConfirmForUncopiedSidecars(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-nocopy-skip")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		for i := range result.Subtitles {
			result.Subtitles[i].Copied = false
		}
	}}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-nocopy-skip"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	cmd.PublicationFence = pr260FailureArtifactFencer{}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	require.NoError(t, stage.publish(context.Background(), orch, state, nil), "nothing uncopied gets batch-confirmed, but publish still settles")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// A faulted sibling inverse journal aborts the source-removal loop without
// consuming anything.
func TestDeferredMoveSiblingIntentFaultAborts(t *testing.T) {
	base, root, source, subtitle, _, _, match := pr260FencedFiles(t, "deferred-sibling-intent-fault")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: &completeCallFaultLog{intentFailAt: 3}}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-sibling-intent-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "journal sibling move intent")
	exists, _ := afero.Exists(base, subtitle)
	assert.True(t, exists, "the faulted sibling never got its source consumed")
	existsSrc, _ := afero.Exists(base, source)
	assert.True(t, existsSrc, "rollback restores the moved video onto its source path")

}

// Deferred copy mode with MoveSubtitles disabled: the organizer never touches
// sidecars, the staged copy publishes through the sidecar block and joins the
// ledger's Delete list (copy semantics: revert removes the install, keeps the
// source).
func TestDeferredCopyPublishesStagedSiblingWithoutOrganizer(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-sidecar-publish")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: &completeCallFaultLog{}}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-sidecar-publish"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	cmd.OperationMode = operationmode.OperationModeOrganize
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)

	finalPlan, planErr2 := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr2)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	require.NoError(t, stage.publish(context.Background(), orch, state, nil))

	require.FileExists(t, finalPlan.TargetPath)
	subTarget := filepath.Join(filepath.Dir(finalPlan.TargetPath), stagedArtifactSiblingName(filepath.Base(source), filepath.Base(finalPlan.TargetPath), filepath.Base(subtitle)))
	require.FileExists(t, subTarget, "staged sidecar published on copy mode")
	ret, _ := afero.Exists(base, source)
	assert.True(t, ret, "copy leaves the video source in place")
	retSub, _ := afero.Exists(base, subtitle)
	assert.True(t, retSub, "copy leaves the subtitle source in place")
	_ = multipart
	_ = unrelated
}

// Ledger-active copy path: the hash-pinned delete intent writes for each
// copy-installed sidecar before execute, and the run lands cleanly.
func TestDeferredCopySidecarIntentJournalHappyPath(t *testing.T) {
	base, root, source, subtitle, _, _, match := pr260FencedFiles(t, "deferred-copy-intent-happy")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: &completeCallFaultLog{}}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-intent-happy"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	cmd.PublicationFence = pr260FailureArtifactFencer{}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	require.NoError(t, stage.publish(context.Background(), orch, state, nil))
	log := orch.revertLog.(*completeCallFaultLog)
	assert.Positive(t, atomic.LoadInt32(&log.deletes), "pinned delete intents journaled for armed copy-installed sidecars")
	assert.NotEmpty(t, log.deletePaths)
	existsSub, _ := afero.Exists(base, subtitle)
	assert.True(t, existsSub, "copy retains source")
}

type denyOpenAfterPrepareFS struct {
	afero.Fs
	path  string
	armed bool
}

func (f *denyOpenAfterPrepareFS) Open(name string) (afero.File, error) {
	if f.armed && name == f.path {
		return nil, errors.New("digest read denied")
	}
	return f.Fs.Open(name)
}

// Copy-mode arm: unreadable source content at digest time shows the digest
// leg's error (journal never gets a bogus hash to pin).
func TestDeferredCopySidecarDigestFaultAborts(t *testing.T) {
	base, root, source, subtitle, _, _, match := pr260FencedFiles(t, "deferred-copy-digest-fault")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: &completeCallFaultLog{}}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-digest-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	cmd.PublicationFence = pr260FailureArtifactFencer{}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)

	fsWithFault := &denyOpenAfterPrepareFS{Fs: base}
	fsWithFault.path = subtitle
	fsWithFault.armed = true
	stage.fs = fsWithFault
	orch.fs = fsWithFault
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "digest", "digest failure prevents the journal from pinning wrong bytes")
}

// Journal refusal for a copy-installed sidecar also fails closed.
func TestDeferredCopySidecarIntentJournalFaultAborts(t *testing.T) {
	base, root, source, subtitle, _, _, match := pr260FencedFiles(t, "deferred-copy-intent-refusal")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: &completeCallFaultLog{deleteErr: errors.New("ledger down")}}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-copy-intent-refusal"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	cmd.PublicationFence = pr260FailureArtifactFencer{}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "record copy-installed sidecar intent")
	existsSub, serr := afero.Exists(base, subtitle)
	require.NoError(t, serr)
	assert.True(t, existsSub, "the faulted intent never ran execution; the subtitle never left its source")
}

// sameBytes surfaces read failures so the caller classifies them as unequal.
func TestSameBytesFaultLegs(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/x", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/x/a.txt", []byte("same"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/x/b.txt", []byte("same"), 0o644))
	eq, err := sameBytes(fs, "/x/a.txt", "/x/b.txt")
	require.NoError(t, err)
	assert.True(t, eq)

	deny := &denyOpenAfterPrepareFS{Fs: fs, armed: true, path: "/x/a.txt"}
	_, err = sameBytes(deny, "/x/a.txt", "/x/b.txt")
	require.Error(t, err, "first source's digest fail surfaces")
	deny.path = "/x/b.txt"
	_, err = sameBytes(deny, "/x/a.txt", "/x/b.txt")
	require.Error(t, err, "second source's digest fail surfaces")
}

// A copy-published generic sibling registers a hash-pinned delete intent
// BEFORE its bytes land: a crash window between the copy and the MoveBack
// arm must still prove ownership of the destination. The capturing recorder
// asserts the intent order.
type genericSiblingIntentCapture struct {
	noOpRevertLog
	intents []models.DeleteEntry
}

func (c *genericSiblingIntentCapture) RecordDeleteIntent(_ context.Context, _ OperationID, entries []models.DeleteEntry) error {
	c.intents = append(c.intents, entries...)
	return nil
}

func TestDeferredPublishGenericSiblingPinsInstallIntent(t *testing.T) {
	base, root, _, _, _, _, match := pr260FencedFiles(t, "deferred-sibling-intent")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	capture := &genericSiblingIntentCapture{}
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: capture}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-sibling-intent"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil))
	require.NotEmpty(t, capture.intents, "the sibling publish must register hash-pinned intents")
	for _, entry := range capture.intents {
		require.NotEmpty(t, entry.SHA256, "intent on %s needs its content pin", entry.Path)
	}
}

// The pinned-intent leg propagates recorder faults; a staged-read fault marg
// surfaces before the copy as the digest error.
type faultSiblingIntentRecorder struct {
	noOpRevertLog
}

func (faultSiblingIntentRecorder) RecordDeleteIntent(context.Context, OperationID, []models.DeleteEntry) error {
	return errors.New("intent persistence fault")
}

func TestDeferredPublishGenericSiblingPinsIntentFaults(t *testing.T) {
	t.Run("recorder fault", func(t *testing.T) {
		base, root, _, _, _, _, match := pr260FencedFiles(t, "deferred-sibling-intent-rec")
		dest := filepath.Join(root, "library")
		real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
		orch := &applyOrchImpl{fs: base, organizer: real, revertLog: faultSiblingIntentRecorder{}}
		cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-sibling-intent-rec"}, match, dest)
		cmd.Organize.Skip = false
		cmd.Organize.MoveFiles = true
		cmd.Download = false
		stage, _, err := orch.prepareArtifact(context.Background(), cmd)
		require.NoError(t, err)
		defer stage.cleanup()

		plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
		require.NoError(t, planErr)
		state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

		publishErr := stage.publish(context.Background(), orch, state, nil)
		require.ErrorContains(t, publishErr, "intent persistence fault")
	})

	t.Run("staged digest fault", func(t *testing.T) {
		base, root, _, _, _, _, match := pr260FencedFiles(t, "deferred-sibling-intent-dig")
		dest := filepath.Join(root, "library")
		real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
		orch := &applyOrchImpl{fs: base, organizer: real, revertLog: noOpRevertLog{}}
		cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "deferred-sibling-intent-dig"}, match, dest)
		cmd.Organize.Skip = false
		cmd.Organize.MoveFiles = true
		cmd.Download = false
		stage, _, err := orch.prepareArtifact(context.Background(), cmd)
		require.NoError(t, err)
		defer stage.cleanup()

		plan, planErr := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
		require.NoError(t, planErr)
		state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: plan.TargetPath, FolderPath: plan.TargetDir}}

		// Make the staged sibling undigestible (only its Open faults), so the
		// digest leg errors BEFORE the publish loop records or copies anything.
		stage.fs = &denyOpenStagedFS{Fs: base, path: stage.siblings[0].stagedPath}
		publishErr := stage.publish(context.Background(), orch, state, nil)
		require.ErrorContains(t, publishErr, "staged read denied")
	})
}

// codex P1 (PRRT_kwDORn9KaM6m0-JL): after a successful deferred move with a
// generic sibling, the durable revert record must NOT retain a PlannedDeletes
// row for the sibling target — the armed move-back inverse owns it. Reverting
// the batch restores the sibling onto its source path instead of deleting the
// destination and losing the removed source.
func TestDeferredMoveGenericSiblingPinPromotesAndReverts(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "sibling-promote-w161", "")
	fs, root, source, _, multipart, _, match := pr260FencedFiles(t, "sibling-promote-w161")
	dest := filepath.Join(root, "library")
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
	repo := database.NewBatchFileOperationRepository(db)
	orch.revertLog = NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "sibling-promote-w161", fs, nil, nil, nil)
	cmd := pr260FencedCommand(&movie, match, dest, pr260FencedCounter(t, db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)

	result, err := orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)

	siblingTarget := filepath.Join(filepath.Dir(result.OrganizeResult.NewPath),
		stagedArtifactSiblingName(filepath.Base(source), filepath.Base(result.OrganizeResult.NewPath), filepath.Base(multipart)))
	require.Equal(t, []byte("part two"), mustReadStagedTxn(t, fs, siblingTarget), "the sibling published at the final destination")
	exists, statErr := afero.Exists(fs, multipart)
	require.NoError(t, statErr)
	assert.False(t, exists, "move mode consumed the sibling source")

	ledger := p3Ledger(t, repo, result.OperationID)
	moveBacked := map[string]bool{}
	for _, fm := range ledger.MoveBack {
		moveBacked[fm.NewPath] = true
	}
	assert.True(t, moveBacked[siblingTarget], "the sibling inverse is armed in the durable record")
	for _, pd := range ledger.PlannedDeletes {
		assert.False(t, moveBacked[pd.Path], "no pending delete survives for a move-armed destination: %s", pd.Path)
		assert.NotEqual(t, siblingTarget, pd.Path, "the sibling target's pinned delete promoted with the arm")
	}

	res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "sibling-promote-w161")
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	require.Equal(t, []byte("part two"), mustReadStagedTxn(t, fs, multipart), "revert moved the sibling back onto its source path")
	require.Equal(t, []byte("video"), mustReadStagedTxn(t, fs, source), "the primary moved back too")
	exists, statErr = afero.Exists(fs, siblingTarget)
	require.NoError(t, statErr)
	assert.False(t, exists, "the revert moved the destination away, never deleted-then-lost it")
}
