package workflow

import (
	"context"
	"errors"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type pr260FinalInspectFS struct {
	afero.Fs
	path, op string
	enabled  bool
}

func (f *pr260FinalInspectFS) Stat(p string) (os.FileInfo, error) {
	if f.enabled && f.op == "stat" && p == f.path {
		return nil, errors.New("final inspect denied")
	}
	return f.Fs.Stat(p)
}
func (f *pr260FinalInspectFS) Open(p string) (afero.File, error) {
	if f.enabled && f.op == "open" && p == f.path {
		return nil, errors.New("sidecar copy denied")
	}
	return f.Fs.Open(p)
}
func TestPR260ExactPostOrganizeFaultLeavesPublishedVideoAndInputs(t *testing.T) {
	for _, tc := range []struct{ name, op, want string }{{"target inspect", "stat", "inspect publication sidecar"}, {"staged copy", "open", "publish sidecar before source cleanup"}} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, "exact-"+tc.name, "")
			base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-"+tc.name)
			fs := &pr260FinalInspectFS{Fs: base, op: tc.op}
			dest := filepath.Join(root, "library")
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
			cmd := pr260FencedCommand(&movie, match, dest, pr260FencedCounter(t, db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
			planner := orch.organizer.(artifactPlanExecutor)
			plan, e := planner.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: cmd.Match, Movie: cmd.Movie, DestDir: dest, MoveFiles: true})
			require.NoError(t, e)
			target := filepath.Join(filepath.Dir(plan.TargetPath), stagedArtifactSiblingName(filepath.Base(source), filepath.Base(plan.TargetPath), filepath.Base(sub)))
			if tc.op == "stat" {
				fs.path = target
			} else {
				fs.path = stage.siblings[0].stagedPath
			}
			fs.enabled = true
			err = stage.publish(context.Background(), orch, state, nil)
			require.ErrorContains(t, err, tc.want)
			exists, e := afero.Exists(base, source)
			require.NoError(t, e)
			require.True(t, exists)
			pr260AssertRetained(t, base, source, sub, part, other)
			entries, e := afero.ReadDir(base, dest)
			require.NoError(t, e)
			require.Empty(t, entries, "post-video fault rolls back the published output atomically")
			fs.enabled = false
			stage.cleanup()
			pr260AssertStageGone(t, base, root)
		})
	}
}
