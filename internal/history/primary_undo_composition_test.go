package history

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestPrimaryUndoCompositionUnprovenPublicationPrecedesResume(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("published")
	sidecar := filepath.Join(filepath.Dir(j.Source), "unproven.jpg")
	require.NoError(t, os.WriteFile(sidecar, []byte("unproven synthetic sidecar"), 0600))
	ledger, err := models.ParseGeneratedFiles(f.op.GeneratedFiles)
	require.NoError(t, err)
	ledger.PlannedDeletes = append(ledger.PlannedDeletes, models.DeleteEntry{Path: sidecar, RecoveryUnproven: true})
	f.op.GeneratedFiles = models.MarshalLedgerJSON(ledger)
	require.NoError(t, f.repo.Update(context.Background(), f.op))
	f.reload()
	before := f.op.GeneratedFiles
	sourceInfo, err := os.Stat(j.Source)
	require.NoError(t, err)
	result, err := NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	require.Contains(t, result.Error, "manual recovery")
	f.reload()
	requireOnlyUndoAdmissionChanged(t, before, f.op.GeneratedFiles)
	require.Equal(t, models.RevertStatusApplied, f.op.RevertStatus)
	after, err := os.Stat(j.Source)
	require.NoError(t, err)
	require.True(t, os.SameFile(sourceInfo, after))
	for _, path := range []string{j.Source, j.Destination} {
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "synthetic primary video", string(got))
	}
	got, err := os.ReadFile(sidecar)
	require.NoError(t, err)
	require.Equal(t, "unproven synthetic sidecar", string(got))
	claims, err := filepath.Glob(j.Source + ".undo-remove.*")
	require.NoError(t, err)
	require.Empty(t, claims)
	// Once the synthetic unproven output is explicitly absent, both journals
	// coexist through the ordinary pre/post checks and primary missing-anchor skip.
	require.NoError(t, os.Remove(sidecar))
	result, err = NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeReverted, result.Outcome, result.Error)
	f.success()
}

func TestPrimaryUndoCompositionContradictoryResidentOwnershipRefuses(t *testing.T) {
	for _, state := range []string{"copying", "published"} {
		t.Run(state, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			j := f.pause(state)
			ledger, err := models.ParseGeneratedFiles(f.op.GeneratedFiles)
			require.NoError(t, err)
			ledger.PrimaryResident = true
			f.op.GeneratedFiles = models.MarshalLedgerJSON(ledger)
			require.NoError(t, f.repo.Update(context.Background(), f.op))
			f.reload()
			before := f.op.GeneratedFiles
			sourceInfo, err := os.Stat(j.Source)
			require.NoError(t, err)
			result, err := NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
			require.NoError(t, err)
			require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
			require.Contains(t, result.Error, "conflicting resident")
			f.reload()
			requireOnlyUndoAdmissionChanged(t, before, f.op.GeneratedFiles)
			require.Equal(t, models.RevertStatusApplied, f.op.RevertStatus)
			after, err := os.Stat(j.Source)
			require.NoError(t, err)
			require.True(t, os.SameFile(sourceInfo, after))
			got, err := os.ReadFile(j.Source)
			require.NoError(t, err)
			require.Equal(t, "synthetic primary video", string(got))
			if state == "copying" {
				require.FileExists(t, j.Stage)
				require.NoFileExists(t, j.Destination)
			} else {
				require.FileExists(t, j.Destination)
			}
			claims, err := filepath.Glob(j.Source + ".undo-remove.*")
			require.NoError(t, err)
			require.Empty(t, claims)
		})
	}
}

func TestPrimaryUndoCompositionFreshResidentClaimBlocksJournalCAS(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("published")
	stale := *f.op
	ledger, err := models.ParseGeneratedFiles(f.op.GeneratedFiles)
	require.NoError(t, err)
	ledger.PrimaryResident = true
	f.op.GeneratedFiles = models.MarshalLedgerJSON(ledger)
	require.NoError(t, f.repo.Update(context.Background(), f.op))
	before := f.op.GeneratedFiles
	require.ErrorContains(t, primaryUndoState(context.Background(), f.repo, &stale, j, "unlinking"), "conflicting resident")
	f.reload()
	require.Equal(t, before, f.op.GeneratedFiles)
	require.FileExists(t, j.Source)
	require.FileExists(t, j.Destination)
}
