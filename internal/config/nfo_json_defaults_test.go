package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The config API decodes a fresh NFOConfig, so a payload from a client that
// predates include_actress_images must keep the documented default (true)
// instead of silently persisting the Go zero value.
func TestNFOConfigUnmarshalJSONKeepsIncludeActressImagesDefault(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"omitted keeps documented default", `{"enabled":true}`, true},
		{"explicit false honoured", `{"include_actress_images":false}`, false},
		{"explicit true honoured", `{"include_actress_images":true}`, true},
		{"null keeps documented default", `{"include_actress_images":null}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var n NFOConfig
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &n))
			assert.Equal(t, tc.want, n.Feature.IncludeActressImages)
		})
	}
}

func TestNFOConfigUnmarshalJSONKeepsOtherDocumentedTrueDefaults(t *testing.T) {
	var n NFOConfig
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true}`), &n))

	assert.True(t, n.Feature.IncludeFanart, "include_fanart defaults to true")
	assert.True(t, n.Feature.IncludeTrailer, "include_trailer defaults to true")
	assert.True(t, n.Feature.IncludeActressImages, "include_actress_images defaults to true")
	assert.False(t, n.Feature.IncludeStreamDetails, "include_stream_details defaults to false")
}

func TestNFOConfigUnmarshalJSONNestedLegacyKeepsDocumentedDefaults(t *testing.T) {
	var explicit NFOConfig
	require.NoError(t, json.Unmarshal([]byte(`{"Feature":{"enabled":false,"include_actress_images":false}}`), &explicit))
	assert.False(t, explicit.Feature.IncludeActressImages, "explicit false inside the nested shape is honoured")

	var omitted NFOConfig
	require.NoError(t, json.Unmarshal([]byte(`{"Feature":{"enabled":false}}`), &omitted))
	assert.True(t, omitted.Feature.IncludeActressImages)
	assert.True(t, omitted.Feature.IncludeFanart)
	assert.True(t, omitted.Feature.IncludeTrailer)
}
