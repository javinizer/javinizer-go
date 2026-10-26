package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type undoPolicyFixture struct {
	fs                afero.Fs
	db                *database.DB
	repo              database.BatchFileOperationRepositoryInterface
	orch              *applyOrchImpl
	log               *dbRevertLog
	cfg               *RevertLogConfig
	cmd               ApplyCmd
	source, dest, dsn string
	before            os.FileInfo
}

func newUndoPolicyFixture(t *testing.T, enabled bool) *undoPolicyFixture {
	t.Helper()
	root := t.TempDir()
	fs := afero.NewOsFs()
	dir := filepath.Join(root, "library")
	require.NoError(t, fs.MkdirAll(dir, 0755))
	source := filepath.Join(dir, "TEST-001.mp4")
	dest := filepath.Join(dir, "TEST-001.nfo")
	require.NoError(t, afero.WriteFile(fs, source, []byte("synthetic video placeholder"), 0644))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("<movie><title>Before</title></movie>"), 0644))
	before, err := fs.Stat(source)
	require.NoError(t, err)
	dsn := filepath.Join(root, "test.sqlite")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	t.Cleanup(func() { _ = db.Close() })
	repo := database.NewBatchFileOperationRepository(db)
	movie := &models.Movie{ID: "TEST-001", ContentID: "synthetic-policy", Title: "After"}
	engine := template.NewEngine()
	mt, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>"}, engine, mt)
	ncfg := &nfo.Config{FilenameTemplate: "<ID>.nfo", TemplateEngine: engine}
	cfg := NewRevertLogConfig(enabled, nil)
	log := NewDBRevertLog(repo, cfg, "policy-job", fs, nil, nil, nil).(*dbRevertLog)
	orch := newApplyOrchestrator(fs, org, nil, nfo.NewGenerator(fs, ncfg), nfo.NewNFOImplementor(fs, ncfg, engine), ApplyConfig{NFONameCfg: nfo.NFONameConfig{FilenameTemplate: "<ID>.nfo"}}, engine, log, nil, nil)
	cmd := ApplyCmd{Movie: movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: dir, Organize: OrganizeOptions{Skip: true}, OperationMode: operationmode.OperationModeMetadataArtwork, GenerateNFO: true, Merge: MergeOptions{ScalarStrategy: nfo.PreferScraper}, OverwriteExistingMedia: true, PublicationFence: replacedArtifactFence{movie}}
	return &undoPolicyFixture{fs, db, repo, orch, log, cfg, cmd, source, dest, dsn, before}
}

func (f *undoPolicyFixture) ledger(t *testing.T, result *ApplyResult) models.GeneratedFilesJSON {
	t.Helper()
	var id uint
	_, err := fmt.Sscanf(result.OperationID, "%d", &id)
	require.NoError(t, err)
	row, err := f.repo.FindByID(t.Context(), id)
	require.NoError(t, err)
	ledger, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return ledger
}
func (f *undoPolicyFixture) assertVideo(t *testing.T) {
	t.Helper()
	now, err := f.fs.Stat(f.source)
	require.NoError(t, err)
	require.True(t, os.SameFile(f.before, now))
	data, err := afero.ReadFile(f.fs, f.source)
	require.NoError(t, err)
	require.Equal(t, "synthetic video placeholder", string(data))
}

func TestUndoPolicyCapturedBeforePublication(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			f := newUndoPolicyFixture(t, enabled)
			f.cmd.PublicationFence = postPublishFence{movie: f.cmd.Movie, after: func() { f.cfg.AllowRevert = !enabled }}
			result, err := f.orch.Execute(t.Context(), f.cmd)
			require.NoError(t, err)
			ledger := f.ledger(t, result)
			require.NotNil(t, ledger.UndoRetention)
			require.Equal(t, enabled, *ledger.UndoRetention)
			backups, err := filepath.Glob(f.dest + ".dlbak.*")
			require.NoError(t, err)
			if enabled {
				require.Len(t, ledger.Replacements, 1)
				require.Len(t, backups, 1)
			} else {
				require.Empty(t, ledger.Replacements)
				require.Empty(t, backups)
				// Enabling Undo later cannot authorize deleting the new output now that
				// the original bytes were intentionally discarded at successful commit.
				undo, err := history.NewReverter(f.fs, f.repo).RevertBatch(t.Context(), "policy-job")
				require.NoError(t, err)
				require.Equal(t, 1, undo.Failed)
				data, err := afero.ReadFile(f.fs, f.dest)
				require.NoError(t, err)
				require.Contains(t, string(data), "After")
			}
			f.assertVideo(t)
		})
	}
}

