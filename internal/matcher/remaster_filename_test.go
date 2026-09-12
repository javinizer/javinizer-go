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

func TestMatchFile_ResolutionTokensAreNotContentIDs(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"1920x1080.mkv", ""},
		{"1280x720.mp4", ""},
		{"3840x2160.mkv", ""},
		{"1080p60.mkv", ""},
		{"1080i.mkv", ""},
		{"720p30.mkv", ""},
		{"118ipx00535.mkv", "118IPX00535"},
		{"436abf00030.mkv", "436ABF00030"},
		{"1rct00156h-pt2.mkv", "1RCT00156H"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, m.MatchString(tc.input))
			got := matchOne(t, m, tc.input)
			if tc.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.ID)
		})
	}
}
