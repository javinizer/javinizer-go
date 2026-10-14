package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

type deniedStageParentFS struct {
	afero.Fs
	parent string
	denied error
}

func (f *deniedStageParentFS) Mkdir(path string, perm os.FileMode) error {
	if filepath.Clean(filepath.Dir(path)) == filepath.Clean(f.parent) && strings.HasPrefix(filepath.Base(path), artifactStageDirPrefix) {
		return fmt.Errorf("staging parent: %w", f.denied)
	}
	return f.Fs.Mkdir(path, perm)
}

func TestArtifactStageParentPermissionFallback(t *testing.T) {
	fs, root, source, _, _, _, match := pr260FencedFiles(t, "permission-fallback")
	wrapped := &deniedStageParentFS{Fs: fs, parent: root, denied: os.ErrPermission}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "permission-fallback"}, match, filepath.Dir(source))
	cmd.OperationMode = operationmode.OperationModeInPlaceNoRenameFolder
	cmd.Organize.MoveFiles, cmd.Organize.Skip = true, false
	cmd.Download = false
	stage, _, err := (&applyOrchImpl{fs: wrapped}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	require.Equal(t, filepath.Dir(source), filepath.Dir(stage.root))
	_, authenticated := readArtifactStageAuthenticatedManifest(wrapped, stage.root)
	require.True(t, authenticated)
	stage.cleanup()
	require.FileExists(t, source)
	pr260AssertStageGone(t, wrapped, filepath.Dir(source))
}

func TestArtifactStageParentUnexpectedErrorRefusesFallback(t *testing.T) {
	fs, root, source, _, _, _, match := pr260FencedFiles(t, "parent-io-error")
	denied := errors.New("unrelated parent IO failure")
	wrapped := &deniedStageParentFS{Fs: fs, parent: root, denied: denied}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "parent-io-error"}, match, filepath.Dir(source))
	_, _, err := (&applyOrchImpl{fs: wrapped}).prepareArtifact(context.Background(), cmd)
	require.ErrorIs(t, err, denied)
	pr260AssertStageGone(t, fs, filepath.Dir(source))
	require.FileExists(t, source)
}

func TestArtifactStageParentNativeReadonlyParent(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires native POSIX permission enforcement")
	}
	fs := afero.NewOsFs()
	parent := t.TempDir()
	folder := filepath.Join(parent, "writable-media")
	require.NoError(t, os.Mkdir(folder, 0o755))
	source := filepath.Join(folder, "MIRD-281.mkv")
	require.NoError(t, os.WriteFile(source, []byte("video"), 0o644))
	require.NoError(t, os.Chmod(parent, 0o555))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	probeErr := os.WriteFile(filepath.Join(parent, "unwritable-parent-probe"), []byte("x"), 0o600)
	require.ErrorIs(t, probeErr, os.ErrPermission)
	aside := filepath.Join(folder, "renamed-video.mkv")
	require.NoError(t, os.Rename(source, aside))
	require.NoError(t, os.Rename(aside, source))
	movie := models.Movie{ID: "MIRD-281", ContentID: "native-parent-permissions", RenderGeneration: 1}
	cmd := pr260ArtifactFailureCommand(&movie, models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv"}, folder)
	cmd.OperationMode, cmd.Download = operationmode.OperationModeInPlaceNoRenameFolder, false
	cmd.Organize.Skip, cmd.Organize.MoveFiles = false, true
	cmd.PublicationFence = postPublishFence{movie: &movie}
	orch := &applyOrchImpl{fs: fs, organizer: organizer.NewOrganizer(fs, &organizer.Config{FileFormat: "renamed-video", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)}
	before, err := fs.Stat(source)
	require.NoError(t, err)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	require.Equal(t, folder, filepath.Dir(stage.root))
	_, authenticated := readArtifactStageAuthenticatedManifest(fs, stage.root)
	require.True(t, authenticated)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(stage.root, "renamed-video.nfo"), []byte("metadata"), 0o644))
	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: filepath.Join(stage.root, "renamed-video.mkv"), FolderPath: stage.root}}
	require.NoError(t, stage.publish(context.Background(), orch, state, nil))
	after, err := fs.Stat(aside)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	require.FileExists(t, filepath.Join(folder, "renamed-video.nfo"))
	stage.cleanup()
	require.NoFileExists(t, source)
	pr260AssertStageGone(t, fs, folder)
}
