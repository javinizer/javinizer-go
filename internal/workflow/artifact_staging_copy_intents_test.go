package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Subtitle-lane organizer used by the deferred copy-intent tests.
func copyIntentOrganizer(base afero.Fs) *organizer.Organizer {
	return organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
}

func copyIntentCommand(movie *models.Movie, match models.FileMatchInfo, dest string) ApplyCmd {
	cmd := pr260ArtifactFailureCommand(movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	return cmd
}

func copyIntentPlans(t *testing.T, real *organizer.Organizer, stage *artifactStage, match models.FileMatchInfo, source, dest string) (staged, final *organizer.OrganizePlan) {
	t.Helper()
	staged, err := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, OperationMode: stage.original.OperationMode})
	require.NoError(t, err)
	final, err = real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, OperationMode: stage.original.OperationMode})
	require.NoError(t, err)
	return staged, final
}

// The deferred primary copy/link leg must journal a delete intent for
// plan.TargetPath BEFORE ExecuteOrganizePlan runs — an exit between the copy
// landing and the final completion otherwise leaves a copy no journal can
// attribute. The pin authenticates the bytes about to land WITHOUT streaming
// the multi-gigabyte source in advance (codex P2, PRRT_kwDORn9KaM6nBUrF):
// the size + bounded head+tail interim shape (fsutil.PartialCopyDigest), and
// it comes after the sidecar pins.
func TestDeferredCopyPrimaryPinPrecedesExecute(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-primary-pin-order")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: &pr260PublicationFaultOrganizer{Organizer: real, failExecute: true}, revertLog: ledger}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-primary-pin-order"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, finalPlan := copyIntentPlans(t, real, stage, match, source, dest)
	subMoves := real.PlanSubtitleMoves(finalPlan)
	require.Len(t, subMoves, 1)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "final execution denied")

	wantSub, err := artifactDigest(base, subtitle)
	require.NoError(t, err)
	wantInfo, wantVideoPartial, err := fsutil.PartialCopyDigest(base, source)
	require.NoError(t, err)
	require.Equal(t, []models.DeleteEntry{
		{Path: subMoves[0].NewPath, SHA256: wantSub},
		{Path: finalPlan.TargetPath, CopySize: wantInfo.Size(), CopyPartialSHA256: wantVideoPartial},
	}, ledger.deletePaths, "sidecar pin, then the interim partial primary pin — both journaled BEFORE execute can land bytes, and no full pre-read")
	assert.Zero(t, atomic.LoadInt32(&ledger.seals), "no seal before execute ever returned a stream digest")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
}

// The primary pin digests the real source (the bytes execute will copy): an
// unreadable source must abort the pin leg before any publish.
func TestDeferredCopyPrimaryPinDigestFaultAborts(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-primary-pin-digest")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: &completeCallFaultLog{}}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-primary-pin-digest"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, _ := copyIntentPlans(t, real, stage, match, source, dest)
	stage.fs = &denyOpenAfterPrepareFS{Fs: base, path: source, armed: true}
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "pin deferred primary copy digest")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
}

// A journal refusal on the primary pin aborts the publish before execute ever
// runs (MoveSubtitles disabled: the primary pin is the first delete intent).
func TestDeferredCopyPrimaryPinJournalFaultAborts(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-primary-pin-journal")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{deleteErr: errors.New("ledger down"), deleteFailAt: 1}
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: ledger}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-primary-pin-journal"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, _ := copyIntentPlans(t, real, stage, match, source, dest)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "record deferred primary copy intent")
	require.Equal(t, int32(1), atomic.LoadInt32(&ledger.deletes), "the primary pin is the first delete intent in this shape")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
}

// Once the confirmed publish graduates the primary into a user-owned install,
// the durable pins settle in one journal transaction: the primary's pin is
// retracted (this row's revert retains installed copy/link primaries) and only
// confirmed-copied sidecars keep theirs.
func TestDeferredCopyReconcilesDeleteIntentsAtGraduation(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-delete-reconcile")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: ledger}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-delete-reconcile"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, finalPlan := copyIntentPlans(t, real, stage, match, source, dest)
	subMoves := real.PlanSubtitleMoves(finalPlan)
	require.Len(t, subMoves, 1)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil))
	assert.Equal(t, int32(1), atomic.LoadInt32(&ledger.deleteReconciles), "one settle transaction after the primary confirm")
	assert.Equal(t, []string{subMoves[0].NewPath}, ledger.deleteKeepCaptured, "only the confirmed-copied sidecar keeps its pin")
	require.FileExists(t, finalPlan.TargetPath)
	require.FileExists(t, subMoves[0].NewPath)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// A failed reconcile rolls the whole publication back: installed copies are
