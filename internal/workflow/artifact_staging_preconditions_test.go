package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260ArtifactStagingPreconditionsRetainAllInputs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ApplyCmd, string)
		want   string
	}{
		{"missing destination", func(c *ApplyCmd, _ string) { c.DestPath = "" }, "requires a destination path"},
		{"missing source", func(c *ApplyCmd, _ string) { c.Match.Path = "" }, "requires a source path"},
		{"nonregular source", func(c *ApplyCmd, source string) { c.Match.Path = filepath.Dir(source) }, "non-regular source"},
		{"missing source file", func(c *ApplyCmd, source string) { c.Match.Path = source + ".missing" }, "artifact staging source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, tc.name)
			dest := filepath.Join(root, "published")
			cmd := ApplyCmd{Movie: &models.Movie{ContentID: "PR260-" + tc.name}, PublicationFence: pr260FailureArtifactFencer{}, Match: match, DestPath: dest, Organize: OrganizeOptions{MoveFiles: true}}
			tc.change(&cmd, source)
			stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, stage)
			pr260AssertNoFinals(t, fs, dest)
			pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
			pr260AssertStageGone(t, fs, root)
		})
	}
}

func TestPR260ArtifactStageRejectsEscapedResultWithoutPublishing(t *testing.T) {
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "escaped-result")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "PR260-escaped-result"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	state := &applyPipelineState{organizeResult: nil}
	_, err = stage.finalPath(filepath.Join(root, "outside.nfo"))
	require.ErrorContains(t, err, "escapes staging area")
	state.nfoPath = filepath.Join(root, "outside.nfo")
	err = stage.publish(context.Background(), &applyOrchImpl{fs: fs}, state, nil)
	require.ErrorContains(t, err, "escapes staging area")
	pr260AssertNoFinals(t, fs, dest)
	pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
}
