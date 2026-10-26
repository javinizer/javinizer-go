package history

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type successfulCleanupReadRepo struct {
	*p3OpRepo
	find func(context.Context, uint) (*models.BatchFileOperation, error)
}

func (r *successfulCleanupReadRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	return r.find(ctx, id)
}

type successfulCleanupAdmissionRepo struct {
	*p3OpRepo
	beforeTx func()
}

func (r *successfulCleanupAdmissionRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if r.beforeTx != nil {
		before := r.beforeTx
		r.beforeTx = nil
		before()
	}
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

func requireSuccessfulCleanupFiles(t *testing.T, fs afero.Fs, entry models.ReplacementEntry) {
	t.Helper()
	require.Equal(t, "old synthetic metadata 0", string(mustRead2(t, fs, entry.Backup)))
	require.Equal(t, "new synthetic metadata", string(mustRead2(t, fs, entry.Destination)))
}

func TestSuccessfulCleanupSafetyRepositoryUnavailable(t *testing.T) {
	for name, sweeper := range map[string]*ReplacementSweeper{
		"nil sweeper":    nil,
		"nil repository": NewReplacementSweeper(afero.NewMemMapFs(), nil),
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, sweeper.CleanupSuccessfulOperation(t.Context(), 1), "repository unavailable")
		})
	}
}

func TestSuccessfulCleanupSafetyReadFailureRetainsPendingIntent(t *testing.T) {
	for _, readAt := range []int{1, 2} {
		for _, fault := range []string{"error", "missing", "malformed"} {
			name := fault + "/initial"
			if readAt == 2 {
				name = fault + "/final"
			}
			t.Run(name, func(t *testing.T) {
				fs, base, row, ledger := successCleanupFixture(t, 1)
				// An existing two-phase intent must be left to ordinary recovery,
				// not re-quarantined while automatic cleanup verifies its remainder.
				ledger.Replacements[0].SetRestorePending(models.RestorePendingKindPrune)
				row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
				require.NoError(t, base.Update(t.Context(), row))
				readErr := errors.New("cleanup journal read failed")
				reads := 0
				repo := &successfulCleanupReadRepo{p3OpRepo: base}
				repo.find = func(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
					reads++
					if reads == readAt {
						switch fault {
						case "error":
							return nil, readErr
						case "missing":
							return nil, nil
						case "malformed":
							return &models.BatchFileOperation{ID: id, GeneratedFiles: `{"replacements":broken`}, nil
						}
					}
					return base.FindByID(ctx, id)
				}
				err := NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(t.Context(), row.ID)
				require.Error(t, err)
				if fault == "error" {
					require.ErrorIs(t, err, readErr)
				} else if fault == "missing" && readAt == 1 {
					require.ErrorContains(t, err, "missing")
				} else if fault == "missing" {
					require.ErrorContains(t, err, "disappeared during cleanup")
				} else {
					var syntaxErr *json.SyntaxError
					require.ErrorAs(t, err, &syntaxErr)
				}
				require.Equal(t, readAt, reads)
				current, err := base.FindByID(t.Context(), row.ID)
				require.NoError(t, err)
				require.Equal(t, *row, *current, "read failures must not discard the audit row or durable intent")
				requireSuccessfulCleanupFiles(t, fs, ledger.Replacements[0])
			})
		}
	}
}