// unlinked, sources retained, and the pins stay durable for recovery.
func TestDeferredCopyReconcileDeleteIntentsFaultRollsBack(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-delete-reconcile-fault")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: &completeCallFaultLog{deleteReconcileErr: errors.New("reconcile down")}}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-delete-reconcile-fault"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, _ := copyIntentPlans(t, real, stage, match, source, dest)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "reconcile copy-installed delete intents")
	pr260AssertNoFinals(t, base, dest)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

type copyIntentE2EEnv struct {
	db        *database.DB
	fs        afero.Fs
	repo      *database.BatchFileOperationRepository
	orch      *applyOrchImpl
	org       *organizer.Organizer
	movie     models.Movie
	source    string
	subtitle  string
	multipart string
	unrelated string
	match     models.FileMatchInfo
	dest      string
	jobID     string
}

// Full-pipeline copy-mode apply with a subtitle-enabled organizer and a
// DB-backed revert log: the durable ledger outcome is the assertion target.
func setupCopyIntentE2E(t *testing.T, slug string) *copyIntentE2EEnv {
	t.Helper()
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, slug, "")
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, slug)
	engine := template.NewEngine()
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, engine, nil)
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), slug, fs, nil, nil, nil)
	nameCfg := nfo.NFONameConfig{FilenameTemplate: "<ACTRESS>.nfo", FirstNameOrder: true}
	orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{NFONameCfg: nameCfg}, engine, log, nil, nil)
	return &copyIntentE2EEnv{
		db: db, fs: fs, repo: repo, orch: orch, org: org, movie: movie,
		source: source, subtitle: subtitle, multipart: multipart, unrelated: unrelated,
		match: match, dest: filepath.Join(root, "library"), jobID: slug,
	}
}

// End-to-end: after a graduated deferred copy the durable row carries NO
// pending pin for the primary, the copied sidecar graduated into the plain
// Delete ledger, and a revert removes only the row's own artifacts while
// retaining the installed primary (the user's copy) and every source.
func TestDeferredCopyGraduatedPrimarySurvivesRevert(t *testing.T) {
	env := setupCopyIntentE2E(t, "copy-grad-revert")
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	subTarget := ""
	for _, sr := range result.OrganizeResult.Subtitles {
		if sr.Copied && filepath.Ext(sr.NewPath) == ".srt" {
			subTarget = sr.NewPath
		}
	}
	require.NotEmpty(t, subTarget, "the subtitle lane reported a copied install")
	require.FileExists(t, subTarget)

	ledger := p3Ledger(t, env.repo, result.OperationID)
	for _, pd := range ledger.PlannedDeletes {
		assert.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed at publish")
		assert.NotEqual(t, subTarget, pd.Path, "the copied sidecar graduated out of pending pins")
	}
	assert.Contains(t, ledger.Delete, subTarget, "copied sidecar graduated into the plain delete ledger")
	assert.Empty(t, ledger.MoveBack, "copy mode journals no move-back")

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	require.FileExists(t, video, "copy-mode revert retains the installed primary (the user's copy)")
	subGone, statErr := afero.Exists(env.fs, subTarget)
	require.NoError(t, statErr)
	assert.False(t, subGone, "revert deletes the copied sidecar installed by this row")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}

// End-to-end: a subtitle target occupied between the plan and the execute
// reports Skipped — its durable pin must be retracted at settle time so the
// occupant (which happens to carry the same bytes as the source subtitle)
// never hash-matches into deletion on a later revert.
func TestDeferredCopySkippedSidecarPinRetractedBeforeRevert(t *testing.T) {
	env := setupCopyIntentE2E(t, "copy-skip-retract")
	probePlan, planErr := env.org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, planErr)
	subMoves := env.org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 1)
	subTarget := subMoves[0].NewPath

	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, preExecute: func(*organizer.OrganizePlan) {
		// The occupant lands after the pin/arm and before execution — the exact
		// interleaving the pin was written to cover, with the source's own bytes.
		require.NoError(t, env.fs.MkdirAll(filepath.Dir(subTarget), 0o755))
		require.NoError(t, afero.WriteFile(env.fs, subTarget, []byte("subtitle"), 0o644))
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	skipped := false
	for _, sr := range result.OrganizeResult.Subtitles {
		if sr.Skipped && filepath.Clean(sr.NewPath) == filepath.Clean(subTarget) {
			skipped = true
		}
	}
	require.True(t, skipped, "the occupant forced the organizer's skip outcome")

	ledger := p3Ledger(t, env.repo, result.OperationID)
	for _, pd := range ledger.PlannedDeletes {
		assert.NotEqual(t, subTarget, pd.Path, "the skipped sidecar's pin was retracted at settle")
		assert.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed")
	}
	assert.NotContains(t, ledger.Delete, subTarget)

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	bytes, readErr := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, readErr, "the same-content occupant survives the revert untouched")
	assert.Equal(t, "subtitle", string(bytes))
	require.FileExists(t, video, "the installed primary is retained")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}

