package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/require"
)

// These tests use two real SQLite pools: publisher writes use publisherRepo,
// while closure, simulated recovery consumption, and assertions use repo. The
// shared fixture creates only synthetic files beneath t.TempDir.
func newApplyAdmissionFixture(t *testing.T) *durableAdmissionFixture {
	t.Helper()
	f := newDurableAdmissionFixture(t, true)
	root := filepath.Dir(f.primary)
	require.NoError(t, f.repo.UpdateJournalInTx(t.Context(), f.row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return ledger, false, err
		}
		ledger.Roots = []string{root}
		ledger.Delete = []string{filepath.Join(root, "owned-extra.jpg")}
		ledger.MoveBack = []models.FileMove{{OriginalPath: filepath.Join(root, "source.srt"), NewPath: filepath.Join(root, "moved.srt")}}
		ledger.PlannedDeletes = append(ledger.PlannedDeletes, models.DeleteEntry{Path: filepath.Join(root, "partial-copy.jpg"), CopySize: 123, CopyPartialSHA256: "partial-proof"})
		ledger.Replacements[0].Installed = false
		ledger.Replacements[0].InstalledSize = 0
		ledger.Replacements[0].InstalledModUnix = 0
		ledger.Replacements[0].InstalledSHA256 = ""
		return ledger, true, nil
	}))
	row := applyAdmissionRead(t, f)
	row.NewPath = filepath.Join(root, "prior-primary.mp4")
	row.NFOPath = filepath.Join(root, "prior.nfo")
	row.NFOSnapshot = "<movie>prior snapshot</movie>"
	row.OriginalDirPath = root
	row.InPlaceRenamed = true
	require.NoError(t, f.repo.UpdateNonJournalFields(t.Context(), row))
	return f
}

func applyAdmissionRead(t *testing.T, f *durableAdmissionFixture) *models.BatchFileOperation {
	t.Helper()
	row, err := f.repo.FindByID(t.Context(), f.row.ID)
	require.NoError(t, err)
	require.NotNil(t, row)
	return row
}

func applyAdmissionLedger(t *testing.T, row *models.BatchFileOperation) models.GeneratedFilesJSON {
	t.Helper()
	ledger, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return ledger
}

func applyAdmissionLateResult(f *durableAdmissionFixture, pending, final bool) *ApplyResult {
	root := filepath.Dir(f.primary)
	return &ApplyResult{
		Movie: &models.Movie{ID: f.row.MovieID}, PublicationPending: pending, PublicationFinalized: final,
		NFOPath: filepath.Join(root, "late.nfo"), FoundNFOPath: filepath.Join(root, "late-found.nfo"),
		DownloadPaths:  []string{filepath.Join(root, "late-download.jpg")},
		OrganizeResult: &organizer.OrganizeResult{OriginalPath: f.primary, NewPath: filepath.Join(root, "late-primary.mp4"), FolderPath: filepath.Join(root, "late-root")},
	}
}

type applyAdmissionWriter struct {
	name string
	run  func(*dbRevertLog) error
}

