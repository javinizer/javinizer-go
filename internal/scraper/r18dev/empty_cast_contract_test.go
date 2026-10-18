package r18dev

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The adapter's output slice shape does not carry cast-removal intent:
// absent, null, and explicitly empty arrays all become allocated empty slices.
func TestOmittedNullAndEmptyCastFixturesProduceAllocatedEmpty(t *testing.T) {
	minimal := loadTestData(t, "minimal_response.json")
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(minimal, &fields))
	_, hasActresses := fields["actresses"]
	require.False(t, hasActresses, "minimal fixture must genuinely omit the field")
	fields["actresses"] = json.RawMessage("null")
	nullCast, err := json.Marshal(fields)
	require.NoError(t, err)
	cases := []struct {
		name     string
		payload  []byte
		inputNil bool
	}{
		{"omitted", minimal, true},
		{"null", nullCast, true},
		{"empty_array", loadTestData(t, "empty_arrays_response.json"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createTestSettings(true)
			s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
			t.Cleanup(func() { require.NoError(t, s.Close()) })
			var data r18Response
			require.NoError(t, json.Unmarshal(tc.payload, &data))
			require.Equal(t, tc.inputNil, data.Actresses == nil)
			stubR18MetadataRequests(t, s)
			result, err := s.parseResponse(t.Context(), &data, "https://r18.dev/test")
			require.NoError(t, err)
			require.NotNil(t, result.Actresses)
			require.Empty(t, result.Actresses)
		})
	}
}
