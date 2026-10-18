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

func TestInPlaceResidentFailedDirectoryRollbackRemainsRevertable(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	old := filepath.Join(root, "incoming")
	target := filepath.Join(root, "ABC-123")
	require.NoError(t, base.Mkdir(old, 0755))
	source := filepath.Join(old, "abc-123-source.mp4")
	alias := filepath.Join(old, "ABC-123.mp4")
	require.NoError(t, afero.WriteFile(base, source, []byte("resident"), 0644))
	require.NoError(t, os.Link(source, alias))
	primary, err := base.Stat(source)
	require.NoError(t, err)
	directory, err := base.Stat(old)
	require.NoError(t, err)
	poisoned := false
	fs := &directoryFaultFS{Fs: base, beforeRename: func(_, dst string) error {
		if poisoned && dst == old {
			return os.ErrPermission
		}
		return nil
	}, afterRename: func(_, dst string) error {
		if dst == target && !poisoned {
			poisoned = true
			p := filepath.Join(target, "ABC-123.mp4")
			if err := base.Remove(p); err != nil {
				return err
			}
			return afero.WriteFile(base, p, []byte("foreign alias"), 0644)
		}
		return nil
	}}
	db, _ := pr260ArtifactDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	movie := models.Movie{ID: "ABC-123", ContentID: "directory-rollback-failure"}
	engine := template.NewEngine()
	mt, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, engine, mt)
	orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "failed-directory", fs, nil, nil, nil), nil, nil)
	cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeInPlace, PublicationFence: postPublishFence{movie: &movie}}
	result, err := orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	require.True(t, poisoned)
	require.False(t, result.PrePublication, "owned directory remains displaced despite having no sidecar mutation")
	rows, err := repo.FindByBatchJobID(t.Context(), "failed-directory")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, models.RevertStatusFailed, rows[0].RevertStatus)
	// Retry from the durable ledger with fault injection removed. The source
	// alias and the foreign target both survive the directory-only inverse.
	undo, err := history.NewReverter(base, repo).RevertBatch(t.Context(), "failed-directory")
	require.NoError(t, err)
	require.Equal(t, 1, undo.Succeeded)
	require.Zero(t, undo.Failed)
	after, err := base.Stat(old)
	require.NoError(t, err)
	require.True(t, os.SameFile(directory, after))
	after, err = base.Stat(source)
	require.NoError(t, err)
	require.True(t, os.SameFile(primary, after))
	body, err := afero.ReadFile(base, alias)
	require.NoError(t, err)
	require.Equal(t, "foreign alias", string(body))
}