func applyAdmissionWriters(ctx context.Context, f *durableAdmissionFixture) []applyAdmissionWriter {
	root := filepath.Dir(f.primary)
	return []applyAdmissionWriter{
		{"RecordMoveIntent", func(l *dbRevertLog) error {
			return l.RecordMoveIntent(ctx, f.id, filepath.Join(root, "late-source.srt"), filepath.Join(root, "late-moved.srt"))
		}},
		{"RecordDeleteIntent", func(l *dbRevertLog) error {
			return l.RecordDeleteIntent(ctx, f.id, []models.DeleteEntry{{Path: filepath.Join(root, "late-pinned.jpg"), SHA256: "late-proof"}})
		}},
		{"ReconcileMoveIntents", func(l *dbRevertLog) error { return l.ReconcileMoveIntents(ctx, f.id, nil) }},
		{"ReconcileDeleteIntents", func(l *dbRevertLog) error { return l.ReconcileDeleteIntents(ctx, f.id, nil) }},
		{"FinalizeDeleteIntentCopyDigest", func(l *dbRevertLog) error {
			return l.FinalizeDeleteIntentCopyDigest(ctx, f.id, filepath.Join(root, "partial-copy.jpg"), "completed-copy-proof")
		}},
		{"RecordReplacement", func(l *dbRevertLog) error {
			return l.RecordReplacement(ctx, f.id, filepath.Join(root, "late-poster.jpg"), filepath.Join(root, "late-poster.prior"), models.ReplacementBackupFacts{Size: 42, ModUnix: 123, SHA256: "backup-proof"})
		}},
		{"ReleaseReplacement", func(l *dbRevertLog) error { return l.ReleaseReplacement(ctx, f.id, f.replaced, f.backup) }},
		{"MarkReplacementRestorePending", func(l *dbRevertLog) error { return l.MarkReplacementRestorePending(ctx, f.id, f.replaced, f.backup) }},
		{"MarkReplacementRestorePendingKind", func(l *dbRevertLog) error {
			return l.MarkReplacementRestorePendingKind(ctx, f.id, f.replaced, f.backup, models.RestorePendingKindClean)
		}},
		{"ConfirmReplacement", func(l *dbRevertLog) error { return l.ConfirmReplacement(ctx, f.id, f.replaced, f.backup) }},
		{"ConfirmReplacementInstalled", func(l *dbRevertLog) error {
			return l.ConfirmReplacementInstalled(ctx, f.id, f.replaced, f.backup, models.ReplacementBackupFacts{Size: 73, ModUnix: 456, SHA256: "installed-proof"})
		}},
		{"seedRoot", func(l *dbRevertLog) error { return l.seedRoot(ctx, f.id, filepath.Join(root, "late-seeded-root")) }},
		{"CompleteIntermediate", func(l *dbRevertLog) error { return l.Complete(ctx, f.id, applyAdmissionLateResult(f, false, false)) }},
		{"CompletePending", func(l *dbRevertLog) error { return l.Complete(ctx, f.id, applyAdmissionLateResult(f, true, false)) }},
		{"CompleteFinal", func(l *dbRevertLog) error { return l.Complete(ctx, f.id, applyAdmissionLateResult(f, false, true)) }},
		{"CompleteNil", func(l *dbRevertLog) error { return l.Complete(ctx, f.id, nil) }},
		{"CompleteFailed", func(l *dbRevertLog) error {
			return l.CompleteFailed(ctx, f.id, applyAdmissionLateResult(f, false, false))
		}},
		{"CompleteFailedNil", func(l *dbRevertLog) error { return l.CompleteFailed(ctx, f.id, nil) }},
	}
}

func applyAdmissionClose(t *testing.T, f *durableAdmissionFixture, state string) {
	t.Helper()
	switch state {
	case "UndoStarted":
		require.NoError(t, f.repo.UpdateJournalInTx(t.Context(), f.row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
			ledger.UndoStarted = true
			return ledger, true, err
		}))
	case "Failed", "NoOp", "Reverted":
		row := applyAdmissionRead(t, f)
		row.RevertStatus = map[string]models.RevertStatusEnum{"Failed": models.RevertStatusFailed, "NoOp": models.RevertStatusNoOp, "Reverted": models.RevertStatusReverted}[state]
		require.NoError(t, f.repo.UpdateNonJournalFields(t.Context(), row))
	case "SuccessTerminal":
		require.NoError(t, f.log.Complete(t.Context(), f.id, f.completion(false, true)))
	case "NoOpTerminal":
		require.NoError(t, f.log.Complete(t.Context(), f.id, &ApplyResult{PrePublication: true}))
	case "FailureTerminal":
		require.NoError(t, f.log.CompleteFailed(t.Context(), f.id, f.completion(false, false)))
	default:
		t.Fatalf("unknown closure state %q", state)
	}
}

func TestApplyJournalAdmissionEveryWriterAcceptsActiveRow(t *testing.T) {
	// Verify the rejection matrix's inputs actually attempt valid writes; none
	// may pass merely because its entry is absent or its arguments are invalid.
	for i := range applyAdmissionWriters(t.Context(), &durableAdmissionFixture{}) {
		t.Run(applyAdmissionWriters(t.Context(), &durableAdmissionFixture{})[i].name, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			before := applyAdmissionRead(t, f)
			writer := applyAdmissionWriters(t.Context(), f)[i]
			require.NoError(t, writer.run(f.log.(*dbRevertLog)))
			after := applyAdmissionRead(t, f)
			require.NotEqual(t, before.GeneratedFiles, after.GeneratedFiles, "the active control must change the journal")
			f.assertUntouched(t)
		})
	}
}

