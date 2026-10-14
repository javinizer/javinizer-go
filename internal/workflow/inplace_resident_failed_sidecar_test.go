package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestInPlaceResidentSidecarFailureComposition(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, kind := range []string{"subtitle", "multipart"} {
			for _, blocked := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/blocked=%v", mode, kind, blocked), func(t *testing.T) {
					fs := afero.NewOsFs()
					root := t.TempDir()
					old := filepath.Join(root, "incoming")
					current := old
					if mode == operationmode.OperationModeInPlace {
						current = filepath.Join(root, "ABC-123")
					}
					require.NoError(t, fs.Mkdir(old, 0755))
					sourceName := "abc-123-source.mp4"
					targetName := "ABC-123.mp4"
					require.NoError(t, afero.WriteFile(fs, filepath.Join(old, sourceName), []byte("resident"), 0644))
					require.NoError(t, os.Link(filepath.Join(old, sourceName), filepath.Join(old, targetName)))
					primary, err := fs.Stat(filepath.Join(old, targetName))
					require.NoError(t, err)
					sideName, sideTarget := "abc-123-source.en.srt", "ABC-123.eng.srt"
					if kind == "multipart" {
						sideName, sideTarget = "abc-123-source-cd2.mp4", "ABC-123-cd2.mp4"
					}
					require.NoError(t, afero.WriteFile(fs, filepath.Join(old, sideName), []byte("sidecar"), 0644))
					db, _ := pr260ArtifactDB(t)
					repo := database.NewBatchFileOperationRepository(db)
					movie := models.Movie{ID: "ABC-123", ContentID: "inplace-sidecar-failure"}
					engine := template.NewEngine()
					mt, err := matcher.NewMatcher(&matcher.Config{})
					require.NoError(t, err)
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, engine, mt)
					orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "failed-sidecar", fs, nil, nil, nil), nil, nil)
					fence := postPublishFence{movie: &movie, err: os.ErrPermission, after: func() {
						require.FileExists(t, filepath.Join(current, sideTarget))
						if blocked {
							require.NoError(t, afero.WriteFile(fs, filepath.Join(current, sideName), []byte("foreign claimant"), 0644))
						}
					}}
					cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: filepath.Join(old, sourceName), Name: sourceName, Extension: ".mp4", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: mode, PublicationFence: fence}
					result, err := orch.Execute(t.Context(), cmd)
					require.ErrorIs(t, err, os.ErrPermission)
					require.Equal(t, !blocked, result.PrePublication)
					rows, err := repo.FindByBatchJobID(t.Context(), "failed-sidecar")
					require.NoError(t, err)
					require.Len(t, rows, 1)
					if blocked {
						require.Equal(t, models.RevertStatusFailed, rows[0].RevertStatus)
						body, err := afero.ReadFile(fs, filepath.Join(current, sideName))
						require.NoError(t, err)
						require.Equal(t, "foreign claimant", string(body))
						require.NoError(t, fs.Remove(filepath.Join(current, sideName)))
						require.NoError(t, fs.Remove(filepath.Join(current, sourceName)))
						undo, err := history.NewReverter(fs, repo).RevertBatch(t.Context(), "failed-sidecar")
						require.NoError(t, err)
						require.Equal(t, 1, undo.Succeeded)
						require.Zero(t, undo.Failed)
					} else {
						require.Equal(t, models.RevertStatusNoOp, rows[0].RevertStatus)
					}
					body, err := afero.ReadFile(fs, filepath.Join(old, sideName))
					require.NoError(t, err)
					require.Equal(t, "sidecar", string(body))
					after, err := fs.Stat(filepath.Join(old, targetName))
					require.NoError(t, err)
					require.True(t, os.SameFile(primary, after))
				})
			}
		}
	}
}
