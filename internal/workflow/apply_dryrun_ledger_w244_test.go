package workflow

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ---------------------------------------------------------------------------
// applyOrchImpl.beginRevertLog — dry-run previews journal NOTHING (#244)
//
// The revert ledger exists to arm reverts of real filesystem mutations. A
// dry-run apply mutates nothing, so a preview row would sit in
// batch_file_operations with revert_status 'applied' — looking revertable for
// bytes that never moved (and would surface that way in `history list
// --batch`). Previously every preview journaled a row (under "" for the CLI
// bootstrap workflow); with CLI batches now persisting under a real job ID,
// the ledger must skip previews explicitly.
// ---------------------------------------------------------------------------

// dryRunLedgerOrchestrator builds an applyOrchestrator whose revert log is the
// REAL DB-backed ledger from newTestDBRevertLog; every potentially-mutating
// step is disabled so Execute exercises exactly the begin/complete boundary.
func dryRunLedgerOrchestrator(rl RevertLog) *applyOrchImpl {
	return &applyOrchImpl{
		fs:         afero.NewMemMapFs(),
		downloader: &stubDownloader{},
		nfo:        &applyStubNFO{},
		revertLog:  rl,
	}
}

func TestApplyOrchImpl_BeginRevertLog_DryRunJournalsNothing(t *testing.T) {
	rl, db := newTestDBRevertLog(t)
	impl := dryRunLedgerOrchestrator(rl)

	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "DRY-001", Title: "Preview"},
		Match:    models.FileMatchInfo{Path: "/src/DRY-001.mp4", MovieID: "DRY-001"},
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true}, // update-mode shape: no move step
		DryRun:   true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.OperationID, "dry-run apply must not open a revert-ledger operation")

	repo := database.NewBatchFileOperationRepository(db)
	n, err := repo.CountByBatchJobID(context.Background(), "test-job")
	require.NoError(t, err)
	assert.Zero(t, n, "dry-run apply must journal no batch_file_operations rows")
}

func TestApplyOrchImpl_BeginRevertLog_LiveJournalsRow(t *testing.T) {
	// Control leg: the live apply path still journals its operation row.
	rl, db := newTestDBRevertLog(t)
	impl := dryRunLedgerOrchestrator(rl)

	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "LIVE-001", Title: "Live"},
		Match:    models.FileMatchInfo{Path: "/src/LIVE-001.mp4", MovieID: "LIVE-001"},
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.OperationID, "live apply must journal a revert-ledger operation")

	repo := database.NewBatchFileOperationRepository(db)
	n, err := repo.CountByBatchJobID(context.Background(), "test-job")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "live apply journals exactly one row")
}
