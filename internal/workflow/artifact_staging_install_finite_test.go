package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type pr260FiniteWalkFS struct {
	afero.Fs
	root string
	fail bool
}

func (f *pr260FiniteWalkFS) Stat(name string) (os.FileInfo, error) {
	if f.fail && name == f.root {
		return nil, errors.New("root inspect denied")
	}
	return f.Fs.Stat(name)
}
func TestPR260FiniteInstallRootInspectAndPreservedMedia(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "install-root")
	dest := filepath.Join(root, "published")
	fs := &pr260FiniteWalkFS{Fs: base}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-install-root"}, match, dest)
	stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stage.inPlace = true
	fs.root = stage.root
	fs.fail = true
	_, err = stage.installTree("", "", nil, "", "")
	require.ErrorContains(t, err, "inspect staged artifact root")
	pr260AssertNoFinals(t, base, dest)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	fs.fail = false
	staged := filepath.Join(stage.root, "poster.jpg")
	target := filepath.Join(dest, "poster.jpg")
	require.NoError(t, afero.WriteFile(base, staged, []byte("replacement"), 0o644))
	require.NoError(t, base.MkdirAll(dest, 0o755))
	require.NoError(t, afero.WriteFile(base, target, []byte("current"), 0o644))
	preserved, err := stage.installTree("", "", []string{staged}, "", "")
	require.NoError(t, err)
	require.True(t, preserved)
	b, err := afero.ReadFile(base, target)
	require.NoError(t, err)
	require.Equal(t, "current", string(b))
	exists, err := afero.Exists(base, staged)
	require.NoError(t, err)
	require.True(t, exists)
	stage.original.OverwriteExistingMedia = true
	preserved, err = stage.installTree("", "", []string{staged}, "", "")
	require.NoError(t, err)
	require.False(t, preserved)
	b, err = afero.ReadFile(base, target)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(b))
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}
func TestPR260FiniteInstallDirectoryTargetRetainsStagedPayload(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "install-directory")
	dest := filepath.Join(root, "published")
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-directory"}, match, dest))
	require.NoError(t, err)
	defer stage.cleanup()
	staged := filepath.Join(stage.root, "artwork.jpg")
	target := filepath.Join(dest, "artwork.jpg")
	require.NoError(t, afero.WriteFile(base, staged, []byte("rendered"), 0o644))
	require.NoError(t, base.MkdirAll(target, 0o755))
	_, err = stage.installTree("", "", nil, "", "")
	require.ErrorContains(t, err, "artifact destination is a directory")
	b, e := afero.ReadFile(base, staged)
	require.NoError(t, e)
	require.Equal(t, "rendered", string(b))
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}
