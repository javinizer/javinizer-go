package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

// The deferred publication's copy lane publishes the ADMITTED source's
// permission bits: organizing a private (0640) source must not widen it to the
// umask-masked staging default on its way into the library (codex P2,
// PRRT_kwDORn9KaM6pw_EY).
func TestDeferredCopyPreservesAdmittedSourceMode(t *testing.T) {
	base, root, source, _, _, _, match := pr260FencedFiles(t, "verified-copy-mode")
	require.NoError(t, os.Chmod(source, 0o640))
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	orch := &applyOrchImpl{fs: base, organizer: org}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "verified-copy-mode"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, org, base, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.NoError(t, publishErr)
	target := filepath.Join(dest, "movie", "movie.mp4")
	info, err := os.Lstat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the admitted source's bits survive the deferred copy")
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got))
	exists, existsErr := afero.Exists(base, source)
	require.NoError(t, existsErr)
	assert.True(t, exists, "copy mode retains the source")
}
