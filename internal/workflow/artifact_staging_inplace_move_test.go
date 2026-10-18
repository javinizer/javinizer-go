package workflow

import (
	"bytes"
	"context"
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

func TestInPlaceMovePreparationDefersVideoPayloads(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		t.Run(string(mode), func(t *testing.T) {
			fs, _, source, _, multipart, _, match := pr260FencedFiles(t, "inplace-native-defer")
			payload := bytes.Repeat([]byte("large-video"), 8192)
			require.NoError(t, afero.WriteFile(fs, source, payload, 0o644))
			require.NoError(t, afero.WriteFile(fs, multipart, payload, 0o644))
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "inplace-native-defer"}, match, filepath.Dir(source))
			cmd.OperationMode, cmd.Download = mode, false
			cmd.Organize.Skip, cmd.Organize.MoveFiles = false, true
			stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			require.NotNil(t, stage)
			t.Cleanup(stage.cleanup)
			require.True(t, stage.videoStagingDeferred())
			_, err = fs.Stat(stage.stagedSource)
			require.True(t, os.IsNotExist(err), "a move must not stage the primary payload")
			for _, sibling := range stage.siblings {
				if sibling.sourcePath == multipart {
					require.True(t, sibling.deferred)
					require.Empty(t, sibling.stagedPath)
				}
			}
		})
	}
}

func TestInPlaceMoveNativePublicationPreservesInode(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		t.Run(string(mode), func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, "native-inplace-publish", "")
			fs := afero.NewOsFs()
			root := t.TempDir()
			input := filepath.Join(root, "incoming")
			require.NoError(t, fs.MkdirAll(input, 0o755))
			directoryBefore, err := fs.Stat(input)
			require.NoError(t, err)
			require.True(t, os.SameFile(directoryBefore, directoryBefore))
			require.NoError(t, fs.MkdirAll(filepath.Join(input, "extras"), 0o755))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(input, "keep.txt"), []byte("existing notes"), 0o644))
			require.NoError(t, afero.WriteFile(fs, filepath.Join(input, "extras", "bonus.mkv"), []byte("existing extra"), 0o644))
			source := filepath.Join(input, movie.ID+" - original.mkv")
			payload := bytes.Repeat([]byte("native-video"), 8192)
			require.NoError(t, afero.WriteFile(fs, source, payload, 0o644))
			before, err := fs.Stat(source)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, before))
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
			m, err := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, err)
			orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed-folder", FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, template.NewEngine(), m)
			match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), MovieID: movie.ID, Extension: ".mkv"}
			cmd := pr260FencedCommand(&movie, match, input, pr260FencedCounter(t, db), mode, false, true, organizer.LinkModeNone, false, true)
			result, err := orch.Execute(context.Background(), cmd)
			require.NoError(t, err)
			require.NotNil(t, result.OrganizeResult)
			targetDir := input
			if mode == operationmode.OperationModeInPlace {
				targetDir = filepath.Join(root, "renamed-folder")
			}
			target := filepath.Join(targetDir, movie.ID+".mkv")
			require.Equal(t, target, result.OrganizeResult.NewPath)
			require.FileExists(t, filepath.Join(targetDir, "keep.txt"))
			require.FileExists(t, filepath.Join(targetDir, "extras", "bonus.mkv"))
			directoryAfter, err := fs.Stat(targetDir)
			require.NoError(t, err)
			require.True(t, os.SameFile(directoryBefore, directoryAfter), "in-place must rename the original directory object")
			if mode == operationmode.OperationModeInPlace {
				require.True(t, result.OrganizeResult.InPlaceRenamed)
				require.Equal(t, input, result.OrganizeResult.OldDirectoryPath)
				require.Equal(t, targetDir, result.OrganizeResult.NewDirectoryPath)
				require.NoDirExists(t, input)
			}
			after, err := fs.Stat(target)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after), "same-volume move must retain the admitted video object")
			data, err := afero.ReadFile(fs, target)
			require.NoError(t, err)
			require.Equal(t, payload, data)
			require.FileExists(t, result.NFOPath)
			require.Equal(t, targetDir, filepath.Dir(result.NFOPath))
			_, err = fs.Stat(source)
			require.True(t, os.IsNotExist(err))
			pr260AssertStageGone(t, fs, root)
		})
	}
}
