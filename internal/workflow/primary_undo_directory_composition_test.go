package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPrimaryUndoDirectoryCompositionPreservesEveryClaim(t *testing.T) {
	mode := uint32(0400)
	proof := models.DeleteEntry{Path: "/current/published.srt", RecoveryPublication: true, IdentityStrong: true, IdentityMode: &mode, IdentityDev: 7, IdentityIno: 11}
	undo := &models.PrimaryUndo{Token: "synthetic-attempt", State: "published", Source: "/current/video.mkv", Destination: "/original/video.mkv"}
	directory := &models.DirectoryMove{Original: "/original", Locations: []string{"/current", "/scratch"}, Device: 3, Inode: 9}
	for _, primary := range []*models.PrimaryUndo{nil, undo} {
		for _, resident := range []bool{false, true} {
			for _, payload := range []string{"", `{"delete":["/current/new.nfo"]}`, `{"directory_move":{"original":"/original","locations":["/current","/next"],"device":3,"inode":9}}`} {
				prior := models.GeneratedFilesJSON{PrimaryUndo: primary, PrimaryResident: resident, DirectoryMove: directory, MoveBack: []models.FileMove{{OriginalPath: "/original/published.srt", NewPath: proof.Path, Proof: &proof}}, PlannedDeletes: []models.DeleteEntry{{Path: "/current/unknown.jpg", RecoveryUnproven: true}}}
				merged, err := models.ParseGeneratedFiles(mergeReplacementLedgerIntents(models.MarshalLedgerJSON(prior), payload, false))
				require.NoError(t, err)
				require.Equal(t, primary, merged.PrimaryUndo)
				require.Equal(t, resident, merged.PrimaryResident)
				require.Equal(t, directory.Original, merged.DirectoryMove.Original)
				require.Equal(t, directory.Device, merged.DirectoryMove.Device)
				require.Equal(t, directory.Inode, merged.DirectoryMove.Inode)
				require.Subset(t, merged.DirectoryMove.Locations, directory.Locations)
				require.Equal(t, prior.MoveBack, merged.MoveBack)
				require.Equal(t, prior.PlannedDeletes, merged.PlannedDeletes)
				// Contradictory rows are retained for history's refusal, never
				// normalized by silently dropping either ownership claim.
			}
		}
	}
	prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryUndo: undo, PrimaryResident: true, DirectoryMove: directory})
	recovery := &models.PublicationRecovery{Managed: []string{proof.Path}, Moves: []models.FileMove{{NewPath: proof.Path, OriginalPath: "/original/published.srt", Proof: &proof}}}
	next, persist, _, err := completionLedgerMergeOpt(prior, "", "", false, recovery)
	require.NoError(t, err)
	require.True(t, persist)
	require.Equal(t, undo, next.PrimaryUndo)
	require.True(t, next.PrimaryResident)
	require.Equal(t, directory, next.DirectoryMove)
	require.Equal(t, recovery.Moves, next.MoveBack)
	require.Equal(t, &mode, next.MoveBack[0].Proof.IdentityMode)
}
