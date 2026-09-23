package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPR260ResidualExecuteStagingFailureDoesNotPublish(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "execute-staging-fault")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "execute-staging-fault"}, match, dest)
	cmd.Organize.Skip = false
	fs := &pr260OpenFailureFs{Fs: base, path: source}
	ledger := &dupGateCapturingRevertLog{}
	orch := &applyOrchImpl{fs: fs, revertLog: ledger}
	result, err := orch.Execute(context.Background(), cmd)
	require.ErrorContains(t, err, "open artifact source")
	require.NotNil(t, result)
	require.Equal(t, "artifact_staging", result.FailedStep)
	require.True(t, result.PrePublication)
	require.Equal(t, OperationID("dup-gate-op"), result.OperationID)
	require.Len(t, ledger.failed, 1)
	require.True(t, ledger.failed[0].PrePublication)
	require.Equal(t, result.OperationID, ledger.failed[0].OperationID)
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertNoFinals(t, base, dest)
	pr260AssertStageGone(t, base, root)
}

func TestPR260ResidualMergeRejectsEscapedStagedPathWithoutChangingMovie(t *testing.T) {
	fs := afero.NewMemMapFs()
	movie := &models.Movie{ID: "MERGE-1", Title: "original"}
	stage := &artifactStage{fs: fs, root: "/stage", finalRoot: "/final"}
	original := models.FileMatchInfo{Path: "/incoming/movie.mp4", Name: "movie.mp4"}
	state := &applyPipelineState{movie: movie, artifact: stage, organizeResult: nil}
	state.organizeResult = &organizer.OrganizeResult{NewPath: "/elsewhere/movie.mp4"}
	orch := &applyOrchImpl{nfo: nfo.NewNFOImplementor(fs, &nfo.Config{}, template.NewEngine())}
	steps := &stepCompletion{}
	err := orch.stepMerge(ApplyCmd{Movie: movie, Match: original}, state, steps)
	require.ErrorContains(t, err, "escapes staging area")
	require.Same(t, movie, state.movie)
	require.Equal(t, "original", state.movie.Title)
	require.False(t, steps.Merged)
}
