package models

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestPrimaryUndoCompositionContradictoryDoneJournalRetained(t *testing.T) {
	token := strings.Repeat("a", 32)
	j := &PrimaryUndo{Token: token, State: "done", Source: "library", Destination: "original", Stage: "original.undo." + token, CleanupFrom: "library", Cleanup: "library.undo-remove." + token, SourceIdentity: PrimaryUndoIdentity{Strong: true}, StageIdentity: PrimaryUndoIdentity{Strong: true}, SHA256: strings.Repeat("b", 64)}
	op := BatchFileOperation{OriginalPath: j.Destination, NewPath: j.Source, OperationType: OperationTypeMove}
	ledger := GeneratedFilesJSON{PrimaryUndo: j}
	op.GeneratedFiles = MarshalLedgerJSON(ledger)
	require.False(t, PrimaryUndoNeedsRetention(op))
	ledger.PrimaryResident = true
	op.GeneratedFiles = MarshalLedgerJSON(ledger)
	require.True(t, PrimaryUndoNeedsRetention(op), "contradictory ownership must not be pruned as a completed recovery")
}
