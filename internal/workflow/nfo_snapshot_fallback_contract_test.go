package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCaptureSnapshotFallsBackAfterPreferredNFOReadFailure(t *testing.T) {
	ctx := context.Background()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repo := &snapshotJournalRecordingRepo{BatchFileOperationRepositoryInterface: database.NewBatchFileOperationRepository(db)}
	base := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "source")
	require.NoError(t, base.MkdirAll(dir, 0755))
	preferred := filepath.Join(dir, "Preferred.nfo")
	legacy := filepath.Join(dir, "SNAP-001.nfo")
	preferredBytes := []byte("<movie><title>Preferred bytes must not be changed</title></movie>")
	legacyBytes := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n<movie>\r\n  <title>Legacy &amp; original</title>\r\n</movie>\r\n")
	require.NoError(t, afero.WriteFile(base, preferred, preferredBytes, 0644))
	require.NoError(t, afero.WriteFile(base, legacy, legacyBytes, 0644))
	fs := &snapshotPreferredReadFailureFS{Fs: base, denied: preferred}
	cfg := &nfo.Config{FilenameTemplate: "<TITLE>.nfo"}
	engine := template.NewEngine()
	log := NewDBRevertLog(repo, &RevertLogConfig{AllowRevert: true, NFOCfg: cfg}, "snapshot-fallback", fs, engine, nfo.NewNFOImplementor(fs, cfg, engine), nil)
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "SNAP-001", Title: "Preferred"},
		Match:    models.FileMatchInfo{Path: filepath.Join(dir, "SNAP-001.mp4"), MovieID: "SNAP-001"},
		Organize: OrganizeOptions{Skip: true},
		DestPath: dir,
	}
	opID, err := log.Begin(ctx, cmd)
	require.NoError(t, err)
	id, err := strconv.ParseUint(opID, 10, 64)
	require.NoError(t, err)
	before, err := repo.FindByID(ctx, uint(id))
	require.NoError(t, err)
	require.NotEmpty(t, before.GeneratedFiles, "Begin must seed a real active recovery journal")

	log.CaptureSnapshot(ctx, opID, cmd)

	after, err := repo.FindByID(ctx, uint(id))
	require.NoError(t, err)
	require.Equal(t, []string{preferred, legacy}, fs.reads, "a non-not-found error must not prevent trying the legacy candidate")
	require.Equal(t, string(legacyBytes), after.NFOSnapshot, "snapshot bytes, including XML formatting and line endings, must be preserved")
	require.Equal(t, legacy, after.NFOPath, "Undo must restore the candidate that was actually read")
	require.Equal(t, before.GeneratedFiles, after.GeneratedFiles)
	require.Zero(t, repo.journalWrites, "snapshot capture must not rewrite the operation journal")
	require.Equal(t, before.RevertStatus, after.RevertStatus)
	require.Equal(t, before.OriginalPath, after.OriginalPath)
	require.Equal(t, before.OperationType, after.OperationType)
	for path, want := range map[string][]byte{preferred: preferredBytes, legacy: legacyBytes} {
		got, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, want, got, "snapshot capture must be read-only for %s", path)
	}
}

type snapshotPreferredReadFailureFS struct {
	afero.Fs
	denied string
	reads  []string
}

func (fs *snapshotPreferredReadFailureFS) Open(name string) (afero.File, error) {
	fs.reads = append(fs.reads, name)
	if filepath.Clean(name) == fs.denied {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrPermission}
	}
	return fs.Fs.Open(name)
}

type snapshotJournalRecordingRepo struct {
	database.BatchFileOperationRepositoryInterface
	journalWrites int
}

func (repo *snapshotJournalRecordingRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	repo.journalWrites++
	return repo.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
}
