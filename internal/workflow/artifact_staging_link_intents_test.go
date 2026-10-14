package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The deferred primary pin keys its proof shape on the link mode (codex P2,
// PRRT_kwDORn9KaM6nBUq8 / PRRT_kwDORn9KaM6nBUrF): a soft link pins the exact
// target string the strategy installs, a hard link pins the admitted source
// object's identity tuple, and neither ever pre-streams the payload.

func primaryPinOf(t *testing.T, ledger *completeCallFaultLog, target string) models.DeleteEntry {
	t.Helper()
	for _, entry := range ledger.deletePaths {
		if filepath.Clean(entry.Path) == filepath.Clean(target) {
			return entry
		}
	}
	t.Fatalf("no durable pin captured for %s", target)
	return models.DeleteEntry{}
}

func TestDeferredSoftLinkPrimaryPinCarriesTarget(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "soft-pin-target")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: ledger}
	cmd := copyIntentCommand(&models.Movie{ContentID: "soft-pin-target"}, match, dest)
	cmd.Organize.LinkMode = organizer.LinkModeSoft
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, finalPlan := copyIntentPlans(t, real, stage, match, source, dest)
	subMoves := real.PlanSubtitleMoves(finalPlan)
	require.Len(t, subMoves, 1)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil))

	pin := primaryPinOf(t, ledger, finalPlan.TargetPath)
	wantTarget, targetErr := organizer.SymlinkLinkTarget(finalPlan.SourcePath)
	require.NoError(t, targetErr)
	assert.Equal(t, wantTarget, pin.LinkTarget, "the pin carries the exact payload the symlink leg installs (one shared computation route)")
	assert.Empty(t, pin.SHA256, "a link install has no content hash — and must not pre-read one (PRRT_kwDORn9KaM6nBUrF)")
	assert.Empty(t, pin.CopyPartialSHA256)
	assert.Zero(t, pin.IdentityModUnix)

	// The installed destination IS a link carrying that payload — the pin's
	// authentication model matches reality byte for byte.
	got, readErr := os.Readlink(finalPlan.TargetPath)
	require.NoError(t, readErr, "the soft-link publish installed a link object")
	assert.Equal(t, pin.LinkTarget, got)

	assert.Zero(t, atomic.LoadInt32(&ledger.seals), "link lanes carry no content digest — no seal runs")
	assert.Equal(t, []string{subMoves[0].NewPath}, ledger.deleteKeepCaptured,
		"the link primary's pin settles away with the unkept sidecar pins")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

func TestDeferredHardLinkPrimaryPinCarriesAdmittedIdentity(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "hard-pin-identity")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: ledger}
	cmd := copyIntentCommand(&models.Movie{ContentID: "hard-pin-identity"}, match, dest)
	cmd.Organize.LinkMode = organizer.LinkModeHard
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, finalPlan := copyIntentPlans(t, real, stage, match, source, dest)
	subMoves := real.PlanSubtitleMoves(finalPlan)
	require.Len(t, subMoves, 1)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil))

	pin := primaryPinOf(t, ledger, finalPlan.TargetPath)
	srcInfo, statErr := base.Stat(source)
	require.NoError(t, statErr)
	dev, ino, identityOK := fsutil.BoundObjectIdentity(base, source, srcInfo)
	require.True(t, identityOK, "OsFs exposes the kernel identity this pin shape is built on")
	assert.True(t, pin.IdentityStrong, "OsFs identity pins strongly (weak legs only serve identity-free filesystems)")
	assert.Equal(t, dev, pin.IdentityDev)
	assert.Equal(t, ino, pin.IdentityIno)
	assert.Equal(t, srcInfo.Size(), pin.IdentitySize)
	assert.Equal(t, srcInfo.ModTime().Unix(), pin.IdentityModUnix)
	assert.Empty(t, pin.SHA256, "the linked object IS the source's object — no content pass (PRRT_kwDORn9KaM6nBUrF)")

	gotInfo, statErr := base.Stat(finalPlan.TargetPath)
	require.NoError(t, statErr, "the hard-link publish installed")
	assert.True(t, os.SameFile(srcInfo, gotInfo), "the linked destination shares the source's object exactly as the pin assumes")

	assert.Zero(t, atomic.LoadInt32(&ledger.seals), "link lanes carry no content digest — no seal runs")
	assert.Equal(t, []string{subMoves[0].NewPath}, ledger.deleteKeepCaptured)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// A seal-journal refusal aborts the publication laps the SAME rollback
// discipline as a reconcile refusal: the install is batch-armed, so rollback
// vacates it and the durable pins stay for recovery — while every source is
// retained.
func TestDeferredCopySealFaultRollsBack(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-seal-fault")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: &completeCallFaultLog{sealErr: errors.New("seal journal down")}}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-seal-fault"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, _ := copyIntentPlans(t, real, stage, match, source, dest)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "seal deferred primary copy pin")
	pr260AssertNoFinals(t, base, dest)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// deferredPrimaryDeleteEntry, direct table: the helper's construction legs
