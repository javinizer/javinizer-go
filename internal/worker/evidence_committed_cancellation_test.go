package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestLateEvidenceCancellationPreservesAlreadyPersistedResult(t *testing.T) {
	const path = "movie.mp4"
	store := resultstore.New(1, []string{path})
	store.UpdateFileResult(path, &resultstore.MovieResult{
		Status:    models.JobStatusCompleted,
		Persisted: true,
		Movie:     &models.Movie{ID: "TEST-001", Title: "Committed title"},
	})
	store.SetProvenance(path, &resultstore.ProvenanceData{
		FieldSources:    map[string]string{"title": "r18dev"},
		OfflineEvidence: workerEvidence("committed-generation"),
	})
	before, err := store.GetMovieResult(path)
	require.NoError(t, err)
	provenance := store.GetProvenance(path)
	// A delayed cancellation must not turn a committed result into a cancelled
	// one, discard its metadata, or detach its coherently published evidence.
	markUnpersistedEvidenceCancelled(store, path, context.Canceled)
	after, err := store.GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, provenance, store.GetProvenance(path))
}
