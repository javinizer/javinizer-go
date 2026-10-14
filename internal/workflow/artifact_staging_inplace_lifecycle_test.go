package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

func TestInPlaceDeferredNativeLifecycle(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, canonical := range []bool{false, true} {
			for _, failCommit := range []bool{false, true} {
				name := string(mode)
				if canonical {
					name += "/unchanged"
				} else {
					name += "/renamed"
				}
				if failCommit {
					name += "/rollback"
				} else {
					name += "/success"
				}
				t.Run(name, func(t *testing.T) {
					fs := afero.NewOsFs()
					movie := models.Movie{ID: "IPX-123", ContentID: "native-lifecycle", RenderGeneration: 1}
					input := filepath.Join(t.TempDir(), "renamed-folder")
					require.NoError(t, fs.MkdirAll(input, 0o755))
					stem := movie.ID
					if !canonical {
						stem += " - original"
					}
					source := filepath.Join(input, stem+".mkv")
					part := filepath.Join(input, stem+"-pt2.mkv")
					subtitle := filepath.Join(input, stem+".en.srt")
					foreign := filepath.Join(input, "keep.txt")
					for path, body := range map[string]string{source: "primary media", part: "second media", subtitle: "subtitle", foreign: "foreign file"} {
						require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
					}
					before, err := fs.Stat(source)
					require.NoError(t, err)
					require.True(t, os.SameFile(before, before))
					partBefore, err := fs.Stat(part)
					require.NoError(t, err)
					require.True(t, os.SameFile(partBefore, partBefore))
					m, err := matcher.NewMatcher(&matcher.Config{})
					require.NoError(t, err)
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed-folder", FileFormat: "<ID>", RenameFile: true, OperationMode: mode, MoveSubtitles: true}, template.NewEngine(), m)
					orch := &applyOrchImpl{fs: fs, organizer: org}
					fence := postPublishFence{movie: &movie}
					commitErr := errors.New("commit denied")
					if failCommit {
						fence.err = commitErr
					}
					cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), MovieID: movie.ID, Extension: ".mkv"}, DestPath: input, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: mode, PublicationFence: fence}
					stage, _, err := orch.prepareArtifact(context.Background(), cmd)
					require.NoError(t, err)
					defer stage.cleanup()
					require.True(t, stage.videoStagingDeferred())
					require.NoError(t, afero.WriteFile(fs, filepath.Join(stage.root, movie.ID+".nfo"), []byte("new metadata"), 0o644))
					state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: filepath.Join(stage.root, movie.ID+".mkv"), FolderPath: stage.root}}
					err = stage.publish(context.Background(), orch, state, nil)
					primaryTarget := filepath.Join(input, movie.ID+".mkv")
					partTarget := filepath.Join(input, movie.ID+"-pt2.mkv")
					if failCommit {
						require.ErrorIs(t, err, commitErr)
						primaryTarget, partTarget = source, part
						require.NoFileExists(t, filepath.Join(input, movie.ID+".nfo"))
						require.FileExists(t, subtitle)
					} else {
						require.NoError(t, err)
						require.FileExists(t, filepath.Join(input, movie.ID+".nfo"))
					}
					after, err := fs.Stat(primaryTarget)
					require.NoError(t, err)
					require.True(t, os.SameFile(before, after))
					partAfter, err := fs.Stat(partTarget)
					require.NoError(t, err)
					require.True(t, os.SameFile(partBefore, partAfter))
					body, err := afero.ReadFile(fs, foreign)
					require.NoError(t, err)
					require.Equal(t, "foreign file", string(body))
				})
			}
		}
	}
}
