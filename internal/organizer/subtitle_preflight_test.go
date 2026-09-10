package organizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func TestOrganizeSubtitlePreflightOsFs(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(map[bool]string{false: "copy", true: "move"}[move], func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "ABC-123.mkv")
			sub := filepath.Join(dir, "ABC-123.srt")
			require.NoError(t, os.WriteFile(src, []byte("video"), 0o600))
			require.NoError(t, os.WriteFile(sub, []byte("subtitle"), 0o600))
			org := NewOrganizer(afero.NewOsFs(), &Config{
				FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
				OperationMode: operationmode.OperationModeOrganize,
				MoveSubtitles: true, SubtitleExtensions: []string{".srt"},
			}, nil, nil)
			dest := filepath.Join(dir, "out")
			result, err := org.Organize(context.Background(), OrganizeCmd{
				Match: models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: "ABC-123.mkv", Extension: ".mkv"},
				Movie: &models.Movie{ID: "ABC-123"}, DestDir: dest, MoveFiles: move, LinkMode: LinkModeNone,
			})
			require.NoError(t, err)
			require.Len(t, result.Subtitles, 1)
			require.Equal(t, move, result.Subtitles[0].Moved)
			require.Equal(t, !move, result.Subtitles[0].Copied)
			output := filepath.Join(dest, "ABC-123")
			got, err := os.ReadFile(filepath.Join(output, "ABC-123.srt"))
			require.NoError(t, err)
			require.Equal(t, "subtitle", string(got))
			if move {
				require.NoFileExists(t, sub)
			} else {
				require.FileExists(t, sub)
			}
			entries, err := os.ReadDir(output)
			require.NoError(t, err)
			require.Len(t, entries, 2, "video and subtitle only; no probe/staging residue")
		})
	}
}
