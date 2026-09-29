package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verifiedStagePublish drives prepareArtifact + publish against a real
// organizer, mirroring the deferred-flow harness in artifact_staging_deferred_test.go.
func verifiedStagePublish(t *testing.T, orch *applyOrchImpl, org organizer.OrganizerInterface, base afero.Fs, root, source, dest string, match models.FileMatchInfo, cmd ApplyCmd) (*artifactStage, *applyPipelineState, error) {
	t.Helper()
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	executor, ok := org.(artifactPlanExecutor)
	require.True(t, ok)
	stagedPlan, planErr := executor.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	return stage, state, stage.publish(context.Background(), orch, state, nil)
}

func assertNoVacResidue(t *testing.T, fs afero.Fs, dir string) {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, strings.Contains(entry.Name(), ".vac."), "bound-take residue in %s: %s", dir, entry.Name())
	}
}

// F1 (codex P1, PRRT_kwDORn9KaM6m9ae4), move leg: the source directory entry
// is rename-swapped AT ExecuteOrganizePlan — after the pre-execute identity
// gate passed. The bound publication must refuse rather than consume the
// replacement, and the restore must return the foreign bytes to the source
// name untouched.
func TestDeferredMoveRefusesSourceSwappedInsideExecute(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "verified-move-swap")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	aside := filepath.Join(root, "incoming", "swapped-aside.mp4")
	fault := &pr260PublicationFaultOrganizer{Organizer: real, preExecute: func(*organizer.OrganizePlan) {
		_ = base.Rename(source, aside)
		_ = afero.WriteFile(base, source, []byte("replacement video"), 0o644)
	}}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-move-swap"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.ErrorIs(t, publishErr, fsutil.ErrTakeAsideForeign, "the bound consumption refuses the swapped entry")
	require.ErrorIs(t, publishErr, errArtifactSourceChanged, "the admission class rides the refusal")
	assert.False(t, fsutil.PublishRefusal(publishErr), "a pre-publication admission refusal never classifies as a publish refusal")
	assert.False(t, stage.sourceCleanupArmed)
	got, err := afero.ReadFile(base, aside)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got), "the admitted object is never touched")
	got, err = afero.ReadFile(base, source)
	require.NoError(t, err)
	assert.Equal(t, "replacement video", string(got), "the replacement rides back onto the source name byte-intact")
	pr260AssertNoFinals(t, base, dest)
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, kept := range []string{subtitle, multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}

// F1, copy leg: the swap also refuses the moveFiles=false publication before
// any staged byte reaches the destination.
func TestDeferredCopyRefusesSourceSwappedInsideExecute(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "verified-copy-swap")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	aside := filepath.Join(root, "incoming", "swapped-aside.mp4")
	fault := &pr260PublicationFaultOrganizer{Organizer: real, preExecute: func(*organizer.OrganizePlan) {
		_ = base.Rename(source, aside)
		_ = afero.WriteFile(base, source, []byte("replacement video"), 0o644)
	}}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-copy-swap"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.ErrorIs(t, publishErr, fsutil.ErrTakeAsideForeign)
	require.ErrorIs(t, publishErr, errArtifactSourceChanged)
	pr260AssertNoFinals(t, base, dest)
	got, err := afero.ReadFile(base, source)
	require.NoError(t, err)
	assert.Equal(t, "replacement video", string(got), "the copy leg opens and refuses the replacement without staging it")
	got, err = afero.ReadFile(base, aside)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got))
	for _, kept := range []string{subtitle, multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}

func verifiedMemFixture(t *testing.T, slug string) (afero.Fs, string, string, string, string, string, models.FileMatchInfo) {
	t.Helper()
	base := afero.NewMemMapFs()
	root := filepath.FromSlash("/" + slug)
	sourceDir := filepath.Join(root, "incoming")
	source := filepath.Join(sourceDir, "movie.mp4")
	subtitle := filepath.Join(sourceDir, "movie.srt")
	multipart := filepath.Join(sourceDir, "movie-cd2.mp4")
	unrelated := filepath.Join(sourceDir, "do-not-touch.txt")
	require.NoError(t, base.MkdirAll(sourceDir, 0o755))
	require.NoError(t, afero.WriteFile(base, source, []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(base, subtitle, []byte("subtitle"), 0o644))
	require.NoError(t, afero.WriteFile(base, multipart, []byte("part two"), 0o644))
	require.NoError(t, afero.WriteFile(base, unrelated, []byte("unrelated"), 0o644))
	match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4"}
	return base, root, source, subtitle, multipart, unrelated, match
}

// plantAfterSourceRenameFS plants a foreign file at the source name as soon
// as the take-aside rename has consumed it — the mid-composite F1 window.
type plantAfterSourceRenameFS struct {
	afero.Fs
	sourcePath string
	plant      []byte
	fired      bool
}

func (f *plantAfterSourceRenameFS) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.fired && filepath.Clean(oldname) == filepath.Clean(f.sourcePath) {
		f.fired = true
		_ = afero.WriteFile(f.Fs, f.sourcePath, f.plant, 0o644)
	}
	return err
}

