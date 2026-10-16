package resultstore

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStagedUpdatePublishesOnlyAfterTransactionSuccess(t *testing.T) {
	for _, mode := range []string{"success", "skipped", "transaction-error-before", "transaction-error-after", "update-error", "double-prepare", "panic-update", "panic-after"} {
		t.Run(mode, func(t *testing.T) {
			store := New(1, []string{"file"})
			store.UpdateFileResult("file", &MovieResult{ResultID: "id", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "MOVIE-1"}})
			store.SetProvenance("file", &ProvenanceData{FieldSources: map[string]string{"title": "before"}})
			before, beforeCounts := store.SnapshotForStatus()
			failure := errors.New("transaction did not commit")
			update := func(current *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
				current.Movie.Title = "After"
				current.Status = models.JobStatusFailed
				prov.FieldSources["title"] = "after"
				if mode == "update-error" {
					return current, prov, failure
				}
				if mode == "panic-update" {
					panic("update panic")
				}
				return current, prov, nil
			}
			run := func() error {
				return store.AtomicUpdateFileResultWithProvenanceAfter("file", func(prepare func() error) error {
					// Transaction entry must not hold the store lock: DB writers
					// can read an envelope before prepare acquires it.
					_, err := store.GetMovieResult("file")
					require.NoError(t, err)
					if mode == "skipped" {
						return nil
					}
					if mode == "transaction-error-before" {
						return failure
					}
					if err := prepare(); err != nil {
						// Even a transaction that ignores prepare failure cannot
						// commit its provisional state.
						return nil
					}
					switch mode {
					case "transaction-error-after":
						return failure
					case "double-prepare":
						_ = prepare()
					case "panic-after":
						panic("commit panic")
					}
					return nil
				}, update)
			}
			if mode == "panic-update" || mode == "panic-after" {
				assert.Panics(t, func() { _ = run() })
			} else if mode == "success" || mode == "skipped" {
				assert.NoError(t, run())
			} else {
				assert.Error(t, run())
			}
			// These reads also prove every error/panic path released the lock.
			after, afterCounts := store.SnapshotForStatus()
			if mode == "success" {
				assert.Equal(t, uint64(2), after.Results["file"].Revision)
				assert.Equal(t, "After", after.Results["file"].Movie.Title)
				assert.Equal(t, "after", after.Provenance["file"].FieldSources["title"])
				assert.Zero(t, afterCounts.Completed)
				assert.Equal(t, 1, afterCounts.Failed)
			} else {
				assert.Equal(t, before, after)
				assert.Equal(t, beforeCounts, afterCounts)
			}
		})
	}
}

func TestStagedUpdateMissingRowCannotPublish(t *testing.T) {
	store := New(0, nil)
	err := store.AtomicUpdateFileResultWithProvenanceAfter("missing", func(prepare func() error) error {
		_ = prepare()
		return nil
	}, func(current *MovieResult, prov *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
		t.Fatal("missing row must not invoke updater")
		return current, prov, nil
	})
	require.ErrorContains(t, err, "not found")
	assert.Empty(t, store.GetResults())
}
