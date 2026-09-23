package batch

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// Repository-read provenance is ephemeral: reload must reacquire authority from the repo.
func TestPersistedMovieServerOnlySerialization(t *testing.T) {
	original := resultstore.MovieResult{ResultID: "r-1", Revision: 7, PersistedMovie: true}
	stored, err := json.Marshal(original)
	require.NoError(t, err)
	require.NotContains(t, string(stored), "persisted_movie")
	require.Contains(t, string(stored), "revision")

	var reloaded resultstore.MovieResult
	require.NoError(t, json.Unmarshal(stored, &reloaded))
	require.False(t, reloaded.PersistedMovie)
	require.Equal(t, original.Revision, reloaded.Revision)
	require.NoError(t, json.Unmarshal([]byte(`{"persisted_movie":true,"PersistedMovie":true}`), &reloaded))
	require.False(t, reloaded.PersistedMovie, "client JSON cannot assert repository provenance")

	for name, response := range map[string]any{
		"full": movieResultToResponse(&original, nil),
		"slim": movieResultToSlimResponse(&original, nil),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(response)
			require.NoError(t, err)
			require.NotContains(t, string(data), "persisted_movie")
			require.NotContains(t, string(data), "PersistedMovie")
			if name == "full" {
				require.Contains(t, string(data), "revision")
			}
		})
	}
}
