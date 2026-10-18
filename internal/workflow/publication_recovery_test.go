package workflow

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPublicationRecoveryReconcilesOnlyManagedClaims(t *testing.T) {
	ledger := models.GeneratedFilesJSON{Delete: []string{"/managed", "/other"}, PlannedDeletes: []models.DeleteEntry{{Path: "/managed"}, {Path: "/other"}}, MoveBack: []models.FileMove{{NewPath: "/managed"}, {NewPath: "/other"}}}
	proof := models.DeleteEntry{Path: "/owned", IdentityPinned: true}
	next := reconcilePublicationRecovery(ledger, &models.PublicationRecovery{Managed: []string{"/managed"}, Deletes: []models.DeleteEntry{proof}, Moves: []models.FileMove{{NewPath: "/moved", Proof: &proof}}})
	require.Equal(t, []string{"/other"}, next.Delete)
	require.Len(t, next.PlannedDeletes, 2)
	require.Equal(t, "/other", next.PlannedDeletes[0].Path)
	require.Len(t, next.MoveBack, 2)
	require.Equal(t, "/other", next.MoveBack[0].NewPath)
	_, _, _, err := completionLedgerMergeOpt("invalid", "invalid", "", true, &models.PublicationRecovery{})
	require.Error(t, err)
}