type afterCommitReadFaultRepo struct {
	database.BatchFileOperationRepositoryInterface
}

func (r afterCommitReadFaultRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	row, err := r.BatchFileOperationRepositoryInterface.FindByID(ctx, id)
	if err != nil || row == nil {
		return row, err
	}
	ledger, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	if err == nil && ledger.ApplyTerminal != nil && ledger.ApplyTerminal.Outcome == "success" {
		return nil, errors.New("synthetic crash-window read failure after durable commit")
	}
	return row, nil
}

func TestUndoPolicyRestartAfterCommitBeforeCleanup(t *testing.T) {
	f := newUndoPolicyFixture(t, false)
	f.log.repo = afterCommitReadFaultRepo{f.repo}
	result, err := f.orch.Execute(t.Context(), f.cmd)
	require.NoError(t, err)
	require.Contains(t, result.RecoveryWarning, ErrBackupCleanupPending.Error())
	require.True(t, result.PublicationFinalized)
	ledger := f.ledger(t, result)
	require.Len(t, ledger.Replacements, 1)
	backup := ledger.Replacements[0].Backup
	// New configuration does not alter persisted operation policy.
	f.cfg.AllowRevert = true
	require.NoError(t, f.db.Close())
	db, err := database.New(&database.Config{Type: "sqlite", DSN: f.dsn, LogLevel: "error"})
	require.NoError(t, err)
	defer db.Close()
	repo := database.NewBatchFileOperationRepository(db)
	_, err = history.NewReplacementSweeper(f.fs, repo).Sweep(t.Context())
	require.NoError(t, err)
	_, err = f.fs.Stat(backup)
	require.True(t, os.IsNotExist(err))
	data, err := afero.ReadFile(f.fs, f.dest)
	require.NoError(t, err)
	require.Contains(t, string(data), "After")
	f.assertVideo(t)
}

func TestUndoPolicyFailureAndCancellationPreserveRecovery(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			f := newUndoPolicyFixture(t, false)
			fault := errors.New("synthetic post-publication failure")
			if cancel {
				fault = context.Canceled
			}
			f.cmd.PublicationFence = postPublishFence{movie: f.cmd.Movie, err: fault}
			_, err := f.orch.Execute(t.Context(), f.cmd)
			require.ErrorIs(t, err, fault)
			data, err := afero.ReadFile(f.fs, f.dest)
			require.NoError(t, err)
			require.Equal(t, "<movie><title>Before</title></movie>", string(data))
			_, err = history.NewReplacementSweeper(f.fs, f.repo).Sweep(t.Context())
			require.NoError(t, err)
			data, err = afero.ReadFile(f.fs, f.dest)
			require.NoError(t, err)
			require.Contains(t, string(data), "Before")
			f.assertVideo(t)
		})
	}
}

func TestUndoPolicyForeignBackupSubstitutionPreserved(t *testing.T) {
	f := newUndoPolicyFixture(t, false)
	var backup string
	f.cmd.PublicationFence = postPublishFence{movie: f.cmd.Movie, after: func() {
		backups, err := filepath.Glob(f.dest + ".dlbak.*")
		require.NoError(t, err)
		require.Len(t, backups, 1)
		backup = backups[0]
		info, err := f.fs.Stat(backup)
		require.NoError(t, err)
		require.NoError(t, f.fs.Rename(backup, backup+".saved-original"))
		// Same size and timestamp: hash identity must still reject the foreign file.
		foreign := make([]byte, info.Size())
		for i := range foreign {
			foreign[i] = 'X'
		}
		require.NoError(t, afero.WriteFile(f.fs, backup, foreign, info.Mode()))
		require.NoError(t, f.fs.Chtimes(backup, info.ModTime(), info.ModTime()))
	}}
	result, err := f.orch.Execute(t.Context(), f.cmd)
	require.NoError(t, err)
	require.Contains(t, result.RecoveryWarning, ErrBackupCleanupPending.Error())
	require.True(t, result.PublicationFinalized)
	require.Empty(t, result.FailedStep)
	ledger := f.ledger(t, result)
	require.Len(t, ledger.Replacements, 1)
	require.True(t, ledger.Replacements[0].RestorePending)
	data, err := afero.ReadFile(f.fs, backup)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.Equal(t, byte('X'), data[0])
	_, err = history.NewReplacementSweeper(f.fs, f.repo).Sweep(t.Context())
	require.NoError(t, err)
	data, err = afero.ReadFile(f.fs, backup)
	require.NoError(t, err)
	require.Equal(t, byte('X'), data[0])
	data, err = afero.ReadFile(f.fs, f.dest)
	require.NoError(t, err)
	require.Contains(t, string(data), "After")
	f.assertVideo(t)
}

