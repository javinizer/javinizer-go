package resultstore

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestStagedUpdateReadVisibilityFollowsTransactionOutcome(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			store := New(1, []string{"file"})
			store.UpdateFileResult("file", &MovieResult{ResultID: "stable-id", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "OLD"}, FileMatchInfo: models.FileMatchInfo{MovieID: "OLD"}})
			store.SetProvenance("file", &ProvenanceData{FieldSources: map[string]string{"title": "before"}})
			before, counts := store.SnapshotForStatus()
			prepared, finish := make(chan struct{}), make(chan struct{})
			var finishOnce sync.Once
			unblock := func() { finishOnce.Do(func() { close(finish) }) }
			defer unblock()
			done := make(chan error, 1)
			injected := errors.New("transaction rollback")
			go func() {
				done <- store.AtomicUpdateFileResultWithProvenanceAfter("file", func(prepare func() error) error {
					if err := prepare(); err != nil {
						return err
					}
					close(prepared)
					<-finish
					if !commit {
						return injected
					}
					return nil
				}, func(r *MovieResult, p *ProvenanceData) (*MovieResult, *ProvenanceData, error) {
					r.Movie.ID = "NEW"
					r.FileMatchInfo.MovieID = "NEW"
					r.Status = models.JobStatusFailed
					p.FieldSources["title"] = "after"
					return r, p, nil
				})
			}()
			select {
			case <-prepared:
			case <-time.After(time.Second):
				t.Fatal("staged update did not prepare")
			}
			// A prepared transaction must retain the real lock, independently of
			// whether the concurrent reader below has been scheduled yet.
			tracker := store.(*ResultTracker)
			if tracker.mu.TryRLock() {
				tracker.mu.RUnlock()
				t.Fatal("prepared transaction released the store lock before its outcome")
			}
			readDone := make(chan struct{})
			var result *MovieResult
			var prov *ProvenanceData
			go func() { result, _ = store.GetMovieResult("file"); prov = store.GetProvenance("file"); close(readDone) }()
			select {
			case <-readDone:
				t.Fatal("provisional state became observable before transaction outcome")
			case <-time.After(25 * time.Millisecond):
			}
			unblock()
			var err error
			select {
			case err = <-done:
			case <-time.After(time.Second):
				t.Fatal("staged update did not return")
			}
			if commit {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, injected)
			}
			select {
			case <-readDone:
			case <-time.After(time.Second):
				t.Fatal("store remained locked")
			}
			after, afterCounts := store.SnapshotForStatus()
			if !commit {
				require.Equal(t, before, after)
				require.Equal(t, counts, afterCounts)
				require.Equal(t, "OLD", result.Movie.ID)
				require.Equal(t, "before", prov.FieldSources["title"])
			} else {
				require.Equal(t, "NEW", result.Movie.ID)
				require.Equal(t, "stable-id", result.ResultID)
				require.Equal(t, before.Results["file"].Revision+1, result.Revision)
				require.Equal(t, "after", prov.FieldSources["title"])
				require.Empty(t, store.FindFilePathsForMovieID("OLD"))
				require.Equal(t, []string{"file"}, store.FindFilePathsForMovieID("NEW"))
				require.Zero(t, afterCounts.Completed)
				require.Equal(t, 1, afterCounts.Failed)
			}
		})
	}
}
