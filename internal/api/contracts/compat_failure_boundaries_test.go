package contracts

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchFileResultRejectsMalformedMoviePayloads(t *testing.T) {
	for _, tc := range []struct{ name, payload, prefix string }{
		{"outer type", `[]`, "cannot unmarshal array"},
		{"current movie", `{"movie":"not an object","data":{"id":"LEGACY-001"}}`, "movie field:"},
		{"legacy movie", `{"movie":null,"data":"not an object"}`, "legacy data field:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result BatchFileResult
			err := json.Unmarshal([]byte(tc.payload), &result)
			require.ErrorContains(t, err, tc.prefix)
			require.Nil(t, result.Movie, "invalid current data must not silently fall back to a legacy movie")
		})
	}
}

func TestMovieViewToModelPreservesAbsentMovie(t *testing.T) {
	require.Nil(t, MovieViewToModel(nil), "an absent editable movie must not become a phantom empty record")
}