func TestUndoPolicyOrdinaryOrganizeCopy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			f := newUndoPolicyFixture(t, enabled)
			out := filepath.Join(filepath.Dir(f.dsn), "organized")
			finalDir := filepath.Join(out, "TEST-001")
			require.NoError(t, f.fs.MkdirAll(finalDir, 0755))
			finalNFO := filepath.Join(finalDir, "TEST-001.nfo")
			require.NoError(t, afero.WriteFile(f.fs, finalNFO, []byte("<movie><title>Prior destination</title></movie>"), 0644))
			f.cmd.DestPath = out
			f.cmd.Organize = OrganizeOptions{}
			f.cmd.OperationMode = operationmode.OperationModeOrganize
			result, err := f.orch.Execute(t.Context(), f.cmd)
			require.NoError(t, err)
			require.Equal(t, finalNFO, result.NFOPath)
			ledger := f.ledger(t, result)
			backups, err := filepath.Glob(finalNFO + ".dlbak.*")
			require.NoError(t, err)
			if enabled {
				require.Len(t, backups, 1)
				require.Len(t, ledger.Replacements, 1)
			} else {
				require.Empty(t, backups)
				require.Empty(t, ledger.Replacements)
			}
			data, err := afero.ReadFile(f.fs, finalNFO)
			require.NoError(t, err)
			require.Contains(t, string(data), "After")
			data, err = afero.ReadFile(f.fs, result.OrganizeResult.NewPath)
			require.NoError(t, err)
			require.Equal(t, "synthetic video placeholder", string(data))
			f.assertVideo(t)
		})
	}
}

func TestUndoPolicyLedgerMergePreservesExplicitPolicy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, next := range []string{"", `{"roots":["/synthetic"]}`} {
			prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{UndoRetention: &enabled})
			got, err := models.ParseGeneratedFiles(mergeReplacementLedgerIntents(prior, next, true))
			require.NoError(t, err)
			require.NotNil(t, got.UndoRetention)
			require.Equal(t, enabled, *got.UndoRetention)
		}
	}
}

type finalCommitFaultRepo struct {
	database.BatchFileOperationRepositoryInterface
}

func (r finalCommitFaultRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, func(row *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		next, persist, err := fn(row)
		if err == nil && next.ApplyTerminal != nil && next.ApplyTerminal.Outcome == "success" {
			return next, false, context.Canceled
		}
		return next, persist, err
	})
}

func TestUndoPolicyFinalJournalFailureRetainsRecovery(t *testing.T) {
	f := newUndoPolicyFixture(t, false)
	f.log.repo = finalCommitFaultRepo{f.repo}
	result, err := f.orch.Execute(t.Context(), f.cmd)
	require.NoError(t, err, "published data must not be offered for ordinary Apply retry")
	require.NotEmpty(t, result.RecoveryWarning)
	require.False(t, result.PublicationFinalized)
	ledger := f.ledger(t, result)
	require.Nil(t, ledger.ApplyTerminal)
	require.Len(t, ledger.Replacements, 1)
	require.True(t, ledger.Replacements[0].Installed)
	backup := ledger.Replacements[0].Backup
	_, err = history.NewReplacementSweeper(f.fs, f.repo).Sweep(t.Context())
	require.NoError(t, err)
	data, err := afero.ReadFile(f.fs, backup)
	require.NoError(t, err)
	require.Contains(t, string(data), "Before")
	data, err = afero.ReadFile(f.fs, f.dest)
	require.NoError(t, err)
	require.Contains(t, string(data), "After")
	f.assertVideo(t)
}
