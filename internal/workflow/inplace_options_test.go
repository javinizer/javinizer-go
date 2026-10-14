package workflow

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/require"
)

func TestNormalizeInPlaceLinkMode(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{"", operationmode.OperationModeOrganize, operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder, operationmode.OperationModeMetadataArtwork, operationmode.OperationModePreview} {
		for _, move := range []bool{false, true} {
			for _, link := range []organizer.LinkMode{organizer.LinkModeNone, organizer.LinkModeHard, organizer.LinkModeSoft} {
				t.Run(fmt.Sprintf("%s/move=%v/link=%s", mode, move, link), func(t *testing.T) {
					cmd := ApplyCmd{Movie: &models.Movie{ID: "ABC-123"}, Match: models.FileMatchInfo{Path: "source.mkv"}, DestPath: "library", OperationMode: mode, GenerateNFO: true, Organize: OrganizeOptions{MoveFiles: move, LinkMode: link, ForceRenameFile: true, ForceUpdate: true}}
					original, want := cmd, cmd
					if mode == operationmode.OperationModeInPlace || mode == operationmode.OperationModeInPlaceNoRenameFolder {
						want.Organize.LinkMode = organizer.LinkModeNone
					}
					got := normalizeInPlaceLinkMode(cmd)
					require.Equal(t, want, got, "only the ignored in-place link option changes")
					require.Equal(t, original, cmd, "the caller's value is unchanged")
				})
			}
		}
	}
}

func TestPrepareArtifactNormalizesInPlaceLinkMode(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, move := range []bool{false, true} {
			for _, link := range []organizer.LinkMode{organizer.LinkModeHard, organizer.LinkModeSoft} {
				t.Run(fmt.Sprintf("%s/move=%v/link=%s", mode, move, link), func(t *testing.T) {
					fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "prepare-effective-link")
					orch := &applyOrchImpl{fs: fs, organizer: copyIntentOrganizer(fs)}
					cmd := copyIntentCommand(&models.Movie{ContentID: "prepare-effective-link"}, match, filepath.Join(root, "library"))
					cmd.OperationMode, cmd.Organize.MoveFiles, cmd.Organize.LinkMode = mode, move, link
					stage, stagedCmd, err := orch.prepareArtifact(t.Context(), cmd)
					require.NoError(t, err)
					require.NotNil(t, stage)
					t.Cleanup(stage.cleanup)
					require.Equal(t, organizer.LinkModeNone, stagedCmd.Organize.LinkMode)
					require.Equal(t, organizer.LinkModeNone, stage.original.Organize.LinkMode)
					require.Equal(t, move, stagedCmd.Organize.MoveFiles)
					require.Equal(t, move, stage.original.Organize.MoveFiles)
					require.Equal(t, link, cmd.Organize.LinkMode)
					pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
				})
			}
		}
	}
}