// F1, move leg, mid-composite window: the plant lands after the claim take
// re-proved the admitted object. The publication still lands the ADMITTED
// bytes and the plant is left at the source name, never consumed.
func TestDeferredMovePublishesAdmittedDespitePostClaimPlant(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := verifiedMemFixture(t, "verified-plant-move")
	dest := filepath.Join(root, "library")
	faultFS := &plantAfterSourceRenameFS{Fs: base, sourcePath: source, plant: []byte("planted foreign video")}
	org := organizer.NewOrganizer(faultFS, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: faultFS, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-plant-move"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, org, faultFS, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	require.True(t, faultFS.fired, "the plant actually landed mid-composite")
	target := filepath.Join(dest, "movie", "movie.mp4")
	got, err := afero.ReadFile(base, target)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got), "the bound publication delivered the admitted identity")
	assert.Equal(t, target, state.organizeResult.NewPath)
	got, err = afero.ReadFile(base, source)
	require.NoError(t, err)
	assert.Equal(t, "planted foreign video", string(got), "the foreign plant is preserved, never consumed")
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, gone := range []string{subtitle, multipart} {
		exists, serr := afero.Exists(base, gone)
		require.NoError(t, serr)
		assert.False(t, exists, "admitted siblings publish per the ordinary flow: %s", gone)
	}
	exists, _ := afero.Exists(base, unrelated)
	assert.True(t, exists)
}

// swapAtStagingDrawFS rename-swaps the source once the publish's staging draw
// begins — after the verified copy opened and proved the source handle.
type swapAtStagingDrawFS struct {
	afero.Fs
	sourcePath string
	replBytes  []byte
	fired      bool
}

func (f *swapAtStagingDrawFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if !f.fired && strings.Contains(name, ".nrstg.") {
		f.fired = true
		_ = f.Fs.Remove(f.sourcePath)
		_ = afero.WriteFile(f.Fs, f.sourcePath, f.replBytes, 0o644)
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// F1, copy leg, descriptor pinning: the source entry is swapped after the
// verified open. The stream reads the pinned handle, so the destination still
// receives the admitted bytes and the replacement entry is never read for the
// publish.
func TestDeferredCopyPublishesAdmittedBytesDespitePostOpenSwap(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := verifiedMemFixture(t, "verified-swap-copy")
	dest := filepath.Join(root, "library")
	faultFS := &swapAtStagingDrawFS{Fs: base, sourcePath: source, replBytes: []byte("replacement video — different length")}
	org := organizer.NewOrganizer(faultFS, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: faultFS, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-swap-copy"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, org, faultFS, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	require.True(t, faultFS.fired, "the swap actually landed after the verified open")
	target := filepath.Join(dest, "movie", "movie.mp4")
	got, err := afero.ReadFile(base, target)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got), "the pinned handle delivered the admitted bytes despite the swap")
	assert.Equal(t, target, state.organizeResult.NewPath)
	got, err = afero.ReadFile(base, source)
	require.NoError(t, err)
	assert.Equal(t, "replacement video — different length", string(got), "the replacement is retained untouched")
	for _, kept := range []string{subtitle, multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}

// probeInjectOrganizer adds a NEW subtitle to the source directory right
// after the first PlanSubtitleMoves probe — the F2 probe→execute window
// (codex P2, PRRT_kwDORn9KaM6m9afD).
type probeInjectOrganizer struct {
	*organizer.Organizer
	fs         afero.Fs
	injectPath string
	injectBody []byte
	probeCalls int
}

func (o *probeInjectOrganizer) PlanSubtitleMoves(plan *organizer.OrganizePlan) []models.SubtitleMove {
	moves := o.Organizer.PlanSubtitleMoves(plan)
	o.probeCalls++
	if o.probeCalls == 1 {
		_ = afero.WriteFile(o.fs, o.injectPath, o.injectBody, 0o644)
	}
	return moves
}

// F2, move leg: a subtitle appearing after the intent-journaling probe is
// never installed — the apply publishes the admitted subset only, the late
// source is retained, and the reconciled journal keep-set never names it.
func TestDeferredMoveFreezesSubtitleSetAtProbe(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := verifiedMemFixture(t, "freeze-sub-move")
	dest := filepath.Join(root, "library")
	late := filepath.Join(filepath.Dir(source), "movie.jpn.srt")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	fault := &probeInjectOrganizer{Organizer: org, fs: base, injectPath: late, injectBody: []byte("late subtitle")}
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: fault, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "freeze-sub-move"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, org, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr, "the frozen-out late subtitle is a skip, not a failure")
	require.GreaterOrEqual(t, fault.probeCalls, 2, "probe + revalidation both ran")
	var admittedSeat, lateSeat *organizer.SubtitleResult
	for i := range state.organizeResult.Subtitles {
		switch filepath.Clean(state.organizeResult.Subtitles[i].OriginalPath) {
		case filepath.Clean(subtitle):
			admittedSeat = &state.organizeResult.Subtitles[i]
		case filepath.Clean(late):
			lateSeat = &state.organizeResult.Subtitles[i]
		}
	}
	require.NotNil(t, admittedSeat, "the admitted subtitle executed")
	assert.True(t, admittedSeat.Moved)
	require.NotNil(t, lateSeat, "the late subtitle is classified so the skip machinery absorbs it")
	assert.True(t, lateSeat.Skipped)
	assert.False(t, lateSeat.Moved || lateSeat.Copied, "no install the journal never armed")
	targetDir := filepath.Join(dest, "movie")
	got, err := afero.ReadFile(base, filepath.Join(targetDir, "movie.srt"))
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got), "the admitted subtitle moved into the library")
	exists, _ := afero.Exists(base, filepath.Join(targetDir, "movie.jpn.srt"))
	assert.False(t, exists, "the frozen-out endpoint never lands")
	got, err = afero.ReadFile(base, late)
	require.NoError(t, err)
	assert.Equal(t, "late subtitle", string(got), "the late source is retained untouched")
	assert.Equal(t, 2, len(ledger.keepCaptured), "outcome reconciliation keeps exactly video+admitted subtitle")
	assert.Contains(t, ledger.keepCaptured, models.FileMove{OriginalPath: source, NewPath: filepath.Join(targetDir, "movie.mp4")})
	assert.Contains(t, ledger.keepCaptured, models.FileMove{OriginalPath: subtitle, NewPath: filepath.Join(targetDir, "movie.srt")})
	existsMulti, _ := afero.Exists(base, multipart)
	assert.False(t, existsMulti, "the staged sibling video moves per the ordinary flow")
	existsUnrelated, _ := afero.Exists(base, unrelated)
	assert.True(t, existsUnrelated)
}

