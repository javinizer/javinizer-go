package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPR260FiniteStagePathMappingAndCancellationRetainsInputs(t *testing.T) {
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "mapping-cancel")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-mapping-cancel"}, match, dest)
	orch := &applyOrchImpl{fs: fs}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stage, _, err := orch.prepareArtifact(ctx, cmd)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, stage)
	pr260AssertNoFinals(t, fs, dest)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
	stage, _, err = orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	stagedNFO := filepath.Join(stage.root, "nested", "movie.nfo")
	mappedNested := filepath.Join(dest, "nested", "movie.nfo")
	mapped, err := stage.finalPath(stagedNFO)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dest, "nested", "movie.nfo"), mapped)
	mapped, err = stage.finalPath(stage.root)
	require.NoError(t, err)
	require.Equal(t, dest, mapped)
	matchResult, err := stage.mergeMatch(&applyPipelineState{organizeResult: nil}, models.FileMatchInfo{Name: "movie.mkv"})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dest, "movie.mkv"), matchResult.Path)
	matchResult, err = stage.mergeMatch(&applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stagedNFO}}, models.FileMatchInfo{})
	require.NoError(t, err)
	require.Equal(t, mappedNested, matchResult.Path)
	_, err = stage.mergeMatch(&applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: filepath.Join(root, "outside.mkv")}}, models.FileMatchInfo{})
	require.ErrorContains(t, err, "escapes staging area")
	stage.cleanup()
	pr260AssertStageGone(t, fs, root)
	pr260AssertNoFinals(t, fs, dest)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
}

func TestPR260FiniteInPlaceMappingRejectsOutsideParent(t *testing.T) {
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "inplace-mapping")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-inplace-mapping"}, match, filepath.Join(root, "published"))
	cmd.OperationMode = operationmode.OperationModeInPlace
	cmd.Organize.Skip = false
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	parentPath := filepath.Join(filepath.Dir(stage.finalRoot), "existing.nfo")
	mapped, err := stage.finalPath(parentPath)
	require.NoError(t, err)
	require.Equal(t, parentPath, mapped)
	_, err = stage.finalPath(filepath.Join(filepath.Dir(root), "outside.nfo"))
	require.ErrorContains(t, err, "escapes staging area")
	stage.cleanup()
	pr260AssertStageGone(t, fs, root)
	pr260AssertNoFinals(t, fs, cmd.DestPath)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
}

func TestPR260FiniteStageSidecarSelectionKeepsUnrelated(t *testing.T) {
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "sidecar-selection")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-sidecar-selection"}, match, filepath.Join(root, "published"))
	cmd.Organize.Skip = false
	stage, stagedCmd, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(stage.root, ".source", filepath.Base(source)), stage.stagedSource)
	require.Equal(t, stage.stagedSource, stagedCmd.Match.Path)
	require.Equal(t, stage.root, stagedCmd.DestPath)
	require.NotEmpty(t, stage.siblings)
	for _, sibling := range stage.siblings {
		require.NotEqual(t, unrelated, sibling.sourcePath)
		_, err := afero.ReadFile(fs, sibling.stagedPath)
		require.NoError(t, err)
	}
	stage.cleanup()
	pr260AssertStageGone(t, fs, root)
	pr260AssertNoFinals(t, fs, cmd.DestPath)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
}
