package models

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestPrimaryUndoNeedsRetention(t *testing.T) {
	token := strings.Repeat("a", 32)
	j := PrimaryUndo{Token: token, State: "done", Source: "library", Destination: "original", Stage: "original.undo." + token, CleanupFrom: "library", Cleanup: "library.undo-remove." + token, SourceIdentity: PrimaryUndoIdentity{Strong: true}, StageIdentity: PrimaryUndoIdentity{Strong: true}, SHA256: strings.Repeat("b", 64)}
	op := BatchFileOperation{OriginalPath: "original", NewPath: "library", OperationType: OperationTypeMove}
	save := func(j *PrimaryUndo) BatchFileOperation {
		copy := op
		copy.GeneratedFiles = MarshalLedgerJSON(GeneratedFilesJSON{PrimaryUndo: j})
		return copy
	}
	require.False(t, PrimaryUndoNeedsRetention(save(nil)))
	require.False(t, PrimaryUndoNeedsRetention(save(&j)))
	rotated := j
	rotated.Stage += "." + token
	require.False(t, PrimaryUndoNeedsRetention(save(&rotated)))
	bad := op
	bad.GeneratedFiles = `{"primary_undo":`
	require.True(t, PrimaryUndoNeedsRetention(bad))
	for _, state := range []string{"planned", "copying", "sealed", "published", "unlinking", "primary_removed", "unknown", ""} {
		copy := j
		copy.State = state
		require.True(t, PrimaryUndoNeedsRetention(save(&copy)), state)
	}
	for _, change := range []func(*PrimaryUndo){
		func(j *PrimaryUndo) { j.Token = "bad" }, func(j *PrimaryUndo) { j.Token = "" }, func(j *PrimaryUndo) { j.SHA256 = "bad" }, func(j *PrimaryUndo) { j.SHA256 = "" },
		func(j *PrimaryUndo) { j.SourceIdentity.Strong = false }, func(j *PrimaryUndo) { j.StageIdentity.Strong = false }, func(j *PrimaryUndo) { j.Source = "" }, func(j *PrimaryUndo) { j.Destination = "" }, func(j *PrimaryUndo) { j.Source = j.Destination },
		func(j *PrimaryUndo) { j.Stage = "foreign" }, func(j *PrimaryUndo) { j.Stage += ".bad" }, func(j *PrimaryUndo) { j.Stage += "." + strings.Repeat("x", 32) }, func(j *PrimaryUndo) { j.CleanupFrom = "foreign" }, func(j *PrimaryUndo) { j.Cleanup = "foreign" }, func(j *PrimaryUndo) { j.Cleanup += "bad" }, func(j *PrimaryUndo) { j.SupersededStages = []PrimaryUndoStage{{Path: "pending"}} },
	} {
		copy := j
		change(&copy)
		require.True(t, PrimaryUndoNeedsRetention(save(&copy)))
	}
	for _, change := range []func(*BatchFileOperation){func(op *BatchFileOperation) { op.NewPath = "wrong" }, func(op *BatchFileOperation) { op.OriginalPath = "wrong" }, func(op *BatchFileOperation) { op.InPlaceRenamed = true }, func(op *BatchFileOperation) { op.OperationType = OperationTypeCopy }} {
		copy := save(&j)
		change(&copy)
		require.True(t, PrimaryUndoNeedsRetention(copy))
	}
}