// are the pin contract each finding pins on.
func TestDeferredPrimaryDeleteEntry_Shapes(t *testing.T) {
	base, _, source, _, _, _, _ := pr260FencedFiles(t, "pin-entry-shapes")
	srcInfo, statErr := base.Stat(source)
	require.NoError(t, statErr)
	planFor := func(target string) *organizer.OrganizePlan {
		return &organizer.OrganizePlan{SourcePath: source, TargetPath: target}
	}

	t.Run("soft", func(t *testing.T) {
		stage := &artifactStage{fs: base, sourcePath: source}
		stage.original.Organize.LinkMode = organizer.LinkModeSoft
		entry, err := stage.deferredPrimaryDeleteEntry(planFor(filepath.Join(t.TempDir(), "out.mkv")))
		require.NoError(t, err)
		want, wantErr := organizer.SymlinkLinkTarget(source)
		require.NoError(t, wantErr)
		assert.Equal(t, want, entry.LinkTarget)
	})

	t.Run("hard", func(t *testing.T) {
		stage := &artifactStage{fs: base, sourcePath: source}
		stage.original.Organize.LinkMode = organizer.LinkModeHard
		stage.sourceIdentity = captureArtifactSourceIdentity(base, source, srcInfo)
		entry, err := stage.deferredPrimaryDeleteEntry(planFor(filepath.Join(t.TempDir(), "out.mkv")))
		require.NoError(t, err)
		assert.Equal(t, srcInfo.Size(), entry.IdentitySize)
		assert.Equal(t, srcInfo.ModTime().Unix(), entry.IdentityModUnix)
		if stage.sourceIdentity.hasDevIno {
			assert.True(t, entry.IdentityStrong)
			assert.Equal(t, stage.sourceIdentity.dev, entry.IdentityDev)
			assert.Equal(t, stage.sourceIdentity.ino, entry.IdentityIno)
		}

		t.Run("plan naming another object refuses closed", func(t *testing.T) {
			other := planFor(filepath.Join(t.TempDir(), "out.mkv"))
			other.SourcePath = filepath.Join(t.TempDir(), "not-the-admitted.mkv")
			_, err := stage.deferredPrimaryDeleteEntry(other)
			require.ErrorContains(t, err, "not the admitted deferred source")
		})

		t.Run("no admitted identity refuses closed", func(t *testing.T) {
			unpinned := &artifactStage{fs: base, sourcePath: source}
			unpinned.original.Organize.LinkMode = organizer.LinkModeHard
			_, err := unpinned.deferredPrimaryDeleteEntry(planFor(filepath.Join(t.TempDir(), "out.mkv")))
			require.ErrorContains(t, err, "no admitted identity")
		})
	})

	t.Run("copy", func(t *testing.T) {
		stage := &artifactStage{fs: base, sourcePath: source}
		stage.original.Organize.LinkMode = organizer.LinkModeNone
		target := filepath.Join(t.TempDir(), "out.mkv")
		entry, err := stage.deferredPrimaryDeleteEntry(planFor(target))
		require.NoError(t, err)
		assert.Equal(t, target, entry.Path)
		assert.Equal(t, srcInfo.Size(), entry.CopySize)
		assert.NotEmpty(t, entry.CopyPartialSHA256)
		assert.Empty(t, entry.SHA256, "the interim shape never streams the payload (PRRT_kwDORn9KaM6nBUrF)")

		t.Run("unreadable source aborts the pin", func(t *testing.T) {
			faultStage := &artifactStage{fs: &denyOpenAfterPrepareFS{Fs: base, path: source, armed: true}, sourcePath: source}
			faultStage.original.Organize.LinkMode = organizer.LinkModeNone
			_, err := faultStage.deferredPrimaryDeleteEntry(planFor(target))
			require.ErrorContains(t, err, "pin deferred primary copy digest")
		})
	})
}

// The soft-link pin refuses closed when the shared target computation fails:
// organizer.SymlinkLinkTarget only errors when absolutizing a relative source
// (a cwd failure this package cannot induce deterministically), so the fault
// is replayed through the workflow seam (symlinkLinkTargetFn). The wrapped
// refusal must name the pin/symlink/target semantics and carry the cause,
// and no partial entry may escape.
func TestDeferredPrimaryDeleteEntry_SoftLinkTargetFault(t *testing.T) {
	sentinel := errors.New("synthetic symlink target resolution failure")
	prev := symlinkLinkTargetFn
	symlinkLinkTargetFn = func(string) (string, error) { return "", sentinel }
	t.Cleanup(func() { symlinkLinkTargetFn = prev })

	source := filepath.Join(t.TempDir(), "src.mkv")
	target := filepath.Join(t.TempDir(), "out.mkv")
	stage := &artifactStage{sourcePath: source}
	stage.original.Organize.LinkMode = organizer.LinkModeSoft
	entry, err := stage.deferredPrimaryDeleteEntry(&organizer.OrganizePlan{SourcePath: source, TargetPath: target})
	require.ErrorIs(t, err, sentinel)
	assert.ErrorContains(t, err, "pin deferred primary symlink target")
	assert.ErrorContains(t, err, target, "the refusal names the publication the pin was for")
	assert.Equal(t, models.DeleteEntry{}, entry, "a refused pin carries no partial entry")
}
