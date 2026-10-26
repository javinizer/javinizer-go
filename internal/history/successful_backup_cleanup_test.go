package history

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func successCleanupFixture(t *testing.T, count int) (afero.Fs, *p3OpRepo, *models.BatchFileOperation, models.GeneratedFilesJSON) {
	t.Helper()
	fs := afero.NewOsFs()
	repo := newP3OpRepo()
	dir := t.TempDir()
	disabled := false
	ledger := models.GeneratedFilesJSON{UndoRetention: &disabled, ApplyTerminal: &models.ApplyTerminal{Outcome: "success", Fingerprint: "synthetic-committed"}}
	for i := 0; i < count; i++ {
		dest := filepath.Join(dir, fmt.Sprintf("TEST-%d.nfo", i))
		backup := dest + ".dlbak." + p3HexA
		body := []byte(fmt.Sprintf("old synthetic metadata %d", i))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("new synthetic metadata"), 0644))
		require.NoError(t, afero.WriteFile(fs, backup, body, 0644))
		info, err := fs.Stat(backup)
		require.NoError(t, err)
		ledger.Replacements = append(ledger.Replacements, models.ReplacementEntry{Destination: dest, Backup: backup, DestSeq: int64(i + 1), Installed: true, BackupSize: info.Size(), BackupModUnix: info.ModTime().Unix(), BackupSHA256: fmt.Sprintf("%x", sha256.Sum256(body))})
	}
	row := &models.BatchFileOperation{BatchJobID: "synthetic-cleanup", MovieID: "TEST-001", OperationType: models.OperationTypeUpdate, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
	require.NoError(t, repo.Create(context.Background(), row))
	return fs, repo, row, ledger
}

func TestSuccessfulBackupCleanupMixedPendingBusyRestart(t *testing.T) {
	ctx := context.Background()
	fs, repo, row, ledger := successCleanupFixture(t, 2)
	first, second := ledger.Replacements[0], ledger.Replacements[1]
	ledger.Replacements[0].SetRestorePending(models.RestorePendingKindPrune)
	row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
	require.NoError(t, repo.Update(ctx, row))
	release, err := fsutil.AcquireReplacementBusy(fs, second.Destination)
	require.NoError(t, err)
	t.Cleanup(release)
	// First restart: pending A can finish, but B's live writer must be respected.
	_, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	_, err = fs.Stat(first.Backup)
	require.True(t, os.IsNotExist(err))
	_, err = fs.Stat(second.Backup)
	require.NoError(t, err)
	current, err := repo.FindByID(ctx, row.ID)
	require.NoError(t, err)
	remaining, err := models.ParseGeneratedFiles(current.GeneratedFiles)
	require.NoError(t, err)
	require.False(t, remaining.UndoStarted, "maintenance retry must not poison later cleanup as user Undo")
	require.Len(t, remaining.Replacements, 1)
	require.Equal(t, second.Backup, remaining.Replacements[0].Backup)
	require.False(t, remaining.Replacements[0].RestorePending)
	release()
	// A new sweeper has no in-memory cleanup state. Durable policy must suffice.
	_, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	_, err = fs.Stat(second.Backup)
	require.True(t, os.IsNotExist(err))
	current, err = repo.FindByID(ctx, row.ID)
	require.NoError(t, err)
	remaining, err = models.ParseGeneratedFiles(current.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, remaining.Replacements)
	require.False(t, remaining.UndoStarted)
	for _, entry := range ledger.Replacements {
		require.Equal(t, "new synthetic metadata", string(mustRead2(t, fs, entry.Destination)))
	}
}

func TestSuccessfulBackupCleanupSharedReferenceReportsPending(t *testing.T) {
	ctx := context.Background()
	fs, repo, row, ledger := successCleanupFixture(t, 1)
	shared := &models.BatchFileOperation{BatchJobID: "older-owner", MovieID: "TEST-002", RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: ledger.Replacements})}
	require.NoError(t, repo.Create(ctx, shared))
	err := NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(ctx, row.ID)
	require.ErrorContains(t, err, "retains backup cleanup intent")
	require.Equal(t, "old synthetic metadata 0", string(mustRead2(t, fs, ledger.Replacements[0].Backup)))
	for _, id := range []uint{row.ID, shared.ID} {
		current, e := repo.FindByID(ctx, id)
		require.NoError(t, e)
		remaining, e := models.ParseGeneratedFiles(current.GeneratedFiles)
		require.NoError(t, e)
		require.Len(t, remaining.Replacements, 1)
		require.False(t, remaining.Replacements[0].RestorePending)
	}
}

func TestSuccessfulBackupCleanupRetainsIneligibleRecovery(t *testing.T) {
	for _, kind := range []string{"legacy", "enabled", "failed-terminal", "failed-status", "incomplete", "publication-pending", "undo-started", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			fs, repo, row, ledger := successCleanupFixture(t, 1)
			entry := ledger.Replacements[0]
			switch kind {
			case "legacy":
				ledger.UndoRetention = nil
			case "enabled":
				enabled := true
				ledger.UndoRetention = &enabled
			case "failed-terminal":
				ledger.ApplyTerminal.Outcome = "failure"
			case "failed-status":
				row.RevertStatus = models.RevertStatusFailed
			case "incomplete":
				ledger.ApplyTerminal = nil
			case "publication-pending":
				ledger.PublicationPending = true
			case "undo-started":
				ledger.UndoStarted = true
			}
			row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
			if kind == "malformed" {
				row.GeneratedFiles = `{"undo_retention":false,"replacements":broken`
			}
			require.NoError(t, repo.Update(ctx, row))
			err := NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(ctx, row.ID)
			if kind == "malformed" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			current, err := repo.FindByID(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, row.GeneratedFiles, current.GeneratedFiles)
			require.Equal(t, "old synthetic metadata 0", string(mustRead2(t, fs, entry.Backup)))
			require.Equal(t, "new synthetic metadata", string(mustRead2(t, fs, entry.Destination)))
		})
	}
}

