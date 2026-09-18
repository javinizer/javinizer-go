package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/require"
)

func TestPR260PublicationDuplicateResolvedClaimBranches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		force bool
		want  string
	}{{"force update resolves duplicate", true, ""}, {"without force update conflicts", false, "conflicts detected"}} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "duplicate-"+tc.name)
			dest := filepath.Join(root, "library")
			movie := &models.Movie{ContentID: "pr260-duplicate", ID: "PR260-DUPLICATE"}
			orch := pr260RealApply(base, movie, organizer.MediaFormatConfig{}, nil, false)
			planner, ok := orch.organizer.(artifactPlanExecutor)
			require.True(t, ok)
			plan, err := planner.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: movie, DestDir: dest, MoveFiles: true, ForceUpdate: tc.force})
			require.NoError(t, err)
			tracker := organizer.NewDuplicateTracker(false)
			winner := filepath.Join(root, "incoming", "winner.mp4")
			_, duplicated := tracker.ObserveClaim(context.Background(), winner, plan.TargetPath, true)
			require.False(t, duplicated)
			tracker.SettleClaim(winner, plan.TargetPath)
			cmd := pr260ArtifactFailureCommand(movie, match, dest)
			cmd.Download = false
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = true
			cmd.Organize.ForceUpdate = tc.force
			cmd.Organize.DuplicateTracker = tracker
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.Nil(t, stage, "duplicate may not allocate a stage")
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
			_, duplicate := tracker.ObserveClaim(context.Background(), source, plan.TargetPath, true)
			require.True(t, duplicate, "resolved winner must retain its completed destination claim")
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
			pr260AssertNoFinals(t, base, dest)
			pr260AssertStageGone(t, base, root)
		})
	}
}
