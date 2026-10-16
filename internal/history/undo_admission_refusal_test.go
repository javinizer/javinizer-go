package history

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Every mutating filesystem entry point records an attempt and refuses it.
// Assertions also compare the complete tree, so a refusal cannot hide a write.
type undoAdmissionNoMutationFS struct {
	afero.Fs
	mutations []string
}

func (f *undoAdmissionNoMutationFS) refuse(action, path string) error {
	f.mutations = append(f.mutations, action+":"+path)
	return os.ErrPermission
}
func (f *undoAdmissionNoMutationFS) Create(path string) (afero.File, error) {
	return nil, f.refuse("create", path)
}
func (f *undoAdmissionNoMutationFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		return nil, f.refuse("open-write", path)
	}
	return f.Fs.OpenFile(path, flag, mode)
}
func (f *undoAdmissionNoMutationFS) Mkdir(path string, _ os.FileMode) error {
	return f.refuse("mkdir", path)
}
func (f *undoAdmissionNoMutationFS) MkdirAll(path string, _ os.FileMode) error {
	return f.refuse("mkdir-all", path)
}
func (f *undoAdmissionNoMutationFS) Remove(path string) error {
	return f.refuse("remove", path)
}
func (f *undoAdmissionNoMutationFS) RemoveAll(path string) error {
	return f.refuse("remove-all", path)
}
func (f *undoAdmissionNoMutationFS) Rename(from, to string) error {
	return f.refuse("rename", from+" -> "+to)
}
func (f *undoAdmissionNoMutationFS) Chmod(path string, _ os.FileMode) error {
	return f.refuse("chmod", path)
}
func (f *undoAdmissionNoMutationFS) Chown(path string, _, _ int) error {
	return f.refuse("chown", path)
}
func (f *undoAdmissionNoMutationFS) Chtimes(path string, _, _ time.Time) error {
	return f.refuse("chtimes", path)
}

func undoAdmissionTree(t *testing.T, fs afero.Fs) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	require.NoError(t, afero.Walk(fs, "/", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			tree[path] = "directory"
			return nil
		}
		body, err := afero.ReadFile(fs, path)
		if err == nil {
			tree[path] = info.Mode().String() + ":" + string(body)
		}
		return err
	}))
	return tree
}

// The hook commits a different row after the caller's initial read but before
// the admission transaction. The real in-memory transaction still executes
// the production callback and persists only when that callback requests it.
type undoAdmissionBoundaryRepo struct {
	*p3OpRepo
	beforeTx func()
	calls    int
	persist  []bool
}

func (r *undoAdmissionBoundaryRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	r.calls++
	if r.beforeTx != nil {
		hook := r.beforeTx
		r.beforeTx = nil
		hook()
	}
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		next, persist, err := fn(current)
		r.persist = append(r.persist, persist)
		return next, persist, err
	})
}

func TestUndoAdmissionMalformedLedgerRefusesBeforeMutation(t *testing.T) {
	base := afero.NewMemMapFs()
	createTestFile(t, base, "/library/movie.mp4", "owned primary")
	createTestFile(t, base, "/library/generated.nfo", "generated metadata")
	before := undoAdmissionTree(t, base)
	fs := &undoAdmissionNoMutationFS{Fs: base}
	repo := &undoAdmissionBoundaryRepo{p3OpRepo: newP3OpRepo()}
	op := &models.BatchFileOperation{BatchJobID: "malformed-admission", MovieID: "BAD-001", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, OriginalPath: "/incoming/movie.mp4", NewPath: "/library/movie.mp4", GeneratedFiles: `{"delete":broken`}
	require.NoError(t, repo.Create(t.Context(), op))
	beforeRow := *op
	_, parseErr := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.Error(t, parseErr)

	result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	require.Equal(t, parseErr.Error(), result.Error)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, []bool{false}, repo.persist)
	stored, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, beforeRow, *stored, "malformed admission must not rewrite history")
	require.Empty(t, fs.mutations)
	require.Equal(t, before, undoAdmissionTree(t, base))
}

