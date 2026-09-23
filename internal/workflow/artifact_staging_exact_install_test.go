package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestPR260ClosureStageSiblingAndWalkFaults(t *testing.T) {
	for _, tc := range []struct{ name, op, want string }{{"sibling stat", "stat", "artifact staging sibling"}, {"walk root", "open", "walk staged artifacts"}} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, sub, part, other, match := pr260FencedFiles(t, "closure-"+tc.name)
			dest := filepath.Join(root, "published")
			fs := &pr260ClosureFS{Fs: base}
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "closure-" + tc.name}, match, dest)
			if tc.op == "stat" {
				cmd.Organize.Skip = false
				fs.op = "stat"
				fs.path = part
				fs.enabled = true
				stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
				require.Nil(t, stage)
				require.ErrorContains(t, err, tc.want)
			} else {
				stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
				require.NoError(t, err)
				fs.op = "open"
				fs.path = stage.root
				fs.enabled = true
				_, err = stage.installTree("", "", nil, "", "")
				require.ErrorContains(t, err, tc.want)
				fs.enabled = false
				stage.cleanup()
			}
			pr260AssertRetained(t, base, source, sub, part, other)
			pr260AssertNoFinals(t, base, dest)
			pr260AssertStageGone(t, base, root)
		})
	}
}
func TestPR260ClosureDestinationDirectoryDoesNotReplaceMedia(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "closure-dir-target")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "closure-dir-target"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	staged := filepath.Join(stage.root, "poster.jpg")
	target := filepath.Join(dest, "poster.jpg")
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
	require.NoError(t, base.MkdirAll(target, 0o755))
	_, err = stage.installTree("", "", nil, "", "")
	require.ErrorContains(t, err, "artifact destination is a directory")
	b, e := afero.ReadFile(base, staged)
	require.NoError(t, e)
	require.Equal(t, "new", string(b))
	info, e := base.Stat(target)
	require.NoError(t, e)
	require.True(t, info.IsDir())
	stage.cleanup()
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
}