func TestSuccessfulCleanupSafetyAdmissionRechecksCurrentJournal(t *testing.T) {
	for _, change := range []string{"malformed", "undo-enabled", "undo-started", "reverted", "ownership-sequence", "backup", "destination"} {
		t.Run(change, func(t *testing.T) {
			fs, base, row, ledger := successCleanupFixture(t, 1)
			original := ledger.Replacements[0]
			repo := &successfulCleanupAdmissionRepo{p3OpRepo: base}
			admitted := false
			wantError := "not eligible for successful backup cleanup"
			repo.beforeTx = func() {
				admitted = true
				// This serialized competing write happens after Cleanup's initial
				// snapshot but before its transactional ownership/eligibility gate.
				switch change {
				case "undo-enabled":
					enabled := true
					ledger.UndoRetention = &enabled
				case "undo-started":
					ledger.UndoStarted = true
				case "reverted":
					row.RevertStatus = models.RevertStatusReverted
				case "ownership-sequence":
					ledger.Replacements[0].DestSeq++
					wantError = "replacement changed before cleanup"
				case "backup":
					ledger.Replacements[0].Backup += ".quarantine"
					wantError = "no longer owns backup"
				case "destination":
					ledger.Replacements[0].Destination += ".other"
					wantError = "no longer owns backup"
				}
				row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
				if change == "malformed" {
					row.GeneratedFiles = `{"replacements":broken`
				}
				require.NoError(t, base.Update(t.Context(), row))
			}
			err := NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(t.Context(), row.ID)
			require.True(t, admitted, "test must reach the transaction after the initial eligible snapshot")
			require.Error(t, err)
			if change != "malformed" {
				require.ErrorContains(t, err, wantError)
			} else {
				var syntaxErr *json.SyntaxError
				require.ErrorAs(t, err, &syntaxErr)
			}
			current, err := base.FindByID(t.Context(), row.ID)
			require.NoError(t, err)
			require.Equal(t, *row, *current, "cleanup must not overwrite the competing journal/status")
			requireSuccessfulCleanupFiles(t, fs, original)
		})
	}
}

func TestSuccessfulCleanupSafetyPendingAdmissionRequiresExactMaintenanceIntent(t *testing.T) {
	for _, kind := range []string{models.RestorePendingKindClean, models.RestorePendingKindRearmRefused, models.RestorePendingKindPrune} {
		t.Run(kind, func(t *testing.T) {
			fs, repo, row, ledger := successCleanupFixture(t, 1)
			ledger.Replacements[0].SetRestorePending(kind)
			row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
			require.NoError(t, repo.Update(t.Context(), row))
			// Exercise admission itself: automatic cleanup intentionally skips
			// pending entries, but maintenance may retry an exact durable intent.
			err := NewReplacementSweeper(fs, repo).markSuccessfulCleanupPending(t.Context(), row.ID, ledger.Replacements[0])
			if kind == models.RestorePendingKindPrune {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "conflicting cleanup intent")
			}
			current, err := repo.FindByID(t.Context(), row.ID)
			require.NoError(t, err)
			require.Equal(t, *row, *current, "admission must preserve the exact durable intent")
			requireSuccessfulCleanupFiles(t, fs, ledger.Replacements[0])
		})
	}
}

func TestSuccessfulCleanupSafetyScanCancellationKeepsEarlierFailure(t *testing.T) {
	fs, base, first, firstLedger := successCleanupFixture(t, 1)
	_, _, second, secondLedger := successCleanupFixture(t, 1)
	require.NoError(t, base.Create(t.Context(), second))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	readErr := errors.New("first cleanup journal unavailable")
	reads := 0
	repo := &successfulCleanupReadRepo{p3OpRepo: base}
	repo.find = func(_ context.Context, id uint) (*models.BatchFileOperation, error) {
		reads++
		require.Equal(t, first.ID, id, "cancellation must stop before starting the next operation")
		cancel()
		return nil, readErr
	}
	err := NewReplacementSweeper(fs, repo).cleanupSuccessfulOperations(ctx)
	require.ErrorIs(t, err, readErr)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, reads)
	for _, row := range []*models.BatchFileOperation{first, second} {
		current, err := base.FindByID(t.Context(), row.ID)
		require.NoError(t, err)
		require.Equal(t, *row, *current)
	}
	requireSuccessfulCleanupFiles(t, fs, firstLedger.Replacements[0])
	requireSuccessfulCleanupFiles(t, fs, secondLedger.Replacements[0])
}
