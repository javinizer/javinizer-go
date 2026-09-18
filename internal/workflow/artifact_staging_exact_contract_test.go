package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestPR260ExactOrganizerSeamsAndResultFallback(t *testing.T) {
	for _, tc := range []struct {
		name, want  string
		claim, skip bool
	}{{"claim missing planner", "planned execution seam", true, false}, {"publish missing planner", "planned execution seam", false, false}, {"duplicate skipped", "", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-"+tc.name)
			dest := filepath.Join(root, "published")
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "exact-" + tc.name}, match, dest)
			cmd.Organize.Skip = false
			if tc.claim {
				cmd.Organize.DuplicateTracker = organizer.NewDuplicateTracker(false)
			}
			orch := &applyOrchImpl{fs: base}
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			if tc.claim {
				require.Nil(t, stage)
				require.ErrorContains(t, err, tc.want)
			} else {
				require.NoError(t, err)
				state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: "", DuplicateSkipped: tc.skip}}
				err = stage.publish(context.Background(), orch, state, nil)
				if tc.skip {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tc.want)
				}
				stage.cleanup()
			}
			pr260AssertRetained(t, base, source, sub, part, other)
			pr260AssertNoFinals(t, base, dest)
			pr260AssertStageGone(t, base, root)
		})
	}
}
func TestPR260ExactPreservedPosterInvalidatesVerification(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-poster")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "exact-poster"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	staged := filepath.Join(stage.root, "poster.jpg")
	target := filepath.Join(dest, "poster.jpg")
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
	require.NoError(t, base.MkdirAll(dest, 0o755))
	require.NoError(t, afero.WriteFile(base, target, []byte("current"), 0o644))
	state := &applyPipelineState{downloadPaths: []string{staged}}
	steps := &stepCompletion{PosterVerified: true}
	err = stage.publish(context.Background(), &applyOrchImpl{fs: base}, state, steps)
	require.NoError(t, err)
	require.False(t, steps.PosterVerified)
	b, e := afero.ReadFile(base, target)
	require.NoError(t, e)
	require.Equal(t, "current", string(b))
	require.Equal(t, target, state.downloadPaths[0])
	stage.cleanup()
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
}
