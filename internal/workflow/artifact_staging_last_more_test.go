package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestPR260FiniteSidecarMissingDuringRehomeLeavesOriginal(t *testing.T) {
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "missing-sidecar")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-missing-sidecar"}, match, filepath.Join(root, "published"))
	cmd.Organize.Skip = false
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, stage.siblings)
	absent := stage.siblings[0].stagedPath
	require.NoError(t, fs.Remove(absent))
	stagedVideo := filepath.Join(stage.root, "renamed.mp4")
	err = stage.rehomeRemainingSiblings(stagedVideo)
	require.NoError(t, err)
	exists, err := afero.Exists(fs, absent)
	require.NoError(t, err)
	require.False(t, exists)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
	stage.cleanup()
	pr260AssertStageGone(t, fs, root)
	pr260AssertNoFinals(t, fs, cmd.DestPath)
}

func TestPR260FinitePublicationRequiresOrganizeResultRetainsInputs(t *testing.T) {
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "no-organize-result")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-no-organize-result"}, match, dest)
	cmd.Organize.Skip = false
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	err = stage.publish(context.Background(), &applyOrchImpl{fs: fs}, &applyPipelineState{}, nil)
	require.ErrorContains(t, err, "has no organize result")
	pr260AssertNoFinals(t, fs, dest)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
	stage.cleanup()
	pr260AssertStageGone(t, fs, root)
}

func TestPR260FiniteStageNoArtifactAndInPlaceDestinationFallback(t *testing.T) {
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "fallback")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-fallback"}, match, filepath.Join(root, "published"))
	orch := &applyOrchImpl{fs: fs}
	cmd.DryRun = true
	stage, unchanged, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Nil(t, stage)
	require.Equal(t, cmd.DestPath, unchanged.DestPath)
	cmd.DryRun = false
	cmd.Download = false
	cmd.Organize.Skip = true
	stage, _, err = orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Nil(t, stage)
	cmd.Download = true
	cmd.OperationMode = operationmode.OperationModeInPlaceNoRenameFolder
	cmd.DestPath = ""
	stage, staged, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, filepath.Dir(source), stage.finalRoot)
	require.Equal(t, stage.root, staged.DestPath)
	stage.cleanup()
	pr260AssertStageGone(t, fs, root)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
}

func TestPR260FinitePublishCanceledBeforeFenceRetainsInputs(t *testing.T) {
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "publish-canceled")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-publish-canceled"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	staged := filepath.Join(stage.root, "movie.nfo")
	require.NoError(t, afero.WriteFile(fs, staged, []byte("rendered"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := &applyPipelineState{nfoPath: staged}
	err = stage.publish(ctx, &applyOrchImpl{fs: fs}, state, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, staged, state.nfoPath)
	stage.cleanup()
	pr260AssertStageGone(t, fs, root)
	pr260AssertNoFinals(t, fs, dest)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
}
