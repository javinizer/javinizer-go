package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

type escapingInPlacePlanner struct {
	*organizer.Organizer
	targetDir string
	executes  int
}

func (o *escapingInPlacePlanner) PlanOrganize(ctx context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	plan, err := o.Organizer.PlanOrganize(ctx, cmd)
	if err == nil {
		plan.TargetDir = o.targetDir
		plan.TargetPath = filepath.Join(o.targetDir, filepath.Base(plan.TargetPath))
	}
	return plan, err
}

func (o *escapingInPlacePlanner) ExecuteOrganizePlan(plan *organizer.OrganizePlan, move bool, link organizer.LinkMode) (*organizer.OrganizeResult, error) {
	o.executes++
	return o.Organizer.ExecuteOrganizePlan(plan, move, link)
}

func TestInPlaceDeferredRejectsEscapingPlan(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, phase := range []string{"prepare", "publish"} {
			t.Run(string(mode)+"/"+phase, func(t *testing.T) {
				fs := afero.NewOsFs()
				input := filepath.Join(t.TempDir(), "input")
				require.NoError(t, fs.MkdirAll(input, 0o755))
				source := filepath.Join(input, "IPX-123-old.mkv")
				require.NoError(t, afero.WriteFile(fs, source, []byte("source bytes"), 0o644))
				movie := models.Movie{ID: "IPX-123", ContentID: "in-place-scope", RenderGeneration: 1}
				outside := filepath.Join(t.TempDir(), "outside")
				org := &escapingInPlacePlanner{Organizer: organizer.NewOrganizer(fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, template.NewEngine(), nil), targetDir: outside}
				orch := &applyOrchImpl{fs: fs, organizer: org}
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), MovieID: movie.ID, Extension: ".mkv"}, DestPath: input, OperationMode: mode, Organize: OrganizeOptions{MoveFiles: true}, PublicationFence: postPublishFence{movie: &movie}}
				stage, stagedCmd, err := orch.prepareArtifact(context.Background(), cmd)
				require.NoError(t, err)
				defer stage.cleanup()
				state := &applyPipelineState{artifact: stage, movie: &movie, organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
				if phase == "prepare" {
					err = orch.stepOrganize(context.Background(), stagedCmd, state, &stepCompletion{})
				} else {
					err = stage.publish(context.Background(), orch, state, nil)
				}
				require.ErrorContains(t, err, "escapes staging area")
				require.Zero(t, org.executes)
				require.Equal(t, input, stage.finalRoot)
				body, err := afero.ReadFile(fs, source)
				require.NoError(t, err)
				require.Equal(t, "source bytes", string(body))
				require.NoDirExists(t, outside)
			})
		}
	}
}
