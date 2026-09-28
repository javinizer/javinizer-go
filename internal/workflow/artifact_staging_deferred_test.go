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

	_, err = stage.installTree("", "", nil, "", "")
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
				log = &completeCallFaultLog{intentFailAt: 2}
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
				require.ErrorContains(t, publishErr, "journal subtitle move intent")
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
