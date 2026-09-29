package workflow

import (
	"context"
	"errors"
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

// F1 sidecar leg (codex P1, PRRT_kwDORn9KaM6m_lgp), move lane: the subtitle
// source directory entry is rename-swapped AT ExecuteOrganizePlan — after the
// pre-execute identity gate passed. The bound install must refuse rather than
// consume the replacement: the verified composite restores the foreign object
// onto the source name byte-intact, the seat records the typed refusal (never
// Moved/Skipped), and the staged-twin republish's source-consumption
// revalidation then aborts the apply with full rollback — the admitted
// sidecar, the foreign replacement, and every other input stay put.
func TestDeferredMoveRefusesSubtitleSwappedInsideExecute(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "verified-sub-move-swap")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	aside := filepath.Join(root, "incoming", "swapped-aside.srt")
	var seatErr error
	fault := &pr260PublicationFaultOrganizer{Organizer: real, preExecute: func(*organizer.OrganizePlan) {
		_ = base.Rename(subtitle, aside)
		_ = afero.WriteFile(base, subtitle, []byte("replacement subtitle"), 0o644)
	}, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		for i := range result.Subtitles {
			if result.Subtitles[i].Error != nil {
				seatErr = result.Subtitles[i].Error
			}
		}
	}}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-sub-move-swap"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.ErrorIs(t, publishErr, errArtifactSourceChanged, "the republished twin's source-consumption revalidation aborts the apply")
	assert.False(t, fsutil.PublishRefusal(publishErr), "a pre-publication admission refusal never classifies as a publish refusal")
	require.Error(t, seatErr, "the organizer lane recorded the refused subtitle install on its seat")
	assert.True(t, errors.Is(seatErr, fsutil.ErrTakeAsideForeign), "seat error class: %v", seatErr)
	assert.True(t, errors.Is(seatErr, errArtifactSourceChanged), "the admission class rides the seat error: %v", seatErr)
	got, err := afero.ReadFile(base, subtitle)
	require.NoError(t, err)
	assert.Equal(t, "replacement subtitle", string(got), "the rejected object rides back onto the source name byte-intact")
	got, err = afero.ReadFile(base, aside)
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got), "the admitted object is never touched")
	got, err = afero.ReadFile(base, source)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got), "rollback restored the moved video")
	pr260AssertNoFinals(t, base, dest)
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, kept := range []string{multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}

// F1 sidecar leg, copy lane: the same swap refuses the verified copy (the
// open handle fails its admission proof before a byte flows). The apply still
// publishes: the tree lane installs the ADMITTED staged twin, the foreign
// replacement stays put at the source name, and the seat records the typed
// refusal (never Copied/Skipped — nothing the destination holds came through
// this seat).
func TestDeferredCopyRefusesSubtitleSwappedInsideExecute(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "verified-sub-copy-swap")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	aside := filepath.Join(root, "incoming", "swapped-aside.srt")
	fault := &pr260PublicationFaultOrganizer{Organizer: real, preExecute: func(*organizer.OrganizePlan) {
		_ = base.Rename(subtitle, aside)
		_ = afero.WriteFile(base, subtitle, []byte("replacement subtitle"), 0o644)
	}}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-sub-copy-swap"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	var seat *organizer.SubtitleResult
	for i := range state.organizeResult.Subtitles {
		if filepath.Clean(state.organizeResult.Subtitles[i].OriginalPath) == filepath.Clean(subtitle) {
			seat = &state.organizeResult.Subtitles[i]
		}
	}
	require.NotNil(t, seat, "the subtitle lane classified the refused install")
	require.Error(t, seat.Error)
	assert.True(t, errors.Is(seat.Error, fsutil.ErrTakeAsideForeign), "seat: %v", seat.Error)
	assert.True(t, errors.Is(seat.Error, errArtifactSourceChanged), "seat: %v", seat.Error)
	assert.False(t, seat.Copied || seat.Moved || seat.Skipped, "a refusal is none of the consumption classes")
	targetDir := filepath.Join(dest, "movie")
	got, err := afero.ReadFile(base, filepath.Join(targetDir, "movie.srt"))
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got), "the destination holds the ADMITTED staged twin, never the replacement")
	got, err = afero.ReadFile(base, filepath.Join(targetDir, "movie.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "video", string(got))
	got, err = afero.ReadFile(base, subtitle)
	require.NoError(t, err)
	assert.Equal(t, "replacement subtitle", string(got), "copy mode consumes nothing — the foreign replacement is retained")
	got, err = afero.ReadFile(base, aside)
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got))
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, kept := range []string{source, multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}

