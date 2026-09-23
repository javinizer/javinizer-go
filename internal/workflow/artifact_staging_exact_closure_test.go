package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type pr260ClosureFS struct {
	afero.Fs
	op, path string
	enabled  bool
}

func (f *pr260ClosureFS) MkdirAll(p string, m os.FileMode) error {
	if f.enabled && f.op == "mkdir" && p == f.path {
		return errors.New("mkdir denied")
	}
	return f.Fs.MkdirAll(p, m)
}
func (f *pr260ClosureFS) Mkdir(p string, m os.FileMode) error {
	if f.enabled && f.op == "temp" && filepath.Dir(p) == f.path {
		return errors.New("temp denied")
	}
	return f.Fs.Mkdir(p, m)
}
func (f *pr260ClosureFS) Stat(p string) (os.FileInfo, error) {
	if f.enabled && f.op == "stat" && p == f.path {
		return nil, errors.New("stat denied")
	}
	return f.Fs.Stat(p)
}
func (f *pr260ClosureFS) Open(p string) (afero.File, error) {
	if f.enabled && f.op == "open" && p == f.path {
		return nil, errors.New("walk denied")
	}
	return f.Fs.Open(p)
}

func TestPR260ClosurePrepareFenceAndStageParent(t *testing.T) {
	for _, tc := range []struct{ name, op, want string }{{"parent", "mkdir", "create artifact staging parent"}, {"temp", "temp", "create artifact staging area"}} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, sub, part, other, match := pr260FencedFiles(t, "closure-"+tc.name)
			dest := filepath.Join(root, "published")
			fs := &pr260ClosureFS{Fs: base, op: tc.op, path: root, enabled: true}
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "closure-" + tc.name}, match, dest)
			stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, stage)
			pr260AssertNoFinals(t, base, dest)
			pr260AssertRetained(t, base, source, sub, part, other)
			pr260AssertStageGone(t, base, root)
		})
	}
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "closure-no-fence")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "closure"}, match, filepath.Join(root, "published"))
	cmd.PublicationFence = nil
	stage, same, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Nil(t, stage)
	require.Equal(t, cmd.DestPath, same.DestPath)
	cmd.PublicationFence = pr260FailureArtifactFencer{}
	cmd.Movie = nil
	stage, _, err = (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Nil(t, stage)
	cmd.Movie = &models.Movie{ContentID: "  "}
	stage, _, err = (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Nil(t, stage)
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
	pr260AssertNoFinals(t, base, cmd.DestPath)
}

func TestPR260ClosureMappingAndInstallGuard(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "closure-map")
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "closure-map"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, dest, func() string { p, e := stage.finalPath(stage.root); require.NoError(t, e); return p }())
	stage.inPlace = true
	outside := filepath.Join(filepath.Dir(root), "outside.nfo")
	_, err = stage.finalPath(outside)
	require.ErrorContains(t, err, "escapes staging area")
	sibling := filepath.Join(filepath.Dir(dest), "other.nfo")
	mapped, err := stage.finalPath(sibling)
	require.NoError(t, err)
	require.Equal(t, sibling, mapped)
	stage.cleanup()
	stage.cleanup()
	var empty *artifactStage
	empty.cleanup()
	pr260AssertStageGone(t, base, root)
	stage, _, err = (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	stage.inPlace = true
	stage.cleanup()
	preserved, err := stage.installTree("", "", nil, "", "")
	require.NoError(t, err)
	require.False(t, preserved)
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertNoFinals(t, base, dest)
}

func TestPR260ClosurePublicationValidationAndPathMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*artifactStage, *applyPipelineState, string)
		want   string
	}{
		{"movie missing", func(s *artifactStage, _ *applyPipelineState, _ string) { s.original.Movie = nil }, "requires movie content id"},
		{"download escapes", func(_ *artifactStage, state *applyPipelineState, root string) {
			state.downloadPaths = []string{filepath.Join(root, "outside.jpg")}
		}, "escapes staging area"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, sub, part, other, match := pr260FencedFiles(t, "closure-"+tc.name)
			dest := filepath.Join(root, "published")
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "closure-" + tc.name}, match, dest)
			stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			state := &applyPipelineState{}
			tc.mutate(stage, state, root)
			err = stage.publish(context.Background(), &applyOrchImpl{fs: base}, state, nil)
			require.ErrorContains(t, err, tc.want)
			stage.cleanup()
			pr260AssertRetained(t, base, source, sub, part, other)
			pr260AssertStageGone(t, base, root)
		})
	}
}

func TestPR260ClosureHelperStemAndMode(t *testing.T) {
	require.False(t, isStagedArtifactPrefixedStem("abc", "abc"))
	require.False(t, isStagedArtifactPrefixedStem("abc", "abcd"))
	require.True(t, isStagedArtifactPrefixedStem("abc", "abc-fr"))
	require.Equal(t, "abc", stagedArtifactMultipartRoot("ABC-disc2"))
	require.Empty(t, stagedArtifactMultipartRoot("ABC-discX"))
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "closure-mode")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "closure-mode"}, match, "")
	cmd.OperationMode = operationmode.OperationModeMetadataArtwork
	cmd.Organize.Skip = true
	cmd.GenerateNFO = true
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.Equal(t, filepath.Dir(source), stage.finalRoot)
	stage.cleanup()
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
}
