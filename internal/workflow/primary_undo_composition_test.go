package workflow

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrimaryUndoCompositionPreservesPublicationProofs(t *testing.T) {
	mode := uint32(0400)
	proof := models.DeleteEntry{Path: "/out/subtitle.srt", RecoveryPublication: true, IdentityStrong: true, IdentityMode: &mode, IdentityDev: 7, IdentityIno: 11, IdentitySize: 23, IdentityModUnix: 1700000000}
	undo := &models.PrimaryUndo{Token: "primary-attempt", State: "published", Source: "/out/video.mkv", Destination: "/in/video.mkv"}
	for _, resident := range []bool{false, true} {
		for _, payload := range []string{"", `{"delete":["/out/new.nfo"]}`} {
			prior := models.GeneratedFilesJSON{PrimaryUndo: undo, PrimaryResident: resident, MoveBack: []models.FileMove{{NewPath: proof.Path, OriginalPath: "/in/subtitle.srt", Proof: &proof}}, PlannedDeletes: []models.DeleteEntry{{Path: "/out/unproven.jpg", RecoveryUnproven: true}}}
			merged, err := models.ParseGeneratedFiles(mergeReplacementLedgerIntents(models.MarshalLedgerJSON(prior), payload, false))
			require.NoError(t, err)
			require.Equal(t, undo, merged.PrimaryUndo)
			require.Equal(t, resident, merged.PrimaryResident)
			require.Equal(t, prior.MoveBack, merged.MoveBack)
			require.Equal(t, prior.PlannedDeletes, merged.PlannedDeletes)
		}
	}
	prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryUndo: undo})
	recovery := &models.PublicationRecovery{Managed: []string{proof.Path}, Moves: []models.FileMove{{NewPath: proof.Path, OriginalPath: "/in/subtitle.srt", Proof: &proof}}}
	next, persist, _, err := completionLedgerMergeOpt(prior, "", "", false, recovery)
	require.NoError(t, err)
	require.True(t, persist)
	require.Equal(t, undo, next.PrimaryUndo)
	require.Equal(t, recovery.Moves, next.MoveBack)
	require.Equal(t, &mode, next.MoveBack[0].Proof.IdentityMode)
}
