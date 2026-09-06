package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// Issue #245: a dry-run apply must leave the revert journal AND the
// batch_file_operations table completely empty — no 'applied' phantom rows
// for planned-but-unexecuted organizes — while a later real apply on the same
// ledger keeps journaling exactly as before.
func TestApply_DryRunLeavesRevertLedgerEmpty_W245(t *testing.T) {
	ctx := context.Background()

	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "revert-w245.db"), LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repo := database.NewBatchFileOperationRepository(db)

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0o644))

	const jobID = "job-dryrun-w245"
	rl := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), jobID, fs, nil, nil, nil)
	org := organizer.NewOrganizer(fs, &organizer.Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	orch := &applyOrchImpl{fs: fs, organizer: org, revertLog: rl}

	cmdFor := func(movieID, src, name string, dryRun bool) ApplyCmd {
		return ApplyCmd{
			Movie:    &models.Movie{ID: movieID, Title: "W245 Movie"},
			Match:    models.FileMatchInfo{MovieID: movieID, Path: src, Name: name, Extension: ".mkv"},
			DestPath: "/dest",
			DryRun:   dryRun,
			Organize: OrganizeOptions{MoveFiles: true},
		}
	}

	// Dry-run: the plan computes (the fixture's whole point) but NOTHING may
	// reach the ledger — Begin is skipped before repo.Create can journal a
	// phantom 'applied' row, and the empty OperationID suppresses Complete.
	dryRes, err := orch.Execute(ctx, cmdFor("DRY-001", "/in/A.mkv", "A.mkv", true))
	require.NoError(t, err)
	require.NotNil(t, dryRes)
	require.NotNil(t, dryRes.OrganizeResult, "dry-run still surfaces the plan")
	assert.Empty(t, dryRes.OperationID, "dry-run carries no ledger correlation")

	srcExists, err := afero.Exists(fs, "/in/A.mkv")
	require.NoError(t, err)
	assert.True(t, srcExists, "dry-run moved no bytes")
	destExists, err := afero.Exists(fs, "/dest/DRY-001/DRY-001.mkv")
	require.NoError(t, err)
	assert.False(t, destExists)

	dryCount, err := repo.CountByBatchJobID(ctx, jobID)
	require.NoError(t, err)
	assert.Zero(t, dryCount, "batch_file_operations stays empty after a dry-run sort")
	ledgerRows, err := repo.FindOperationsWithLedger(ctx)
	require.NoError(t, err)
	assert.Empty(t, ledgerRows, "no journaled ledger content survives a dry-run")

	// Later real op on the SAME ledger instance: rows journal normally.
	liveRes, err := orch.Execute(ctx, cmdFor("DRY-001", "/in/A.mkv", "A.mkv", false))
	require.NoError(t, err)
	require.NotNil(t, liveRes)
	assert.NotEmpty(t, liveRes.OperationID, "real apply still correlates with its revert row")

	rows, err := repo.FindByBatchJobID(ctx, jobID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "exactly the real apply's row exists")
	assert.Equal(t, models.RevertStatusApplied, rows[0].RevertStatus)
	assert.Equal(t, "/dest/DRY-001/DRY-001.mkv", filepath.ToSlash(rows[0].NewPath))
	movedBytes, err := afero.ReadFile(fs, "/dest/DRY-001/DRY-001.mkv")
	require.NoError(t, err)
	assert.Equal(t, []byte("a-bytes"), movedBytes, "the subsequent real move is unaffected")
}