func TestApplyJournalAdmissionEveryLateWriterPreservesClosedRow(t *testing.T) {
	for _, state := range []string{"UndoStarted", "Failed", "NoOp", "Reverted", "SuccessTerminal", "NoOpTerminal", "FailureTerminal"} {
		t.Run(state, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			applyAdmissionClose(t, f, state)
			before := applyAdmissionRead(t, f)
			ledger := applyAdmissionLedger(t, before)
			require.NotEmpty(t, ledger.Delete)
			require.NotEmpty(t, ledger.MoveBack)
			require.NotEmpty(t, ledger.PlannedDeletes)
			require.NotEmpty(t, ledger.Replacements)
			require.NotEmpty(t, ledger.Roots)
			for _, writer := range applyAdmissionWriters(t.Context(), f) {
				t.Run(writer.name, func(t *testing.T) {
					require.Error(t, writer.run(f.log.(*dbRevertLog)), "closed rows reject both additive and subtractive writers")
					require.Equal(t, before, applyAdmissionRead(t, f), "all journal bytes, status, metadata, and timestamps must remain exact")
				})
			}

			f.assertUntouched(t)
		})
	}
}

func TestApplyJournalAdmissionPausedWritersObserveTerminalInsideTransaction(t *testing.T) {
	for i := range applyAdmissionWriters(t.Context(), &durableAdmissionFixture{}) {
		t.Run(applyAdmissionWriters(t.Context(), &durableAdmissionFixture{})[i].name, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			barrier := &durableAdmissionBarrierRepo{BatchFileOperationRepositoryInterface: f.publisherRepo, beforeTx: make(chan struct{}), resume: make(chan struct{})}
			old := NewDBRevertLog(barrier, NewRevertLogConfig(true, nil), f.row.BatchJobID, f.fs, nil, nil, nil).(*dbRevertLog)
			writer := applyAdmissionWriters(t.Context(), f)[i]
			done := make(chan error, 1)
			var release sync.Once
			go func() { defer close(done); done <- writer.run(old) }()
			defer func() {
				release.Do(func() { close(barrier.resume) })
				for range done {
				}
			}()
			select {
			case <-barrier.beforeTx:
			case err := <-done:
				t.Fatalf("writer returned before the transaction barrier: %v", err)
			}
			// A separate-process terminal would not share workflow's in-process
			// mutex. Commit its terminal directly through the independent pool.
			require.NoError(t, f.repo.UpdateJournalInTx(t.Context(), f.row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
				ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
				ledger.ApplyTerminal = &models.ApplyTerminal{Outcome: "success", Fingerprint: "other-process-terminal"}
				return ledger, true, err
			}))
			closed := applyAdmissionRead(t, f)
			require.Equal(t, models.RevertStatusApplied, closed.RevertStatus, "status alone cannot exclude a successful terminal")
			release.Do(func() { close(barrier.resume) })
			require.ErrorContains(t, <-done, "closed")
			require.Equal(t, closed, applyAdmissionRead(t, f), "fresh transaction state must defeat the paused old writer")
			f.assertUntouched(t)
		})
	}
}

func TestApplyJournalTerminalRecoveryDefeatsDelayedSuccess(t *testing.T) {
	f := newApplyAdmissionFixture(t)
	owned := applyAdmissionLedger(t, applyAdmissionRead(t, f)).PlannedDeletes[0]
	require.True(t, owned.IdentityStrong, "the carried rollback proof binds the actual temporary artifact")
	checkpoint := f.completion(true, false)
	checkpoint.DownloadPaths = []string{f.artifact}
	require.NoError(t, f.log.Complete(t.Context(), f.id, checkpoint))
	pending := applyAdmissionLedger(t, applyAdmissionRead(t, f))
	require.True(t, pending.PublicationPending)
	require.Nil(t, pending.ApplyTerminal, "checkpoint keeps Apply open")
	require.Contains(t, pending.Delete, f.artifact)

	failed := f.completion(false, false)
	failed.DownloadPaths = []string{f.artifact}
	failed.PublicationRecovery = &models.PublicationRecovery{Managed: []string{f.artifact}, Deletes: []models.DeleteEntry{owned}, Unresolved: true}
	require.NoError(t, f.log.CompleteFailed(t.Context(), f.id, failed))
	closed := applyAdmissionRead(t, f)
	ledger := applyAdmissionLedger(t, closed)
	require.Equal(t, models.RevertStatusFailed, closed.RevertStatus)
	require.False(t, ledger.PublicationPending)
	require.Equal(t, "failure", ledger.ApplyTerminal.Outcome)
	require.NotContains(t, ledger.Delete, f.artifact)
	require.Contains(t, ledger.PlannedDeletes, failed.PublicationRecovery.Deletes[0])

	late := f.completion(false, true)
	late.DownloadPaths = []string{f.artifact, filepath.Join(filepath.Dir(f.primary), "never-owned.jpg")}
	require.Error(t, f.log.Complete(t.Context(), f.id, late))
	require.ErrorContains(t, f.log.RecordMoveIntent(t.Context(), f.id, f.primary, filepath.Join(filepath.Dir(f.primary), "late-primary.mp4")), "closed")
	require.Equal(t, closed, applyAdmissionRead(t, f), "a delayed success cannot strengthen recovery proofs or add claims")
	f.assertUntouched(t)
}

