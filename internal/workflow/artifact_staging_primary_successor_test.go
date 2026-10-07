package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

const primarySuccessorPayload = "foreign successor — external writer's video bytes, never adopted"

// codex P1 (PR #276, finding PRRT_kwDORn9KaM6p3Dq1) — "bind primary
// observation to the published object". The deferred primary publish hands
// back the object it installed; a foreign writer replacing the video
// destination after execute released its lock must NOT be adopted as this
// batch's install, or a later failing leg's rollback deletes the successor.
func TestDeferredPrimaryCopyPostUnlockSuccessorRetainedNotAdopted(t *testing.T) {
	base, root, source, subtitle, _, _, match := pr260FencedFiles(t, "primary-copy-successor")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	videoDst := ""
	fault := &pr260PublicationFaultOrganizer{Organizer: real,
		afterExecute: func(plan *organizer.OrganizePlan, result *organizer.OrganizeResult) {
			if filepath.Clean(plan.SourcePath) != filepath.Clean(source) || result.InstalledIdentity == nil {
				return
			}
			videoDst = plan.TargetPath
			// The intruder: same name, different inode and bytes.
			require.NoError(t, base.Remove(videoDst))
			require.NoError(t, afero.WriteFile(base, videoDst, []byte(primarySuccessorPayload), 0o644))
			// Force a later failing leg (the sidecar confirm) so the pre-fix
			// lane's poisoned adoption actually reaches rollback.
			for _, sr := range result.Subtitles {
				if sr.Copied && sr.NewPath != "" {
					require.NoError(t, base.Remove(sr.NewPath))
				}
			}
		},
	}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "primary-copy-successor"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Organize.LinkMode = organizer.LinkModeNone
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NotEmpty(t, videoDst, "the primary copy installed and the swap landed in the observation window")
	require.Error(t, publishErr, "the late sidecar leg fails the apply, running compensation")
	require.ErrorContains(t, publishErr, "inspect staged publication result", "the removed sidecar destination is the late failing leg")
	got, readErr := afero.ReadFile(base, videoDst)
	require.NoError(t, readErr, "rollback must never unlink the successor: the bound observation never adopted it")
	assert.Equal(t, primarySuccessorPayload, string(got), "the successor is retained byte-intact")
	got, readErr = afero.ReadFile(base, source)
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(got), "copy mode never consumes the admitted source")
	got, readErr = afero.ReadFile(base, subtitle)
	require.NoError(t, readErr)
	assert.Equal(t, "subtitle", string(got), "copy mode retains the admitted sidecar source")
}

// Mirror of the copied-sidecar coverage test: when the PRIMARY bound
// observation refuses with something other than the typed successor class,
// the apply propagates it wrapped as "bind deferred primary install" rather
// than swallowing an unexpected failure.
func TestDeferredPrimaryBoundObserveGenericErrorWraps(t *testing.T) {
	base, root, source, _, _, _, match := pr260FencedFiles(t, "primary-bound-observe-wrap")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: real}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "primary-bound-observe-wrap"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	old := observeBoundPrimaryPublish
	t.Cleanup(func() { observeBoundPrimaryPublish = old })
	observeBoundPrimaryPublish = func(*downloader.ReplacementBatch, string, os.FileInfo) error {
		return errBoundObserveInjected
	}

	stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.Error(t, publishErr)
	assert.ErrorIs(t, publishErr, errBoundObserveInjected, "the injected refusal unwraps through the wrap")
	assert.Contains(t, publishErr.Error(), "bind deferred primary install")
}

// codex P1 (PR #276, finding PRRT_kwDORn9KaM6p3Dq8) — "retract sidecar
// ownership after successor refusal": the refusal must withdraw the durable
// claim too, or the completion reconcile keeps the target pinned and the
// seat graduates into the ledger's unconditional Delete list.
func TestRetractCopiedSidecarOwnershipClearsClaimKeepsExclusion(t *testing.T) {
	// Paths go through the same normalization the helper applies, so the case
	// holds on every host (Windows cleans to volume-style separators).
	movie := filepath.Clean(filepath.FromSlash("/lib/movie.srt"))
	other := filepath.Clean(filepath.FromSlash("/lib/other.srt"))
	result := &organizer.OrganizeResult{
		Subtitles: []organizer.SubtitleResult{
			{SubtitleMove: models.SubtitleMove{NewPath: movie, Copied: true}},
			{SubtitleMove: models.SubtitleMove{NewPath: other, Copied: true}},
		},
	}
	targets := map[string]bool{movie: true, other: true}
	identities := map[string]os.FileInfo{movie: nil, other: nil}

	retractCopiedSidecarOwnership(result, targets, identities, movie)

	assert.False(t, targets[movie], "the refused target leaves the set the reconcile keeps")
	assert.True(t, targets[other], "unrelated seats keep their claim")
	assert.NotContains(t, identities, movie)
	assert.True(t, result.Subtitles[0].SuccessorRefused, "the seat is marked so the completion ledger skips it")
	assert.True(t, result.Subtitles[0].Copied, "Copied stays set: the rehome exclusion still keeps the staged duplicate out of the tree")
	assert.False(t, result.Subtitles[1].SuccessorRefused)
}