// F1 sidecar leg, move lane, mid-composite window: the plant lands at the
// subtitle source name the moment the claim take consumed it. The claim still
// carries the admitted object, so the publication installs the ADMITTED
// subtitle and the plant is preserved, never consumed (the video-leg twin of
// TestDeferredMovePublishesAdmittedDespitePostClaimPlant, sharing its probe).
func TestDeferredMovePublishesAdmittedSubtitleDespitePostClaimPlant(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := verifiedMemFixture(t, "verified-sub-plant-move")
	dest := filepath.Join(root, "library")
	faultFS := &plantAfterSourceRenameFS{Fs: base, sourcePath: subtitle, plant: []byte("planted foreign subtitle")}
	org := organizer.NewOrganizer(faultFS, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: faultFS, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-sub-plant-move"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, org, faultFS, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	require.True(t, faultFS.fired, "the plant actually landed mid-composite")
	require.Len(t, state.organizeResult.Subtitles, 1)
	assert.True(t, state.organizeResult.Subtitles[0].Moved, "the admitted subtitle's install succeeded")
	target := filepath.Join(dest, "movie", "movie.srt")
	got, err := afero.ReadFile(base, target)
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got), "the bound install delivered the admitted identity")
	got, err = afero.ReadFile(base, subtitle)
	require.NoError(t, err)
	assert.Equal(t, "planted foreign subtitle", string(got), "the foreign plant is preserved at the source name, never consumed")
	got, err = afero.ReadFile(base, filepath.Join(dest, "movie", "movie.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "video", string(got))
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, gone := range []string{source, multipart} {
		exists, serr := afero.Exists(base, gone)
		require.NoError(t, serr)
		assert.False(t, exists, "deferred-move consumption of the untouched inputs is unchanged: %s", gone)
	}
	exists, _ := afero.Exists(base, unrelated)
	assert.True(t, exists)
}

// swapAfterSourceOpenFS rename-swaps sourcePath the moment the verified copy
// opens it (armed at ExecuteOrganizePlan entry — after every admission-phase
// reader). The just-opened handle keeps addressing the ADMITTED object, so a
// name swap anywhere after the open cannot retarget the streamed bytes.
type swapAfterSourceOpenFS struct {
	afero.Fs
	sourcePath string
	aside      string
	replBytes  []byte
	armed      bool
	fired      bool
}

func (f *swapAfterSourceOpenFS) Open(name string) (afero.File, error) {
	file, err := f.Fs.Open(name)
	if err == nil && f.armed && !f.fired && filepath.Clean(name) == filepath.Clean(f.sourcePath) {
		f.fired = true
		_ = f.Fs.Rename(f.sourcePath, f.aside)
		_ = afero.WriteFile(f.Fs, f.sourcePath, f.replBytes, 0o644)
	}
	return file, err
}

// F1 sidecar leg, copy lane, descriptor pinning: the subtitle entry is
// swapped after the verified open. The stream reads the pinned handle, so the
// destination receives the ADMITTED bytes and the replacement entry is never
// read for the publish (the video-leg twin of
// TestDeferredCopyPublishesAdmittedBytesDespitePostOpenSwap).
func TestDeferredCopyPublishesAdmittedSubtitleDespitePostOpenSwap(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "verified-sub-open-swap")
	dest := filepath.Join(root, "library")
	aside := filepath.Join(root, "incoming", "swapped-aside.srt")
	faultFS := &swapAfterSourceOpenFS{Fs: base, sourcePath: subtitle, aside: aside, replBytes: []byte("replacement subtitle")}
	real := organizer.NewOrganizer(faultFS, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, preExecute: func(*organizer.OrganizePlan) { faultFS.armed = true }}
	orch := &applyOrchImpl{fs: faultFS, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-sub-open-swap"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, real, faultFS, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	require.True(t, faultFS.fired, "the swap actually landed after the verified open")
	require.Len(t, state.organizeResult.Subtitles, 1)
	assert.True(t, state.organizeResult.Subtitles[0].Copied, "the handle-bound copy installed")
	target := filepath.Join(dest, "movie", "movie.srt")
	got, err := afero.ReadFile(base, target)
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got), "the pinned handle delivered the admitted bytes despite the swap")
	got, err = afero.ReadFile(base, subtitle)
	require.NoError(t, err)
	assert.Equal(t, "replacement subtitle", string(got), "the replacement is retained untouched")
	got, err = afero.ReadFile(base, aside)
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got))
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, kept := range []string{source, multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}

// faultClaimStatFS faults the verified move's POST-TAKE claim lookup once —
// the claim name is sourcePath+".vac."+token, so the fault keys on that
// prefix and never touches the video leg's claims. The reservation release
// and the take's own destination classification look the claim name up too,
// so the fault gates on the take itself: once the source rename lands, the
// NEXT claim-name lookup is the post-take re-proof (the leg under test).
type faultClaimStatFS struct {
	afero.Fs
	sourcePath string
	taken      bool
	fired      bool
}

func (f *faultClaimStatFS) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && filepath.Clean(oldname) == filepath.Clean(f.sourcePath) {
		f.taken = true
	}
	return err
}

