package javstash

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Decode ordinary provider JSON before the existing pure scene parser. No
// private error hooks, client overrides or live-provider requests are involved.
func TestSceneParserPreservesCanonicalProvenanceAndProductIdentity(t *testing.T) {
	const dmm = "https://www.dmm.co.jp/digital/video/-/detail/=/cid=ipx00535/"
	for _, tc := range []struct{ name, urls, source string }{
		{"absent URLs", `[]`, "https://javstash.org/scenes/scene-123"},
		{"empty external URL", `[{"url":""}]`, "https://javstash.org/scenes/scene-123"},
		{"non-DMM URL does not replace scene provenance", `[{"url":"https://example.invalid/provider"}]`, "https://javstash.org/scenes/scene-123"},
		{"empty DMM content ID does not replace provenance", `[{"url":"https://www.dmm.co.jp/?cid="}]`, "https://javstash.org/scenes/scene-123"},
		{"first qualifying DMM URL wins", `[{"url":"https://example.invalid/provider"},{"url":"` + dmm + `"},{"url":"https://www.dmm.co.jp/?cid=other"}]`, dmm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input scene
			require.NoError(t, json.Unmarshal([]byte(`{"id":"scene-123","code":"IPX-535","title":"Provider title","details":"Description","duration":600,"urls":`+tc.urls+`}`), &input))
			before, err := json.Marshal(input)
			require.NoError(t, err)
			s := &scraper{baseURL: "https://javstash.org/graphql", language: "ja"}
			got, err := s.parseScene(&input, "different-query")
			require.NoError(t, err)
			require.Equal(t, tc.source, got.SourceURL)
			require.Equal(t, "IPX-535", got.ID)
			require.Equal(t, "IPX-535", got.ContentID, "DMM provenance must not replace canonical product identity")
			require.Equal(t, "Provider title", got.Title)
			require.Equal(t, "Description", got.Description)
			require.Equal(t, 10, got.Runtime)
			after, err := json.Marshal(input)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