func TestApplyJournalTerminalSuccessRejectsConflictingFailure(t *testing.T) {
	f := newApplyAdmissionFixture(t)
	success := applyAdmissionLateResult(f, false, true)
	require.NoError(t, f.log.Complete(t.Context(), f.id, success))
	closed := applyAdmissionRead(t, f)
	for _, recover := range []bool{false, true} {
		failed := applyAdmissionLateResult(f, false, false)
		if recover {
			failed.PublicationRecovery = &models.PublicationRecovery{Managed: []string{success.DownloadPaths[0]}}
		}
		require.ErrorContains(t, f.log.CompleteFailed(t.Context(), f.id, failed), "closed")
		require.Equal(t, closed, applyAdmissionRead(t, f))
	}
	require.ErrorContains(t, f.log.Complete(t.Context(), f.id, nil), "closed")
	require.ErrorContains(t, f.log.CompleteFailed(t.Context(), f.id, nil), "closed")
	require.Equal(t, closed, applyAdmissionRead(t, f))
}

func TestApplyJournalTerminalExactRetryNeverRemergesConsumedClaims(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			result := applyAdmissionLateResult(f, false, true)
			complete := f.log.Complete
			if failure {
				complete = f.log.CompleteFailed
			}
			require.NoError(t, complete(t.Context(), f.id, result))
			terminal := *applyAdmissionLedger(t, applyAdmissionRead(t, f)).ApplyTerminal
			// Independent recovery consumption is deliberately journal-only.
			// A matching terminal replay must skip its old contribution entirely.
			require.NoError(t, f.repo.UpdateJournalInTx(t.Context(), f.row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
				ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
				ledger.Delete = []string{filepath.Join(filepath.Dir(f.primary), "independent-survivor.jpg")}
				ledger.MoveBack = nil
				ledger.PlannedDeletes = nil
				ledger.Replacements = nil
				ledger.Roots = []string{filepath.Join(filepath.Dir(f.primary), "independent-root")}
				return ledger, true, err
			}))
			consumed := applyAdmissionRead(t, f)
			require.NoError(t, complete(t.Context(), f.id, result))
			replayed := applyAdmissionRead(t, f)
			require.Equal(t, consumed.GeneratedFiles, replayed.GeneratedFiles, "retry must not merge old artifacts, roots, or replacement claims")
			require.Equal(t, terminal, *applyAdmissionLedger(t, replayed).ApplyTerminal)
			require.Equal(t, consumed.RevertStatus, replayed.RevertStatus)
			require.Equal(t, consumed.NewPath, replayed.NewPath)
			require.Equal(t, consumed.NFOPath, replayed.NFOPath)

			changed := applyAdmissionLateResult(f, false, true)
			changed.DownloadPaths = append(changed.DownloadPaths, filepath.Join(filepath.Dir(f.primary), "new-claim.jpg"))
			require.ErrorContains(t, complete(t.Context(), f.id, changed), "closed")
			require.Equal(t, replayed, applyAdmissionRead(t, f), "changed payload is not an idempotent retry")

			applyAdmissionClose(t, f, "UndoStarted")
			admitted := applyAdmissionRead(t, f)
			require.ErrorContains(t, complete(t.Context(), f.id, result), "admitted to Undo", "Undo excludes even exact terminal retries")
			require.Equal(t, admitted, applyAdmissionRead(t, f))
		})
	}
}

