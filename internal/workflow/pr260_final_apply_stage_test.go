package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestFinalExecuteStageOpenFailureIsPrepublicationAndRetainsSources(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "final-execute-open")
	dest := filepath.Join(root, "published")
	fs := &pr260OpenFailureFs{Fs: base, path: source}
	movie := &models.Movie{ContentID: "final-execute-open"}
	cmd := pr260ArtifactFailureCommand(movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	result, err := (&applyOrchImpl{fs: fs}).Execute(context.Background(), cmd)
	require.ErrorContains(t, err, "open artifact source")
	require.NotNil(t, result)
	require.Equal(t, "artifact_staging", result.FailedStep)
	require.True(t, result.PrePublication)
	require.Same(t, movie, result.Movie)
	pr260AssertNoFinals(t, base, dest)
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
}
