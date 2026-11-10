package resultstore

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Pin the readers' defensive behavior for an inconsistent internal index:
// a dangling entry must never be returned as a successful nil result.
func TestReadStoreRejectsDanglingIndexEntries(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "nil result"
		if missing {
			name = "missing result"
		}
		t.Run(name, func(t *testing.T) {
			const path = "ABC-001.mp4"
			rt := New(1, []string{path}).(*ResultTracker)
			rt.UpdateFileResult(path, &MovieResult{ResultID: "stable-id", Movie: &models.Movie{ID: "ABC-001"}})
			// Inject only the damaged result slot, retaining the real indexes built
			// by UpdateFileResult. No public mutation is claimed to create this state.
			rt.mu.Lock()
			if missing {
				delete(rt.Results, path)
			} else {
				rt.Results[path] = nil
			}
			rt.mu.Unlock()
			lookup, err := rt.FindFileForMovieID("abc-001")
			require.Nil(t, lookup)
			require.EqualError(t, err, `result not found for movie ID "abc-001" at path "ABC-001.mp4"`)
			movie, err := rt.FindMovieResultForMovieID("abc-001")
			require.Nil(t, movie)
			require.EqualError(t, err, `no result for movie ID "abc-001" at path "ABC-001.mp4"`)
			result, foundPath, found := rt.GetFileResultByResultID("stable-id")
			require.Nil(t, result)
			require.Empty(t, foundPath)
			require.False(t, found)
		})
	}
}
