package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestPR260ExactSiblingCopyAndMissingMetadataSource(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-sidecopy")
	fs := &pr260FiniteCopyFS{Fs: base, op: "staged create", source: sub}
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "exact-sidecopy"}, match, dest)
	cmd.Organize.Skip = false
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.Nil(t, stage)
	require.ErrorContains(t, err, "create staged source")
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
	pr260AssertNoFinals(t, base, dest)
	cmd.Organize.Skip = true
	cmd.GenerateNFO = true
	cmd.OperationMode = operationmode.OperationModeMetadataArtwork
	cmd.DestPath = ""
	cmd.Match.Path = ""
	stage, _, err = (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.Nil(t, stage)
	require.ErrorContains(t, err, "requires a source path")
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
}
func TestPR260ExactGuardedDirtyAndInvalidRel(t *testing.T) {
	markArtifactDirty(ApplyCmd{})
	markArtifactDirty(ApplyCmd{Movie: &models.Movie{ContentID: " ", RenderGeneration: 2}, PublicationFence: pr260FailureArtifactFencer{}})
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-rel")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "exact-rel"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	_, err = stage.finalPath("relative-path.nfo")
	require.ErrorContains(t, err, "escapes staging area")
	stage.cleanup()
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
	pr260AssertNoFinals(t, base, dest)
}
func TestPR260ExactClaimWithTrackerGoneDoesNotMutateSource(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-claimnil")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "exact-claimnil"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	stage.duplicatePlan = &organizer.OrganizePlan{}
	stage.finishDuplicateClaim(context.Canceled)
	require.Nil(t, stage.duplicatePlan)
	stage.cleanup()
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
	pr260AssertNoFinals(t, base, dest)
}
