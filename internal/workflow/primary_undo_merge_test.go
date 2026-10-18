package workflow

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrimaryUndoJournalSurvivesCompletionMerge(t *testing.T) {
	proof := &models.PrimaryUndo{Token: "test-attempt", State: "published", Source: "/library/movie.mkv", Destination: "/incoming/movie.mkv"}
	for _, payload := range []string{"", `{"delete":["/library/new.nfo"]}`} {
		prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryUndo: proof})
		next, err := models.ParseGeneratedFiles(mergeReplacementLedgerIntents(prior, payload, false))
		require.NoError(t, err)
		require.Equal(t, proof, next.PrimaryUndo)
	}
}