func TestSuccessfulBackupCleanupForeignSubstitutionRetainsOwnershipEvidence(t *testing.T) {
	ctx := context.Background()
	fs, repo, row, ledger := successCleanupFixture(t, 1)
	entry := ledger.Replacements[0]
	info, err := fs.Stat(entry.Backup)
	require.NoError(t, err)
	foreign := []byte("foreign metadata bytes!!") // same length as original; digest must distinguish it.
	require.Len(t, foreign, int(entry.BackupSize))
	require.NoError(t, afero.WriteFile(fs, entry.Backup, foreign, 0644))
	require.NoError(t, fs.Chtimes(entry.Backup, info.ModTime(), info.ModTime()))
	err = NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(ctx, row.ID)
	require.Error(t, err)
	require.Equal(t, string(foreign), string(mustRead2(t, fs, entry.Backup)))
	require.Equal(t, "new synthetic metadata", string(mustRead2(t, fs, entry.Destination)))
	current, err := repo.FindByID(ctx, row.ID)
	require.NoError(t, err)
	remaining, err := models.ParseGeneratedFiles(current.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, remaining.Replacements, 1)
	require.Equal(t, entry.BackupSHA256, remaining.Replacements[0].BackupSHA256)
	require.True(t, remaining.Replacements[0].RestorePending)
}

func TestSuccessfulBackupCleanupMissingProofRetainedAcrossRestart(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for _, fault := range []string{"missing-digest", "short-digest", "nonhex-digest", "missing-stamp", "negative-size"} {
			t.Run(fmt.Sprintf("pending=%t/%s", pending, fault), func(t *testing.T) {
				ctx := context.Background()
				fs, repo, row, ledger := successCleanupFixture(t, 1)
				entry := &ledger.Replacements[0]
				switch fault {
				case "missing-digest":
					entry.BackupSHA256 = ""
				case "short-digest":
					entry.BackupSHA256 = "0123456789abcdef"
				case "nonhex-digest":
					entry.BackupSHA256 = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
				case "missing-stamp":
					entry.BackupModUnix = 0
				case "negative-size":
					entry.BackupSize = -1
				}
				if pending {
					entry.SetRestorePending(models.RestorePendingKindPrune)
				}
				row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
				require.NoError(t, repo.Update(ctx, row))
				require.False(t, successfulCleanupProof(*entry))
				err := NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(ctx, row.ID)
				require.ErrorContains(t, err, "ownership proof unavailable")
				// A fresh sweep exercises both its automatic-cleanup prepass and the
				// durable pending-removal retry, neither of which may fall back to names.
				_, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
				require.NoError(t, err)
				current, err := repo.FindByID(ctx, row.ID)
				require.NoError(t, err)
				require.Equal(t, row.GeneratedFiles, current.GeneratedFiles)
				require.Equal(t, "old synthetic metadata 0", string(mustRead2(t, fs, entry.Backup)))
				require.Equal(t, "new synthetic metadata", string(mustRead2(t, fs, entry.Destination)))
				if !pending {
					// The final transactional gate also refuses an unproven entry, even if
					// a caller bypasses the initial eligibility/proof snapshot check.
					err = NewReplacementSweeper(fs, repo).markSuccessfulCleanupPending(ctx, row.ID, *entry)
					require.Error(t, err)
					current, err = repo.FindByID(ctx, row.ID)
					require.NoError(t, err)
					require.Equal(t, row.GeneratedFiles, current.GeneratedFiles)
				}
			})
		}
	}
}

func TestSuccessfulBackupCleanupUnexpectedPendingKindRetained(t *testing.T) {
	fs, repo, row, ledger := successCleanupFixture(t, 1)
	ledger.Replacements[0].SetRestorePending(models.RestorePendingKindClean)
	row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
	require.NoError(t, repo.Update(t.Context(), row))
	_, err := NewReplacementSweeper(fs, repo).Sweep(t.Context())
	require.NoError(t, err)
	current, err := repo.FindByID(t.Context(), row.ID)
	require.NoError(t, err)
	require.Equal(t, row.GeneratedFiles, current.GeneratedFiles)
	require.Equal(t, "old synthetic metadata 0", string(mustRead2(t, fs, ledger.Replacements[0].Backup)))
}

func TestSuccessfulBackupCleanupExplicitHistoryPrune(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			fs, repo, row, ledger := successCleanupFixture(t, 1)
			if pending {
				ledger.Replacements[0].SetRestorePending(models.RestorePendingKindPrune)
				row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
				require.NoError(t, repo.Update(t.Context(), row))
			}
			require.NoError(t, NewReplacementSweeper(fs, repo).PruneOperationBackups(t.Context(), []models.BatchFileOperation{*row}))
			_, err := fs.Stat(ledger.Replacements[0].Backup)
			require.True(t, os.IsNotExist(err))
			current, err := repo.FindByID(t.Context(), row.ID)
			require.NoError(t, err)
			final, err := models.ParseGeneratedFiles(current.GeneratedFiles)
			require.NoError(t, err)
			require.Empty(t, final.Replacements)
			require.False(t, final.UndoStarted)
			require.Equal(t, "new synthetic metadata", string(mustRead2(t, fs, ledger.Replacements[0].Destination)))
		})
	}
}
