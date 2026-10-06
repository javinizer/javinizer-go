package workflow

// codex P1 (PR #276, finding ntCe6) — "bind copied-sidecar observation to
// the installed object". The deferred copy lane arms each sidecar endpoint
// with the replacement batch BEFORE execution, the organizer's
// handleSubtitles copies and releases its destination lock, and the batch
// observes the install afterwards. An external writer replacing the copied
// subtitle inside that lock-release→observation window used to be ADOPTED:
// ObservePublishResult re-resolved the destination name and recorded the
// successor's identity as this batch's install, so a later failed leg's
// ReplacementBatch.Rollback identity-verified and deleted the foreign
// successor before restoring any displaced backup. The verified copy now
// hands out the destination object's publish-time identity
// (CopyFileNoReplaceVerifiedInstall), and the observation binds THAT
// identity (ObservePublishResultBound, comparing through the same predicate
// the rollback unlink applies). An affirmative successor divergence (the
// typed ErrPublishSuccessorUnproven class, the copy lane's twin of the
// hard-link post-link successor contract PRRT_kwDORn9KaM6nsX9a) releases
// the armed leg UNINSTALLED instead — never armed for deletion — while the
// apply keeps its foreign-swap-survives commit shape
// (PRRT_kwDORn9KaM6m7CBi: the durable hash pin owns later-revert retention).

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

const copiedSuccessorPayload = "foreign successor — external writer's subtitle bytes, never adopted"

// The external replacement lands in the exact lock-release→observation
// window: afterExecute runs after handleSubtitles copied and released its
// destination lock, before the workflow's observation loop. One copied
// endpoint is swapped for a foreign object (same name, different inode and
// bytes); the other is left as this batch's genuine install to witness
// rollback unwinding ours while retaining the successor. The video
// destination is removed too, giving the pre-fix lane a later failing leg so
// its poisoned rollback actually runs (the finding's exploit shape).
func TestDeferredCopiedSidecarPostUnlockSuccessorRetainedNotAdopted(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copied-sidecar-successor")
	engSubtitle := filepath.Join(root, "incoming", "PR260-COPIED-SIDECAR-SUCCESSOR.eng.srt")
	require.NoError(t, afero.WriteFile(base, engSubtitle, []byte("subtitle-eng"), 0o644))
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	videoDst := ""
	var swappedTarget, keptTarget string
	fault := &pr260PublicationFaultOrganizer{Organizer: real,
		preExecute: func(plan *organizer.OrganizePlan) {
			if filepath.Clean(plan.SourcePath) == filepath.Clean(source) {
				videoDst = plan.TargetPath
			}
		},
		afterExecute: func(plan *organizer.OrganizePlan, result *organizer.OrganizeResult) {
			var copied []string
			for _, sr := range result.Subtitles {
				if sr.Copied && sr.NewPath != "" {
					copied = append(copied, sr.NewPath)
				}
			}
			require.Len(t, copied, 2, "both subtitle seats copied — one to swap, one to keep as rollback witness")
			sort.Strings(copied)
			keptTarget, swappedTarget = copied[0], copied[1]
			require.NoError(t, base.Remove(swappedTarget))
			require.NoError(t, afero.WriteFile(base, swappedTarget, []byte(copiedSuccessorPayload), 0o644))
			if videoDst != "" {
				require.NoError(t, base.Remove(videoDst), "the late failing leg: the pre-fix lane's poisoned adoption must reach rollback")
			}
		},
	}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "copied-sidecar-successor"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Organize.LinkMode = organizer.LinkModeNone
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NotEmpty(t, swappedTarget, "the copied sidecar install ran and the external swap landed in the observation window")
	require.Error(t, publishErr, "the late leg (the removed video destination) still fails the apply")
	require.ErrorContains(t, publishErr, "inspect staged publication result")
	require.NotErrorIs(t, publishErr, fsutil.ErrPublishSuccessorUnproven, "adoption-skip is nonfatal for the sidecar: the successor never becomes a batch leg")
	got, readErr := afero.ReadFile(base, swappedTarget)
	require.NoError(t, readErr, "rollback must never UnlinkVerified the successor: the leg was never installed against its identity")
	assert.Equal(t, copiedSuccessorPayload, string(got), "the successor is retained byte-intact")
	_, keptErr := base.Stat(keptTarget)
	assert.True(t, os.IsNotExist(keptErr), "rollback unwound this batch's own verified install (identity-matched), proving the compensation ran")
	got, readErr = afero.ReadFile(base, source)
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(got), "the copy lane never consumes the admitted source")
	got, readErr = afero.ReadFile(base, subtitle)
	require.NoError(t, readErr)
	assert.Equal(t, "subtitle", string(got))
	got, readErr = afero.ReadFile(base, engSubtitle)
	require.NoError(t, readErr)
	assert.Equal(t, "subtitle-eng", string(got))
	assertNoVacResidue(t, base, filepath.Dir(swappedTarget))
	for _, kept := range []string{multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}

// Control: the same bound lane without an intruder — the copy-time identity
// matches at observation, the adopt+confirm discipline commits, and both
// sidecars install exactly as before.
func TestDeferredCopiedSidecarBoundObserveInstalls(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copied-sidecar-bound-install")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	videoDst := ""
	var subDst string
	var identities int
	fault := &pr260PublicationFaultOrganizer{Organizer: real,
		preExecute: func(plan *organizer.OrganizePlan) {
			if filepath.Clean(plan.SourcePath) == filepath.Clean(source) {
				videoDst = plan.TargetPath
			}
		},
		afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
			for _, sr := range result.Subtitles {
				if sr.Copied && sr.NewPath != "" {
					subDst = sr.NewPath
				}
				if sr.InstalledIdentity != nil {
					identities++
				}
			}
		},
	}
	orch := &applyOrchImpl{fs: base, organizer: fault}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "copied-sidecar-bound-install"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Organize.LinkMode = organizer.LinkModeNone
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	require.NotEmpty(t, subDst)
	assert.Equal(t, 1, identities, "the admission-bound copy produced exactly one publish-proven install identity")
	got, readErr := afero.ReadFile(base, subDst)
	require.NoError(t, readErr)
	assert.Equal(t, "subtitle", string(got), "the bound observation adopted the actual installed object")
	require.NotEmpty(t, videoDst)
	got, readErr = afero.ReadFile(base, videoDst)
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(got), "the primary copy install committed")
	require.NotNil(t, state)
	got, readErr = afero.ReadFile(base, source)
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(got), "copy mode retains the source")
	stage.cleanup()
	for _, kept := range []string{subtitle, multipart, unrelated} {
		exists, serr := afero.Exists(base, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}