func (f *faultClaimStatFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.taken && !f.fired && strings.HasPrefix(filepath.Clean(name), filepath.Clean(f.sourcePath)+".vac.") {
		f.fired = true
		return nil, false, errors.New("claim stat fault")
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// F1 sidecar stat-fault leg, move lane: the post-take re-proof lookup fails.
// The taken-aside ADMITTED subtitle rides back onto its source (no-replace),
// the seat records the stat fault, and the staged-twin lane republishes the
// admitted bytes and consumes the revalidated original — move semantics hold
// end to end with no claim residue.
func TestDeferredMoveSubtitleClaimStatFaultRestoresAndRepublishes(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := verifiedMemFixture(t, "verified-sub-claim-stat")
	dest := filepath.Join(root, "library")
	faultFS := &faultClaimStatFS{Fs: base, sourcePath: subtitle}
	org := organizer.NewOrganizer(faultFS, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: faultFS, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-sub-claim-stat"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, org, faultFS, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	require.True(t, faultFS.fired, "the stat fault actually hit the subtitle claim")
	require.Len(t, state.organizeResult.Subtitles, 1)
	seat := state.organizeResult.Subtitles[0]
	require.Error(t, seat.Error)
	assert.Contains(t, seat.Error.Error(), "inspect the taken claim")
	assert.False(t, seat.Moved || seat.Skipped)
	got, err := afero.ReadFile(base, filepath.Join(dest, "movie", "movie.srt"))
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got), "the republished staged twin delivers the admitted bytes")
	exists, serr := afero.Exists(base, subtitle)
	require.NoError(t, serr)
	assert.False(t, exists, "the revalidated original was consumed per move semantics")
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, gone := range []string{source, multipart} {
		e, serr := afero.Exists(base, gone)
		require.NoError(t, serr)
		assert.False(t, e, gone)
	}
	e, _ := afero.Exists(base, unrelated)
	assert.True(t, e)
}

// faultSourceOpenFS faults the verified copy's source open once while armed —
// arming rides the fault organizer's preExecute hook so admission-phase
// readers (staging copy, identity capture) never see it.
type faultSourceOpenFS struct {
	afero.Fs
	sourcePath string
	armed      bool
	fired      bool
}

func (f *faultSourceOpenFS) Open(name string) (afero.File, error) {
	if f.armed && !f.fired && filepath.Clean(name) == filepath.Clean(f.sourcePath) {
		f.fired = true
		return nil, errors.New("source open fault")
	}
	return f.Fs.Open(name)
}

// F1 sidecar stat-fault leg, copy lane: the source open itself fails. The
// seat records the fault, nothing is consumed (copy mode retains sources),
// and the tree lane installs the admitted staged twin.
func TestDeferredCopySubtitleSourceOpenFaultRepublishesTwin(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := verifiedMemFixture(t, "verified-sub-open-fault")
	dest := filepath.Join(root, "library")
	faultFS := &faultSourceOpenFS{Fs: base, sourcePath: subtitle}
	real := organizer.NewOrganizer(faultFS, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, preExecute: func(*organizer.OrganizePlan) { faultFS.armed = true }}
	orch := &applyOrchImpl{fs: faultFS, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-sub-open-fault"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, real, faultFS, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	require.True(t, faultFS.fired, "the open fault actually hit the subtitle source")
	require.Len(t, state.organizeResult.Subtitles, 1)
	seat := state.organizeResult.Subtitles[0]
	require.Error(t, seat.Error)
	assert.Contains(t, seat.Error.Error(), "open source")
	assert.False(t, seat.Copied || seat.Skipped)
	got, err := afero.ReadFile(base, filepath.Join(dest, "movie", "movie.srt"))
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got), "the tree lane installs the admitted staged twin")
	got, err = afero.ReadFile(base, subtitle)
	require.NoError(t, err)
	assert.Equal(t, "subtitle", string(got), "copy mode retains the source")
	assertNoVacResidue(t, base, filepath.Dir(source))
	for _, kept := range []string{source, multipart, unrelated} {
		e, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, e, kept)
	}
}

// siblingSourceProofs: nil for unpinned/absent-identity stages; each closure
// applies the admission identity matcher verbatim, keyed by cleaned source
// path, and unpinnable siblings contribute no entry.
func TestSiblingSourceProofsUnits(t *testing.T) {
	assert.Nil(t, (*artifactStage)(nil).siblingSourceProofs())
	assert.Nil(t, (&artifactStage{}).siblingSourceProofs())

	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/src/m.srt", []byte("admitted sub"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/src/other.srt", []byte("other sub"), 0o644))
	info, err := base.Stat("/src/m.srt")
	require.NoError(t, err)
	other, err := base.Stat("/src/other.srt")
	require.NoError(t, err)
	stage := &artifactStage{fs: base, siblings: []artifactSibling{
		{sourcePath: "/src/m.srt", identity: captureArtifactSourceIdentity(base, "/src/m.srt", info)},
		{sourcePath: "/src/unpinned.srt"},
	}}
	proofs := stage.siblingSourceProofs()
	require.Len(t, proofs, 1, "only pinnable-identity siblings bind")
	proof := proofs[filepath.Clean("/src/m.srt")]
	require.NotNil(t, proof)
	assert.NoError(t, proof("/src/m.srt", info))
	require.ErrorIs(t, proof("/src/other.srt", other), errArtifactSourceChanged)
	_, unpinned := proofs[filepath.Clean("/src/unpinned.srt")]
	assert.False(t, unpinned)
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
