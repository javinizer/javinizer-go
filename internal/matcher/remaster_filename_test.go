package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchStringRemasterFilename(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ input, want string }{
		{"RCT-156-HD.mkv", "RCT-156H"},
		{"/videos/RCT-156-HD.MP4", "RCT-156H"},
		{"DV-818-AI.mkv", "DV-818AI"},
		{"RCT-156-HD", "RCT-156H"},
		{"RCT-156-HD.pt2", "RCT-156H"},
		{"1rct00156hd.pt2.mkv", "1RCT00156HD"},
		{"RCT-156.mkv", "RCT-156"},
	} {
		t.Run(tc.input, func(t *testing.T) { assert.Equal(t, tc.want, m.MatchString(tc.input)) })
	}
}

func TestMatchFile_LeadingQualityLabelsDoNotPreemptCatalogID(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ input, want string }{
		{"FHD 1080 HD IPX-535.mkv", "IPX-535"},
		{"JAV 1080HD IPX-535.mkv", "IPX-535"},
		{"RCT 156 HD.mkv", "RCT-156H"},
		{"IPX 535 HD.mkv", "IPX-535H"},
		{"RCT 156 HD part-2.mkv", "RCT-156H"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, m.MatchString(tc.input))
			got := matchOne(t, m, tc.input)
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.ID)
		})
	}
}
