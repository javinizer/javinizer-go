package history

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func requirePruneAdmissionFilesUnchanged(t *testing.T, fs afero.Fs, dest, backup string) {
	t.Helper()
	require.Equal(t, "organized", string(mustRead2(t, fs, dest)))
	require.Equal(t, "old backup", string(mustRead2(t, fs, backup)))
	entries, err := afero.ReadDir(fs, filepath.Dir(dest))
	require.NoError(t, err)
	require.Len(t, entries, 2, "admission failure must not quarantine bytes or strand a busy marker")
	release, err := fsutil.AcquireReplacementBusy(fs, dest)
	require.NoError(t, err, "failed admission must release its destination claim")
	require.NotNil(t, release)
	release()
}

func TestPruneOperationBackupsRejectsFreshMalformedCandidateLedger(t *testing.T) {
	fs := afero.NewOsFs()
	repo := newP3OpRepo()
	dest := filepath.Join(t.TempDir(), "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, afero.WriteFile(fs, dest, []byte("organized"), 0644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old backup"), 0644))
	op := installedPruneCandidate(t, repo, "prune-malformed-admission", "PRUNE-MALFORMED", dest, backup)
	candidate := *op
	// History pruning captured a valid candidate, but the stored row is now
	// corrupt. Admission must reload it rather than trust the captured ledger.
	op.GeneratedFiles = `{"replacements":broken`
	require.NoError(t, repo.Update(t.Context(), op))
	before, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)

	err = NewReplacementSweeper(fs, repo).PruneOperationBackups(t.Context(), []models.BatchFileOperation{candidate})
	var parseErr *json.SyntaxError
	require.ErrorAs(t, err, &parseErr)
	require.ErrorContains(t, err, "operation 1 backup")
	requirePruneAdmissionFilesUnchanged(t, fs, dest, backup)
	after, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "failed fresh-ledger admission must not mutate journal or status")
}

func TestPruneOperationBackupsAdmissionTransactionFailureCanRetry(t *testing.T) {
	fs := afero.NewOsFs()
	base := newP3OpRepo()
	dest := filepath.Join(t.TempDir(), "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, afero.WriteFile(fs, dest, []byte("organized"), 0644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old backup"), 0644))
	op := installedPruneCandidate(t, base, "prune-admission-tx", "PRUNE-TX", dest, backup)
	before, err := base.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	admissionErr := errors.New("prune Undo admission transaction unavailable")
	repo := &w18TxFailRepo{p3OpRepo: base, fail: map[int]error{1: admissionErr}}
	sweeper := NewReplacementSweeper(fs, repo)

	err = sweeper.PruneOperationBackups(t.Context(), []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, admissionErr)
	require.Equal(t, 1, repo.calls, "admission failure must stop before the pending-cleanup transaction")
	requirePruneAdmissionFilesUnchanged(t, fs, dest, backup)
	after, err := base.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "failed admission must not start Undo or mark cleanup pending")

	// The one-shot failure is gone. A new sweeper must recover using the same
	// durable candidate, without a stale in-process lock or cleanup marker.
	require.NoError(t, NewReplacementSweeper(fs, repo).PruneOperationBackups(t.Context(), []models.BatchFileOperation{*op}))
	require.NoFileExists(t, backup)
	require.Equal(t, "organized", string(mustRead2(t, fs, dest)))
	require.Empty(t, requireLedgerReplacements(t, base, op.ID))
	after, err = base.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, before.RevertStatus, after.RevertStatus, "pruning backups does not rewrite operation status")
	release, err := fsutil.AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	require.NotNil(t, release)
	release()
}
