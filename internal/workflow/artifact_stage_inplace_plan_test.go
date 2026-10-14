package workflow

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInPlaceFinalRootRemainsWithinSourceParent(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "input", "movie.mkv")
	for _, dir := range []string{filepath.Join(parent, "renamed"), filepath.Join(parent, "renamed", "nested")} {
		stage := &artifactStage{sourcePath: source, finalRoot: filepath.Dir(source)}
		require.NoError(t, stage.setInPlaceFinalRoot(dir))
		require.Equal(t, dir, stage.finalRoot)
	}
	for _, dir := range []string{filepath.Join(filepath.Dir(parent), "outside"), "relative-target"} {
		stage := &artifactStage{sourcePath: source, finalRoot: filepath.Dir(source)}
		require.ErrorContains(t, stage.setInPlaceFinalRoot(dir), "escapes staging area")
		require.Equal(t, filepath.Dir(source), stage.finalRoot)
	}
}
