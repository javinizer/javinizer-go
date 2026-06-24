package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseJobResultsJSON_LegacyFormatWithNullEntry verifies that a null entry
// in a legacy FileResult payload does not cause misrouting to the old
// MovieResult parser. The old code broke on the first successfully-parsed map
// value; a null unmarshals to a nil map (no error, no data_type key), which
// would prematurely break and lose legacy "data" decoding.
func TestParseJobResultsJSON_LegacyFormatWithNullEntry(t *testing.T) {
	// Legacy format: every entry has "data_type" + "data", but one entry is null.
	legacy := `{
		"/v/ABP-731.mp4": null,
		"/v/ABP-980.mp4": {
			"file_path": "/v/ABP-980.mp4",
			"movie_id": "ABP-980",
			"status": "completed",
			"data_type": "movie",
			"data": {"id": "ABP-980", "content_id": "ABP-980", "title": "Test"}
		}
	}`

	parsed, err := ParseJobResultsJSON([]byte(legacy))
	require.NoError(t, err)

	// The null entry should be skipped, and the legacy "data" field should be
	// decoded into Movie (proving legacy routing, not old-MovieResult routing).
	r, ok := parsed.Results["/v/ABP-980.mp4"]
	require.True(t, ok, "non-null legacy entry should be present")
	require.NotNil(t, r.Movie, "legacy 'data' field should be decoded into Movie")
	assert.Equal(t, "ABP-980", r.Movie.ID)
}

// TestParseJobResultsJSON_LegacyNilEntryNoPanic verifies that nil legacy
// entries don't panic on deref (parseLegacyFileResultFormat guards lfr == nil).
func TestParseJobResultsJSON_LegacyNilEntryNoPanic(t *testing.T) {
	legacy := `{
		"/v/NULL.mp4": null,
		"/v/OK.mp4": {
			"file_path": "/v/OK.mp4",
			"movie_id": "OK",
			"status": "completed",
			"data_type": "movie",
			"data": {"id": "OK"}
		}
	}`

	assert.NotPanics(t, func() {
		parsed, err := ParseJobResultsJSON([]byte(legacy))
		require.NoError(t, err)
		_, nullOk := parsed.Results["/v/NULL.mp4"]
		assert.False(t, nullOk, "null entry should be skipped")
	})
}

// TestParseJobResultsJSON_NullEntryInOldFormat verifies null entries in the
// old MovieResult format are also skipped (pre-existing guard).
func TestParseJobResultsJSON_NullEntryInOldFormat(t *testing.T) {
	old := `{
		"/v/NULL.mp4": null,
		"/v/OK.mp4": {
			"file_match_info": {"path": "/v/OK.mp4", "movie_id": "OK"},
			"status": "completed",
			"movie": {"id": "OK"}
		}
	}`

	parsed, err := ParseJobResultsJSON([]byte(old))
	require.NoError(t, err)
	_, nullOk := parsed.Results["/v/NULL.mp4"]
	assert.False(t, nullOk, "null entry should be skipped")
	r := parsed.Results["/v/OK.mp4"]
	require.NotNil(t, r)
	assert.Equal(t, "OK", r.Movie.ID)
}

// TestParseJobResultsJSON_LegacyMixedNullAndRealData verifies a legacy payload
// where the null entry comes first in JSON (though map iteration is random).
// The format detection must scan ALL values for data_type, not just the first.
func TestParseJobResultsJSON_LegacyMixedNullAndRealData(t *testing.T) {
	// This payload is unambiguously legacy (has data_type). The null entry
	// must not cause misrouting.
	legacy := `{
		"/v/A.mp4": null,
		"/v/B.mp4": null,
		"/v/C.mp4": {
			"file_path": "/v/C.mp4",
			"movie_id": "C",
			"status": "completed",
			"data_type": "movie",
			"data": {"id": "C", "title": "Legacy C"}
		}
	}`

	parsed, err := ParseJobResultsJSON([]byte(legacy))
	require.NoError(t, err)
	r := parsed.Results["/v/C.mp4"]
	require.NotNil(t, r)
	require.NotNil(t, r.Movie, "legacy data field must be decoded — proves correct routing")
	assert.Equal(t, "Legacy C", r.Movie.Title)
}