func TestUndoAdmissionTerminalTransactionStopsReverter(t *testing.T) {
	for _, terminal := range []models.RevertStatusEnum{models.RevertStatusReverted, models.RevertStatusNoOp} {
		t.Run(string(terminal), func(t *testing.T) {
			base := afero.NewMemMapFs()
			createTestFile(t, base, "/library/movie.mp4", "owned primary")
			createTestFile(t, base, "/library/generated.nfo", "generated metadata")
			createTestFile(t, base, "/library/late.nfo", "late metadata")
			before := undoAdmissionTree(t, base)
			fs := &undoAdmissionNoMutationFS{Fs: base}
			inner := newP3OpRepo()
			op := &models.BatchFileOperation{BatchJobID: "terminal-admission", MovieID: "TERM-001", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, OriginalPath: "/incoming/movie.mp4", NewPath: "/library/movie.mp4", GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Delete: []string{"/library/generated.nfo"}})}
			require.NoError(t, inner.Create(t.Context(), op))
			terminalRow := *op
			terminalRow.RevertStatus = terminal
			terminalRow.GeneratedFiles = models.MarshalLedgerJSON(models.GeneratedFilesJSON{Delete: []string{"/library/generated.nfo", "/library/late.nfo"}})
			repo := &undoAdmissionBoundaryRepo{p3OpRepo: inner, beforeTx: func() {
				require.Equal(t, models.RevertStatusApplied, op.RevertStatus, "the first guard saw an applied row")
				require.NoError(t, inner.Update(t.Context(), &terminalRow))
			}}

			result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
			require.ErrorIs(t, err, ErrBatchAlreadyReverted, "the second guard must use the transactional terminal status")
			require.Nil(t, result)
			require.Equal(t, 1, repo.calls)
			require.Equal(t, []bool{false}, repo.persist, "terminal admission must not add UndoStarted")
			require.Equal(t, terminal, op.RevertStatus)
			require.Equal(t, terminalRow.GeneratedFiles, op.GeneratedFiles)
			stored, err := inner.FindByID(t.Context(), op.ID)
			require.NoError(t, err)
			require.Equal(t, terminalRow, *stored)
			require.Empty(t, fs.mutations)
			require.Equal(t, before, undoAdmissionTree(t, base))
		})
	}
}

func TestUndoAdmissionTerminalTransactionStopsSweepRestore(t *testing.T) {
	for _, terminal := range []models.RevertStatusEnum{models.RevertStatusReverted, models.RevertStatusNoOp} {
		t.Run(string(terminal), func(t *testing.T) {
			base := afero.NewMemMapFs()
			inner := newP3OpRepo()
			dest := "/library/poster.jpg"
			backup := dest + ".dlbak." + p3HexA
			createTestFile(t, base, backup, "last recoverable original")
			before := undoAdmissionTree(t, base)
			fs := &undoAdmissionNoMutationFS{Fs: base}
			op := journalRow(t, inner, "terminal-sweep", "TERM-002", dest, backup, 1, models.RevertStatusApplied)
			terminalRow := *op
			terminalRow.RevertStatus = terminal
			repo := &undoAdmissionBoundaryRepo{p3OpRepo: inner, beforeTx: func() {
				require.NoError(t, inner.Update(t.Context(), &terminalRow))
			}}

			require.False(t, NewReplacementSweeper(fs, repo).restoreAndConsume(t.Context(), op, backup, dest, sweepSlash(backup), nil))
			require.Equal(t, 1, repo.calls, "the terminal transition must occur at atomic admission")
			require.Equal(t, []bool{false}, repo.persist)
			stored, err := inner.FindByID(t.Context(), op.ID)
			require.NoError(t, err)
			require.Equal(t, terminalRow, *stored, "refusal preserves the terminal row and recoverable inverse")
			require.Empty(t, fs.mutations)
			require.Equal(t, before, undoAdmissionTree(t, base), "the missing destination must stay absent and the backup stay intact")
		})
	}
}

func TestUndoAdmissionPendingRetryRefusesBeforeCleanup(t *testing.T) {
	for _, kind := range []string{models.RestorePendingKindClean, models.RestorePendingKindRearmRefused, models.RestorePendingKindPrune} {
		t.Run(kind, func(t *testing.T) {
			base := afero.NewMemMapFs()
			dest := "/library/poster.jpg"
			backup := dest + ".dlbak." + p3HexA
			createTestFile(t, base, dest, "retained destination")
			createTestFile(t, base, backup, "retained backup occupant")
			before := undoAdmissionTree(t, base)
			fs := &undoAdmissionNoMutationFS{Fs: base}
			repo := &undoAdmissionBoundaryRepo{p3OpRepo: newP3OpRepo()}
			entry := models.ReplacementEntry{Destination: dest, Backup: backup, DestSeq: 1, Installed: true}
			require.True(t, entry.SetRestorePending(kind))
			op := &models.BatchFileOperation{BatchJobID: "held-pending-retry", MovieID: "HELD-001", RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PublicationPending: true, Replacements: []models.ReplacementEntry{entry}})}
			require.NoError(t, repo.Create(t.Context(), op))
			beforeRow := *op
			sweeper := NewReplacementSweeper(fs, repo)
			sweeper.rememberPendingRemovalKind(sweepSlash(backup), kind)
			ctx := t.Context()
			if kind == models.RestorePendingKindPrune {
				ctx = database.WithPruneMaintenance(ctx)
			}

			require.False(t, sweeper.retryPendingRemovalClaimed(ctx, op.ID, backup, dest, sweepSlash(backup), nil))
			require.Equal(t, 1, repo.calls)
			require.Equal(t, []bool{false}, repo.persist)
			stored, err := repo.FindByID(t.Context(), op.ID)
			require.NoError(t, err)
			require.Equal(t, beforeRow, *stored, "pending publication must retain the exact ledger without admitting Undo")
			remembered, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
			require.True(t, ok)
			require.Equal(t, kind, remembered, "refusal must retain in-process recovery ownership")
			require.Empty(t, fs.mutations)
			require.Equal(t, before, undoAdmissionTree(t, base))
		})
	}
}
