package resultstore_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResultUpdateKeepsStableIDIndexAndRejectsDiscardedAlias(t *testing.T) {
	for _, incomingID := range []string{"", "discarded-incoming-id"} {
		t.Run("incoming="+incomingID, func(t *testing.T) {
			const file = "owned-file.mp4"
			store := resultstore.New(1, []string{file})
			store.UpdateFileResult(file, &resultstore.MovieResult{ResultID: "stable-result-id", FileMatchInfo: models.FileMatchInfo{Path: file, MovieID: "OLD-001"}, Status: models.JobStatusRunning})
			before, path, found := store.GetFileResultByResultID("stable-result-id")
			require.True(t, found)
			require.Equal(t, file, path)
			require.Equal(t, uint64(1), before.Revision)
			incoming := &resultstore.MovieResult{ResultID: incomingID, FileMatchInfo: models.FileMatchInfo{Path: file, MovieID: "NEW-002"}, Status: models.JobStatusFailed, Error: "provider unavailable"}
			store.UpdateFileResult(file, incoming)
			all := store.GetResults()
			require.Len(t, all, 1)
			require.Equal(t, "stable-result-id", all[0].ResultID)
			require.Equal(t, uint64(2), all[0].Revision)
			require.Equal(t, models.JobStatusFailed, all[0].Status)
			require.Empty(t, store.FindFilePathsForMovieID("OLD-001"))
			require.Equal(t, []string{file}, store.FindFilePathsForMovieID("new-002"))
			_, progress := store.SnapshotForStatus()
			require.Equal(t, 0, progress.Completed)
			require.Equal(t, 1, progress.Failed)
			require.Equal(t, float64(100), progress.Progress)
			// Stored state must be a clone, not the caller's mutable input.
			incoming.Error = "caller mutation"
			require.Equal(t, "provider unavailable", store.GetResults()[0].Error)
			resolved, resolvedPath, stableFound := store.GetFileResultByResultID("stable-result-id")
			assert.True(t, stableFound, "published stable identity must remain addressable after update")
			if stableFound {
				assert.Equal(t, file, resolvedPath)
				assert.Equal(t, all[0], *resolved)
			}
			if incomingID != "" {
				_, _, aliasFound := store.GetFileResultByResultID(incomingID)
				assert.False(t, aliasFound, "an incoming identity discarded by stable-ID preservation must not become an alias")
			}
		})
	}
}