// F2, copy leg: the same freeze holds when the subtitle lane copy-installs —
// the armed/journaled delete intents cover the admitted subset only.
func TestDeferredCopyFreezesSubtitleSetAtProbe(t *testing.T) {
	base, root, source, _, multipart, unrelated, match := verifiedMemFixture(t, "freeze-sub-copy")
	dest := filepath.Join(root, "library")
	late := filepath.Join(filepath.Dir(source), "movie.jpn.srt")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	fault := &probeInjectOrganizer{Organizer: org, fs: base, injectPath: late, injectBody: []byte("late subtitle")}
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: fault, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "freeze-sub-copy"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, org, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	targetDir := filepath.Join(dest, "movie")
	exists, _ := afero.Exists(base, filepath.Join(targetDir, "movie.jpn.srt"))
	assert.False(t, exists, "the frozen-out endpoint never lands")
	got, err := afero.ReadFile(base, late)
	require.NoError(t, err)
	assert.Equal(t, "late subtitle", string(got))
	got, err = afero.ReadFile(base, filepath.Join(targetDir, "movie.srt"))
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got))
	for _, keep := range ledger.deleteKeepCaptured {
		assert.NotContains(t, keep, ".jpn", "no durable pin ever names the frozen-out endpoint")
	}
	assert.Contains(t, ledger.deleteKeepCaptured, filepath.Join(targetDir, "movie.srt"), "the admitted copy-installed sidecar keeps its pin")
	for _, kept := range []string{source, multipart, unrelated} {
		e, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, e, kept)
	}
}

// deferredSourceProof: nil for unpinned stages; the closure applies the
// admission identity matcher verbatim.
func TestDeferredSourceProofUnits(t *testing.T) {
	assert.Nil(t, (*artifactStage)(nil).deferredSourceProof())
	assert.Nil(t, (&artifactStage{}).deferredSourceProof())

	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/src/m.mp4", []byte("mm"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/src/other.mp4", []byte("other"), 0o644))
	info, err := base.Stat("/src/m.mp4")
	require.NoError(t, err)
	other, err := base.Stat("/src/other.mp4")
	require.NoError(t, err)
	stage := &artifactStage{fs: base, sourcePath: "/src/m.mp4", sourceIdentity: captureArtifactSourceIdentity(base, "/src/m.mp4", info)}
	proof := stage.deferredSourceProof()
	require.NotNil(t, proof)
	assert.NoError(t, proof("/src/m.mp4", info))
	require.ErrorIs(t, proof("/src/other.mp4", other), errArtifactSourceChanged)
}
