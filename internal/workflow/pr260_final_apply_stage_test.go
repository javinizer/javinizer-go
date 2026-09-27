package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// pr260SourceMoveFaultFs denies the source-consuming rename so the deferred
// organize-mode video publish fails inside the fence.
type pr260SourceMoveFaultFs struct {
	afero.Fs
	path string
}

func (f *pr260SourceMoveFaultFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(f.path) {
		return errors.New("pr260: source move denied")
	}
	return f.Fs.Rename(oldname, newname)
}

// Organize mode defers the video: a denied source move now surfaces at the
// fenced publication (not during staging), stays pre-publication, and retains
// every input.
func TestFinalExecuteStageOpenFailureIsPrepublicationAndRetainsSources(t *testing.T) {
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "final-execute-open")
	dest := filepath.Join(root, "published")
	fs := &pr260SourceMoveFaultFs{Fs: base, path: source}
	movie := &models.Movie{ContentID: "final-execute-open"}
	cmd := pr260ArtifactFailureCommand(movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	result, err := (&applyOrchImpl{fs: fs, organizer: org}).Execute(context.Background(), cmd)
	require.ErrorContains(t, err, "source move denied")
	require.NotNil(t, result)
	require.Equal(t, "artifact_publication", result.FailedStep)
	require.True(t, result.PrePublication)
	require.Same(t, movie, result.Movie)
	pr260AssertNoFinals(t, base, dest)
	pr260AssertRetained(t, base, source, sub, part, other)
	pr260AssertStageGone(t, base, root)
}
