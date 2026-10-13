package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// In-place modes keep the staged video copy, so the fenced publication still
// runs the non-deferred cleanup block: inverse journal, rollback origin, and
// the post-publish original removal. Each error leg must stay fail-closed.
func TestInPlacePublicationCleanupFaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"inverse persistence", "persist inverse before source cleanup"},
		{"original removal", "remove original after artifact publication"},
		{"untracked rollback arm", "track staged publication destination"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, subtitle, multipart, _, match := pr260FencedFiles(t, "inplace-cleanup-"+tc.name)
			require.NoError(t, base.Remove(subtitle))
			require.NoError(t, base.Remove(multipart))
			movie := models.Movie{ContentID: "inplace-cleanup-" + tc.name, RenderGeneration: 1}
			var fs afero.Fs = base
			cfg := &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}
			var org organizer.OrganizerInterface = organizer.NewOrganizer(fs, cfg, template.NewEngine(), nil)
			var log RevertLog
			switch tc.name {
			case "inverse persistence":
				log = &completionFaultLog{complete: func() error { return errors.New("inverse unavailable") }}
			case "original removal":
				fs = &pr260PublishFinalFS{Fs: base, op: "remove", path: source}
				org = organizer.NewOrganizer(fs, cfg, template.NewEngine(), nil)
			case "untracked rollback arm":
				real := org.(*organizer.Organizer)
				org = &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
					result.NewPath = filepath.Join(root, "missing-parent", "untracked.mp4")
				}}
			}
			orch := &applyOrchImpl{fs: fs, organizer: org, revertLog: log}
			cmd := pr260ArtifactFailureCommand(&movie, match, filepath.Dir(source))
			cmd.OperationMode = operationmode.OperationModeInPlaceNoRenameFolder
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = true
			cmd.Download = false
			cmd.PublicationFence = postPublishFence{movie: &movie}
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			require.NotNil(t, stage)
			defer stage.cleanup()
			require.False(t, stage.videoStagingDeferred(), "in-place keeps a staged copy")
			state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
			publishErr := stage.publish(context.Background(), orch, state, nil)
			require.ErrorContains(t, publishErr, tc.want)
		})
	}
}
