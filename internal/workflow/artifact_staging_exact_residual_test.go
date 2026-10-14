package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestPR260ExactNonregularMatchingSidecarSkipped(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "exact-symlink")
	link := filepath.Join(filepath.Dir(source), "PR260-EXACT-SYMLINK.fr.srt")
	outsideDir := filepath.Join(root, "link-target")
	require.NoError(t, os.Mkdir(outsideDir, 0o755))
	require.NoError(t, os.Symlink(outsideDir, link))
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "exact-symlink"}, match, filepath.Join(root, "published"))
	cmd.Organize.Skip = false
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	for _, s := range stage.siblings {
		require.NotEqual(t, link, s.sourcePath)
	}
	info, e := os.Lstat(link)
	require.NoError(t, e)
	require.True(t, info.Mode()&os.ModeSymlink != 0)
	stage.cleanup()
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
	pr260AssertNoFinals(t, base, cmd.DestPath)
}