// Byte-identical pre-existing artifacts are foreign to this apply: the
// install skips them, so they must never enter the delete-ledger feed
// (state.nfoPath / state.downloadPaths). Only destinations this apply truly
// installed stay ledgered — in every overwrite posture, without registering a
// replacement either.
func TestPublishExcludesByteIdenticalArtifactsFromDeleteLedger(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overwrite bool
	}{
		{name: "default"},
		{name: "overwrite-media", overwrite: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			slug := "identical-ledger-" + tc.name
			movie := pr260FencedMovie(t, db, slug, "")
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, slug)
			dest := filepath.Join(root, "library")
			real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			repo := database.NewBatchFileOperationRepository(db)
			log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), slug, base, nil, nil, nil)
			orch := &applyOrchImpl{fs: base, organizer: real, revertLog: log}
			cmd := copyIntentCommand(&movie, match, dest)
			cmd.OverwriteExistingMedia = tc.overwrite
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
			require.NoError(t, err)

			stagedPlan, finalPlan := copyIntentPlans(t, real, stage, match, source, dest)
			stagedNfo := filepath.Join(stagedPlan.TargetDir, "movie.nfo")
			stagedIdenticalPoster := filepath.Join(stagedPlan.TargetDir, "poster.jpg")
			stagedNewPoster := filepath.Join(stagedPlan.TargetDir, "new.jpg")
			require.NoError(t, base.MkdirAll(stagedPlan.TargetDir, 0o755))
			require.NoError(t, afero.WriteFile(base, stagedNfo, []byte("metadata"), 0o644))
			require.NoError(t, afero.WriteFile(base, stagedIdenticalPoster, []byte("poster-bytes"), 0o644))
			require.NoError(t, afero.WriteFile(base, stagedNewPoster, []byte("new-poster"), 0o644))

			finalDir := filepath.Dir(finalPlan.TargetPath)
			finalNfo := filepath.Join(finalDir, "movie.nfo")
			finalIdenticalPoster := filepath.Join(finalDir, "poster.jpg")
			finalNewPoster := filepath.Join(finalDir, "new.jpg")
			require.NoError(t, base.MkdirAll(finalDir, 0o755))
			require.NoError(t, afero.WriteFile(base, finalNfo, []byte("metadata"), 0o644))
			require.NoError(t, afero.WriteFile(base, finalIdenticalPoster, []byte("poster-bytes"), 0o644))

			state := &applyPipelineState{
				operationID:    opID,
				organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir},
				nfoPath:        stagedNfo,
				downloadPaths:  []string{stagedIdenticalPoster, stagedNewPoster},
			}
			require.NoError(t, stage.publish(context.Background(), orch, state, nil))

			assert.Empty(t, state.nfoPath, "byte-identical pre-existing NFO leaves the delete-ledger feed")
			assert.Equal(t, []string{finalNewPoster}, state.downloadPaths, "only the actually-installed download stays ledgered")
			for path, want := range map[string]string{finalNfo: "metadata", finalIdenticalPoster: "poster-bytes", finalNewPoster: "new-poster"} {
				bytes, readErr := afero.ReadFile(base, path)
				require.NoError(t, readErr, path)
				assert.Equal(t, want, string(bytes), path)
			}

			require.NoError(t, log.Complete(context.Background(), opID, &ApplyResult{OrganizeResult: state.organizeResult, NFOPath: state.nfoPath, DownloadPaths: state.downloadPaths, OperationID: opID}))
			ledger := p3Ledger(t, repo, opID)
			assert.NotContains(t, ledger.Delete, finalNfo, "revert must never condemn the pre-existing identical NFO")
			assert.NotContains(t, ledger.Delete, finalIdenticalPoster, "revert must never condemn the pre-existing identical poster")
			assert.Contains(t, ledger.Delete, finalNewPoster)
			for _, pd := range ledger.PlannedDeletes {
				assert.NotEqual(t, finalNfo, pd.Path, "identical NFO was never pin-journaled either")
				assert.NotEqual(t, finalIdenticalPoster, pd.Path, "identical poster was never pin-journaled either")
				assert.NotEqual(t, finalPlan.TargetPath, pd.Path, "the graduated primary pin was consumed")
			}
			for _, rp := range ledger.Replacements {
				assert.NotEqual(t, finalNfo, rp.Destination, "identical bytes never register a replacement")
				assert.NotEqual(t, finalIdenticalPoster, rp.Destination, "identical bytes never register a replacement")
			}
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
		})
	}
}
