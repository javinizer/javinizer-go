package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// codex P1 (PR #276, finding PRRT_kwDORn9KaM6p3Dq8) — "retract sidecar
// ownership after successor refusal": the refusal must withdraw the durable
// claim too, or the completion reconcile keeps the target pinned and the
// seat graduates into the ledger's unconditional Delete list.
func TestRetractCopiedSidecarOwnershipClearsClaimKeepsExclusion(t *testing.T) {
	result := &organizer.OrganizeResult{
		Subtitles: []organizer.SubtitleResult{
			{SubtitleMove: models.SubtitleMove{NewPath: "/lib/movie.srt", Copied: true}},
			{SubtitleMove: models.SubtitleMove{NewPath: "/lib/other.srt", Copied: true}},
		},
	}
	targets := map[string]bool{"/lib/movie.srt": true, "/lib/other.srt": true}
	identities := map[string]os.FileInfo{"/lib/movie.srt": nil, "/lib/other.srt": nil}

	retractCopiedSidecarOwnership(result, targets, identities, "/lib/movie.srt")

	assert.False(t, targets["/lib/movie.srt"], "the refused target leaves the set the reconcile keeps")
	assert.True(t, targets["/lib/other.srt"], "unrelated seats keep their claim")
	assert.NotContains(t, identities, "/lib/movie.srt")
	assert.True(t, result.Subtitles[0].SuccessorRefused, "the seat is marked so the completion ledger skips it")
	assert.True(t, result.Subtitles[0].Copied, "Copied stays set: the rehome exclusion still keeps the staged duplicate out of the tree")
	assert.False(t, result.Subtitles[1].SuccessorRefused)
}