func TestApplyJournalTerminalPendingFailureRejectsChangedRecovery(t *testing.T) {
	for _, failure := range []string{"CompleteNil", "CompleteFailedNil", "CompleteFailedPartial"} {
		t.Run(failure, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			checkpoint := f.completion(true, false)
			checkpoint.DownloadPaths = []string{f.artifact}
			require.NoError(t, f.log.Complete(t.Context(), f.id, checkpoint))
			var err error
			switch failure {
			case "CompleteNil":
				err = f.log.Complete(t.Context(), f.id, nil)
			case "CompleteFailedNil":
				err = f.log.CompleteFailed(t.Context(), f.id, nil)
			case "CompleteFailedPartial":
				err = f.log.CompleteFailed(t.Context(), f.id, checkpoint)
			}
			require.NoError(t, err)
			held := applyAdmissionRead(t, f)
			pending := applyAdmissionLedger(t, held)
			require.True(t, pending.PublicationPending, "failure without rollback proof cannot lift the publication hold")
			require.Equal(t, "failure", pending.ApplyTerminal.Outcome)
			require.Equal(t, models.RevertStatusFailed, held.RevertStatus)
			require.Error(t, f.log.Complete(t.Context(), f.id, f.completion(false, true)))
			require.Error(t, f.log.RecordDeleteIntent(t.Context(), f.id, []models.DeleteEntry{{Path: f.artifact, SHA256: "late"}}))
			require.Equal(t, held, applyAdmissionRead(t, f))

			// Once failure is durably terminal, a different proof snapshot is a
			// new claim. Keep the existing hold for manual recovery instead.
			for _, extraClaims := range []bool{false, true} {
				recovery := f.completion(false, false)
				if extraClaims {
					recovery = applyAdmissionLateResult(f, false, false)
				}
				recovery.DownloadPaths = append(recovery.DownloadPaths, f.artifact)
				recovery.PublicationRecovery = &models.PublicationRecovery{Managed: []string{f.artifact}, Deletes: []models.DeleteEntry{{Path: f.artifact, RecoveryPublication: true, RecoveryUnproven: true}}, Unresolved: true}
				require.ErrorContains(t, f.log.CompleteFailed(t.Context(), f.id, recovery), "closed")
				require.Equal(t, held, applyAdmissionRead(t, f), "different recovery cannot clear the hold or add paths, metadata, or proofs")
			}
			// The original terminal request itself remains an exact no-write replay.
			switch failure {
			case "CompleteNil":
				err = f.log.Complete(t.Context(), f.id, nil)
			case "CompleteFailedNil":
				err = f.log.CompleteFailed(t.Context(), f.id, nil)
			case "CompleteFailedPartial":
				err = f.log.CompleteFailed(t.Context(), f.id, checkpoint)
			}
			require.NoError(t, err)
			require.Equal(t, held.GeneratedFiles, applyAdmissionRead(t, f).GeneratedFiles)
		})
	}
}

// failCompletionSQL installs a real SQLite failure on the completion UPDATE.
// Both the journal and narrow columns must roll back; no mock transaction is used.
func failCompletionSQL(t *testing.T, repo *database.BatchFileOperationRepository) func() {
	t.Helper()
	require.NoError(t, repo.GetDB().Exec(`CREATE TRIGGER test_completion_sql_failure BEFORE UPDATE OF new_path ON batch_file_operations BEGIN SELECT RAISE(ABORT, 'injected atomic completion SQL failure'); END`).Error)
	var once sync.Once
	release := func() {
		once.Do(func() {
			require.NoError(t, repo.GetDB().Exec(`DROP TRIGGER test_completion_sql_failure`).Error)
		})
	}
	t.Cleanup(release)
	return release
}