// codex P1 (PR #276, finding PRRT_kwDORn9KaM6p6mdQ) — the refusal must not be
// re-adopted by the unconditional primary confirmation: with no later failing
// leg the apply commits, the successor stays byte-intact, and the leg is left
// UNINSTALLED instead of confirmed against the intruder's name.
func TestDeferredPrimarySuccessorRefusalSkipsConfirmation(t *testing.T) {
	base, root, source, _, _, _, match := pr260FencedFiles(t, "primary-refused-confirm")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	videoDst := ""
	fault := &pr260PublicationFaultOrganizer{Organizer: real,
		afterExecute: func(plan *organizer.OrganizePlan, result *organizer.OrganizeResult) {
			if filepath.Clean(plan.SourcePath) != filepath.Clean(source) || result.InstalledIdentity == nil {
				return
			}
			videoDst = plan.TargetPath
			require.NoError(t, base.Remove(videoDst))
			require.NoError(t, afero.WriteFile(base, videoDst, []byte(primarySuccessorPayload), 0o644))
		},
	}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "primary-refused-confirm"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Organize.LinkMode = organizer.LinkModeNone
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NotEmpty(t, videoDst, "the primary install ran and the swap landed in the observation window")
	require.NoError(t, publishErr, "the refusal is nonfatal — the apply commits")
	got, readErr := afero.ReadFile(base, videoDst)
	require.NoError(t, readErr)
	assert.Equal(t, primarySuccessorPayload, string(got), "the occupant is retained and never confirmed as this apply's install")
}

// primarySealFaultLog fails the deferred primary copy seal, placing a failure
// AFTER the point where the unbound confirmation used to adopt the successor.
type primaryReconcileFaultLog struct {
	RevertLog
	err error
}

func (l primaryReconcileFaultLog) ReconcileDeleteIntents(context.Context, OperationID, []string) error {
	return l.err
}

var errPrimaryReconcileDenied = errors.New("injected reconcile failure")

// The exploit shape: with the primary refused, a LATER failure must not let
// rollback delete the successor. Pre-fix the unconditional ConfirmPublish
// adopted the occupant, so the rollback's identity-verified unlink removed
// foreign bytes here (codex P1, PRRT_kwDORn9KaM6p6mdQ).
func TestDeferredPrimarySuccessorRefusalSurvivesLaterFailure(t *testing.T) {
	env := setupCopyIntentE2E(t, "primary-refused-rollback")
	env.orch.revertLog = primaryReconcileFaultLog{RevertLog: env.orch.revertLog, err: errPrimaryReconcileDenied}
	videoDst := ""
	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org,
		afterExecute: func(plan *organizer.OrganizePlan, result *organizer.OrganizeResult) {
			if filepath.Clean(plan.SourcePath) != filepath.Clean(env.source) || result.InstalledIdentity == nil {
				return
			}
			videoDst = plan.TargetPath
			require.NoError(t, env.fs.Remove(videoDst))
			require.NoError(t, afero.WriteFile(env.fs, videoDst, []byte(primarySuccessorPayload), 0o644))
		},
	}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeHard, false, false)
	_, err := env.orch.Execute(t.Context(), cmd)

	require.NotEmpty(t, videoDst, "the primary install ran and the swap landed in the observation window")
	require.Error(t, err, "the injected reconcile failure aborts the apply after the confirmation point")
	got, readErr := afero.ReadFile(env.fs, videoDst)
	require.NoError(t, readErr, "rollback must never unlink the successor: the refused leg was never confirmed against its name")
	assert.Equal(t, primarySuccessorPayload, string(got), "the successor is retained byte-intact")
}
