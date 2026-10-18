package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrimaryUndoDirectoryCompositionContradictoryDoneRetained(t *testing.T) {
	token := strings.Repeat("a", 32)
	j := &PrimaryUndo{Token: token, State: "done", Source: "library", Destination: "original", Stage: "original.undo." + token, CleanupFrom: "library", Cleanup: "library.undo-remove." + token, SourceIdentity: PrimaryUndoIdentity{Strong: true}, StageIdentity: PrimaryUndoIdentity{Strong: true}, SHA256: strings.Repeat("b", 64)}
	op := BatchFileOperation{OriginalPath: j.Destination, NewPath: j.Source, OperationType: OperationTypeMove}
	ledger := GeneratedFilesJSON{PrimaryUndo: j}
	op.GeneratedFiles = MarshalLedgerJSON(ledger)
	require.False(t, PrimaryUndoNeedsRetention(op))
	ledger.DirectoryMove = &DirectoryMove{Original: "incoming", Locations: []string{"organized"}}
	op.GeneratedFiles = MarshalLedgerJSON(ledger)
	require.True(t, PrimaryUndoNeedsRetention(op), "DirectoryMove alone contradicts primary ownership even without the resident flag")
	ledger.PrimaryResident = true
	op.GeneratedFiles = MarshalLedgerJSON(ledger)
	require.True(t, PrimaryUndoNeedsRetention(op))
}