func TestApplyJournalTerminalExactRetryAfterAtomicSQLFailure(t *testing.T) {
	for _, kind := range []string{"Success", "Failure", "CompleteNil", "CompleteFailedNil", "NoOpSuccess", "NoOpFailure"} {
		t.Run(kind, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			releaseFailure := failCompletionSQL(t, f.repo)
			log := NewDBRevertLog(f.publisherRepo, NewRevertLogConfig(true, nil), f.row.BatchJobID, f.fs, nil, nil, nil)
			result := applyAdmissionLateResult(f, false, true)
			complete := log.Complete
			wantStatus := models.RevertStatusApplied
			wantPath, wantNFO := result.OrganizeResult.NewPath, result.FoundNFOPath
			switch kind {
			case "Failure":
				complete = log.CompleteFailed
				wantStatus = models.RevertStatusFailed
			case "CompleteNil":
				result = nil
				wantStatus = models.RevertStatusFailed
				wantPath = ""
				wantNFO = applyAdmissionRead(t, f).NFOPath
			case "CompleteFailedNil":
				complete = log.CompleteFailed
				result = nil
				wantStatus = models.RevertStatusFailed
				wantPath = ""
				wantNFO = applyAdmissionRead(t, f).NFOPath
			case "NoOpSuccess", "NoOpFailure":
				result.PrePublication = true
				wantStatus = models.RevertStatusNoOp
				wantPath = ""
				wantNFO = applyAdmissionRead(t, f).NFOPath
				if kind == "NoOpFailure" {
					complete = log.CompleteFailed
				}
			}
			before := applyAdmissionRead(t, f)
			require.ErrorContains(t, complete(t.Context(), f.id, result), "injected atomic completion SQL failure")
			require.Equal(t, before, applyAdmissionRead(t, f), "SQL failure rolls back the terminal journal and every completion column")
			releaseFailure()
			require.NoError(t, complete(t.Context(), f.id, result))
			repaired := applyAdmissionRead(t, f)
			require.NotNil(t, applyAdmissionLedger(t, repaired).ApplyTerminal)
			require.Equal(t, wantStatus, repaired.RevertStatus)
			require.Equal(t, wantPath, repaired.NewPath)
			require.Equal(t, wantNFO, repaired.NFOPath)
			require.Equal(t, before.NFOSnapshot, repaired.NFOSnapshot)
			require.NoError(t, complete(t.Context(), f.id, result), "identical terminal remains retryable after status changes")
			require.Equal(t, repaired, applyAdmissionRead(t, f), "exact replay must not rewrite metadata or timestamps")
		})
	}
}

func TestApplyJournalTerminalLegacyClosedRowsRejectCarriedRecovery(t *testing.T) {
	for _, status := range []string{"Failed", "NoOp"} {
		t.Run(status, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			applyAdmissionClose(t, f, status)
			legacy := applyAdmissionRead(t, f)
			require.Nil(t, applyAdmissionLedger(t, legacy).ApplyTerminal)
			recovery := applyAdmissionLateResult(f, false, false)
			recovery.PublicationRecovery = &models.PublicationRecovery{Managed: []string{f.artifact}, Deletes: []models.DeleteEntry{{Path: f.artifact, SHA256: "new-proof", RecoveryPublication: true}}}
			require.Error(t, f.log.CompleteFailed(t.Context(), f.id, recovery))
			require.Error(t, f.log.Complete(t.Context(), f.id, applyAdmissionLateResult(f, false, true)))
			require.Equal(t, legacy, applyAdmissionRead(t, f))
		})
	}
}

func TestApplyJournalTerminalMatchingSuccessRefusesExternallyFailedStatus(t *testing.T) {
	for _, status := range []string{"Failed", "NoOp", "Reverted"} {
		t.Run(status, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			success := applyAdmissionLateResult(f, false, true)
			require.NoError(t, f.log.Complete(t.Context(), f.id, success))
			applyAdmissionClose(t, f, status)
			closed := applyAdmissionRead(t, f)
			require.Error(t, f.log.Complete(t.Context(), f.id, success), "even an exact successful terminal cannot revive externally closed status")
			require.Equal(t, closed, applyAdmissionRead(t, f))
		})
	}
}

type applyAdmissionJournalFailureRepo struct {
	database.BatchFileOperationRepositoryInterface
	fail error
}

func (r *applyAdmissionJournalFailureRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		next, persist, err := fn(current)
		if err == nil && r.fail != nil {
			return next, persist, r.fail
		}
		return next, persist, err
	})
}

