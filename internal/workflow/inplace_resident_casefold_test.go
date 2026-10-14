package workflow

import (
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

// This OS-backed folding seam is portable evidence only. The independent
// native Windows fixture separately proves actual volume case semantics in CI.
func TestInPlaceResidentCaseFoldDirectoryUndo(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, folder := range []string{"abc-123", "incoming"} {
			t.Run(string(mode)+"/"+folder, func(t *testing.T) {
				root := t.TempDir()
				fs := &stagedCaseFoldFS{Fs: afero.NewOsFs(), root: root}
				dir := filepath.Join(root, folder)
				require.NoError(t, fs.MkdirAll(dir, 0755))
				source := filepath.Join(dir, "abc-123.mp4")
				require.NoError(t, afero.WriteFile(fs, source, []byte("resident case alias"), 0644))
				primary, err := fs.Stat(source)
				require.NoError(t, err)
				di, err := fs.Stat(dir)
				require.NoError(t, err)
				db, _ := pr260ArtifactDB(t)
				repo := database.NewBatchFileOperationRepository(db)
				movie := models.Movie{ID: "ABC-123", ContentID: "inplace-casefold"}
				engine := template.NewEngine()
				mt, err := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, err)
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, engine, mt)
				log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "casefold", fs, nil, nil, nil)
				orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, log, nil, nil)
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: mode, PublicationFence: postPublishFence{movie: &movie}}
				result, err := orch.Execute(t.Context(), cmd)
				require.NoError(t, err)
				require.True(t, result.OrganizeResult.PrimaryResident)
				undo, err := history.NewReverter(fs, repo).RevertBatch(t.Context(), "casefold")
				require.NoError(t, err)
				require.Equal(t, 1, undo.Succeeded)
				require.Zero(t, undo.Failed)
				now, err := fs.Stat(source)
				require.NoError(t, err)
				require.True(t, os.SameFile(primary, now))
				now, err = fs.Stat(dir)
				require.NoError(t, err)
				require.True(t, os.SameFile(di, now))
			})
		}
	}
}
