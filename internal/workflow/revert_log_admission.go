package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// All Apply journal writers share this transaction-level policy. A read outside
// the transaction cannot authorize either an additive or subtractive mutation.
type applyJournalRequest struct {
	terminal *models.ApplyTerminal
}

type applyJournalState struct {
	raw    string
	status models.RevertStatusEnum
}

func terminalJournalRequest(outcome, raw string, metadata []string, recovery *models.PublicationRecovery) applyJournalRequest {
	// This payload contains only strings, integers, booleans and their slices;
	// marshaling cannot fail. Include carried recovery proofs explicitly.
	payload, _ := json.Marshal(struct {
		Outcome  string
		Journal  string
		Metadata []string
		Recovery *models.PublicationRecovery
	}{outcome, raw, metadata, recovery})
	sum := sha256.Sum256(payload)
	return applyJournalRequest{terminal: &models.ApplyTerminal{Outcome: outcome, Fingerprint: fmt.Sprintf("%x", sum)}}
}

func admitApplyJournal(current *models.BatchFileOperation, ledger models.GeneratedFilesJSON, request applyJournalRequest) (bool, error) {
	if ledger.UndoStarted || current.RevertStatus == models.RevertStatusReverted {
		return false, fmt.Errorf("publication completion refused: operation %d was admitted to Undo or is terminal", current.ID)
	}
	if request.terminal != nil && request.terminal.Outcome == "success" && current.RevertStatus != models.RevertStatusApplied {
		return false, fmt.Errorf("successful Apply completion requires active operation %d", current.ID)
	}
	if terminal := ledger.ApplyTerminal; terminal != nil {
		if request.terminal != nil && *terminal == *request.terminal {
			return true, nil
		}
		return false, fmt.Errorf("Apply journal is closed by %s terminal for operation %d", terminal.Outcome, current.ID)
	}
	if current.RevertStatus != models.RevertStatusApplied {
		return false, fmt.Errorf("Apply journal is not active for operation %d (%s)", current.ID, current.RevertStatus)
	}
	return false, nil
}

type applyJournalUpdateFn func(*models.BatchFileOperation, models.GeneratedFilesJSON) (models.GeneratedFilesJSON, bool, error)

func (l *dbRevertLog) updateApplyJournal(ctx context.Context, id uint, request applyJournalRequest, fn applyJournalUpdateFn) (applyJournalState, error) {
	var state applyJournalState
	err := l.repo.UpdateJournalInTx(ctx, id, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		replay, err := admitApplyJournal(current, ledger, request)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		state = applyJournalState{raw: current.GeneratedFiles, status: current.RevertStatus}
		if replay {
			return ledger, false, nil
		}
		next, persist, err := fn(current, ledger)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		if !persist {
			next = ledger
		}
		if request.terminal != nil {
			next.ApplyTerminal = request.terminal
			persist = true
		}
		if persist {
			state.raw = models.MarshalLedgerJSON(next)
		}
		return next, persist, nil
	})
	return state, err
}

func guardApplyColumns(expected applyJournalState) func(*models.BatchFileOperation) error {
	return func(current *models.BatchFileOperation) error {
		if current.GeneratedFiles != expected.raw || current.RevertStatus != expected.status {
			return fmt.Errorf("Apply columns refused: operation %d changed after its authoritative snapshot", current.ID)
		}
		// expected came from successful journal admission or the explicitly open
		// snapshot admission. Equality under this transaction preserves that grant.
		return nil
	}
}