func TestApplyJournalTerminalFailedTransactionAllowsOriginalRecoveryRetry(t *testing.T) {
	f := newApplyAdmissionFixture(t)
	owned := applyAdmissionLedger(t, applyAdmissionRead(t, f)).PlannedDeletes[0]
	require.True(t, owned.IdentityStrong)
	checkpoint := f.completion(true, false)
	checkpoint.DownloadPaths = []string{f.artifact}
	require.NoError(t, f.log.Complete(t.Context(), f.id, checkpoint))
	before := applyAdmissionRead(t, f)
	injected := errors.New("injected terminal journal transaction failure")
	repo := &applyAdmissionJournalFailureRepo{BatchFileOperationRepositoryInterface: f.publisherRepo, fail: injected}
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), f.row.BatchJobID, f.fs, nil, nil, nil)
	recovery := f.completion(false, false)
	recovery.DownloadPaths = []string{f.artifact}
	recovery.PublicationRecovery = &models.PublicationRecovery{Managed: []string{f.artifact}, Deletes: []models.DeleteEntry{owned}, Unresolved: true}
	require.ErrorIs(t, log.CompleteFailed(t.Context(), f.id, recovery), injected)
	require.Equal(t, before, applyAdmissionRead(t, f), "failed terminal transaction leaves no marker or metadata")
	repo.fail = nil
	require.NoError(t, log.CompleteFailed(t.Context(), f.id, recovery))
	closed := applyAdmissionRead(t, f)
	ledger := applyAdmissionLedger(t, closed)
	require.Equal(t, models.RevertStatusFailed, closed.RevertStatus)
	require.Equal(t, "failure", ledger.ApplyTerminal.Outcome)
	require.False(t, ledger.PublicationPending)
	require.NotContains(t, ledger.Delete, f.artifact)
	require.Contains(t, ledger.PlannedDeletes, recovery.PublicationRecovery.Deletes[0])
	require.NoError(t, log.CompleteFailed(t.Context(), f.id, recovery))
	require.Equal(t, closed.GeneratedFiles, applyAdmissionRead(t, f).GeneratedFiles)
}

// A terminal fingerprint identifies the full completion request, including
// metadata that cannot be inferred from Delete/MoveBack alone.
func TestApplyJournalTerminalRejectsChangedCompletionMetadata(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			result := applyAdmissionLateResult(f, false, true)
			complete := f.log.Complete
			if failure {
				complete = f.log.CompleteFailed
			}
			require.NoError(t, complete(t.Context(), f.id, result))
			closed := applyAdmissionRead(t, f)
			changes := []struct {
				name   string
				mutate func(*ApplyResult)
			}{
				{"primary", func(r *ApplyResult) { r.OrganizeResult.NewPath += ".different" }},
				{"nfo", func(r *ApplyResult) { r.NFOPath += ".different" }},
				{"found_nfo", func(r *ApplyResult) { r.FoundNFOPath += ".different" }},
				{"rename_state", func(r *ApplyResult) { r.OrganizeResult.InPlaceRenamed = true }},
				{"noop", func(r *ApplyResult) { r.PrePublication = true }},
			}
			if !failure {
				changes = append(changes, struct {
					name   string
					mutate func(*ApplyResult)
				}{"discovery_root", func(r *ApplyResult) { r.OrganizeResult.FolderPath += "-different" }})
			}
			for _, change := range changes {
				t.Run(change.name, func(t *testing.T) {
					changed := applyAdmissionLateResult(f, false, true)
					change.mutate(changed)
					require.Error(t, complete(t.Context(), f.id, changed))
					require.Equal(t, closed, applyAdmissionRead(t, f))
				})
			}
		})
	}
}

func TestApplyJournalAdmissionCallbackErrorPreservesRow(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		name := "ordinary"
		if terminal {
			name = "terminal"
		}
		t.Run(name, func(t *testing.T) {
			f := newApplyAdmissionFixture(t)
			before := applyAdmissionRead(t, f)
			injected := errors.New("injected Apply journal callback failure")
			request := applyJournalRequest{}
			if terminal {
				request = terminalJournalRequest("failure", "callback-error-control", nil, nil)
			}
			called := false
			_, err := f.log.(*dbRevertLog).updateApplyJournal(t.Context(), f.row.ID, request, func(current *models.BatchFileOperation, ledger models.GeneratedFilesJSON) (models.GeneratedFilesJSON, bool, error) {
				called = true
				require.Equal(t, before.ID, current.ID)
				require.Equal(t, before.GeneratedFiles, current.GeneratedFiles)
				require.Equal(t, before.RevertStatus, current.RevertStatus)
				ledger.Delete = append(ledger.Delete, filepath.Join(filepath.Dir(f.primary), "rejected-callback-claim.jpg"))
				ledger.Replacements = nil
				return ledger, true, injected
			})
			require.True(t, called, "failure must come from the admitted callback inside the real transaction")
			require.ErrorIs(t, err, injected)
			require.Equal(t, before, applyAdmissionRead(t, f), "callback error discards its candidate ledger and any terminal marker, preserving every column")
			f.assertUntouched(t)
		})
	}
}
