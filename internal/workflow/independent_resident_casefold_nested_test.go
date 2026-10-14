package workflow

import (
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentResidentCaseOnlyDirectoryNestedCleanup(t *testing.T) {
	root := t.TempDir()
	fs := &stagedCaseFoldFS{Fs: afero.NewOsFs(), root: root}
	dir := filepath.Join(root, "abc-123")
	source := filepath.Join(dir, "abc-123.mp4")
	require.NoError(t, fs.MkdirAll(dir, 0755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("primary"), 0644))
	before, e := statResidentTestIdentity(t, fs, source)
	require.NoError(t, e)
	db, dbPath := pr260ArtifactDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	movie := models.Movie{ID: "ABC-123", ContentID: "case-nested"}
	engine := template.NewEngine()
	mt, e := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, e)
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, engine, mt)
	orch := newApplyOrchestrator(fs, org, residentNestedArtifactDownloader{fs}, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "case-nested", fs, nil, nil, nil), nil, nil)
	cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeInPlace, Download: true, PublicationFence: independentArtifactFence{&movie}}
	result, e := orch.Execute(t.Context(), cmd)
	require.NoError(t, e)
	require.True(t, result.OrganizeResult.PrimaryResident)
	t.Logf("final %s", result.OrganizeResult.FolderPath)
	require.NoError(t, db.Close())
	db, e = database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "error"})
	require.NoError(t, e)
	defer db.Close()
	repo = database.NewBatchFileOperationRepository(db)
	undo, e := history.NewReverter(fs, repo).RevertBatch(t.Context(), "case-nested")
	require.NoError(t, e)
	require.Equal(t, 1, undo.Succeeded)
	_, e = fs.Stat(filepath.Join(dir, "owned"))
	require.True(t, os.IsNotExist(e), "case-only folder alias should prune generated nested empty dirs too")
	after, e := statResidentTestIdentity(t, fs, source)
	require.NoError(t, e)
	require.True(t, sameResidentTestIdentity(before, after))
}
