package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

func TestDeferredSoftPrimaryPostUnlockSuccessorRetained(t *testing.T) {
	for _, kind := range []string{"regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			base, root, source, subtitle, _, _, match := pr260FencedFiles(t, "soft-primary-"+kind)
			probe := filepath.Join(root, "symlink-probe")
			if err := os.Symlink(source, probe); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			require.NoError(t, base.Remove(probe))
			dest := filepath.Join(root, "library")
			real := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
			foreignTarget := filepath.Join(root, "foreign-target")
			require.NoError(t, afero.WriteFile(base, foreignTarget, []byte(primarySuccessorPayload), 0o600))
			videoDst := ""
			fault := &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(plan *organizer.OrganizePlan, result *organizer.OrganizeResult) {
				if filepath.Clean(plan.SourcePath) != filepath.Clean(source) || !result.Moved {
					return
				}
				videoDst = plan.TargetPath
				require.NoError(t, base.Remove(videoDst))
				if kind == "symlink" {
					require.NoError(t, os.Symlink(foreignTarget, videoDst))
				} else {
					require.NoError(t, afero.WriteFile(base, videoDst, []byte(primarySuccessorPayload), 0o600))
				}

			}}
			orch := &applyOrchImpl{fs: base, organizer: fault, revertLog: &completeCallFaultLog{deleteReconcileErr: errors.New("injected delete reconcile failure")}}
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "soft-primary-" + kind}, match, dest)
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = false
			cmd.Organize.LinkMode = organizer.LinkModeSoft
			cmd.Download = false
			stage, _, publishErr := verifiedStagePublish(t, orch, real, base, root, source, dest, match, cmd)
			defer stage.cleanup()
			require.NotEmpty(t, videoDst)
			require.ErrorContains(t, publishErr, "reconcile copy-installed delete intents")
			payload, err := afero.ReadFile(base, videoDst)
			require.NoError(t, err)
			require.Equal(t, primarySuccessorPayload, string(payload))
			if kind == "symlink" {
				target, err := os.Readlink(videoDst)
				require.NoError(t, err)
				require.Equal(t, foreignTarget, target)
			}
			payload, err = afero.ReadFile(base, source)
			require.NoError(t, err)
			require.Equal(t, "video", string(payload))
			payload, err = afero.ReadFile(base, subtitle)
			require.NoError(t, err)
			require.Equal(t, "subtitle", string(payload))
		})
	}
}
