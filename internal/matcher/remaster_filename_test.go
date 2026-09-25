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
		{"FHD 1080 HD ABC-12.mkv", "ABC-12"},
		{"FHD 1080 HD ABC-123456.mkv", "ABC-123456"},
		{"FHD 1080 HD FC2-PPV-123456.mkv", "PPV-123456"},
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

// A quality-shaped remaster phrase followed by a plain hyphenated id at
// the digit counts the conservative trailing grammar reserves for quality
// tokens (one, four, five digits): the phrase is consumed vocabulary, so
// the trailing candidate is the real catalog id and the phrase's remaster
// marker moves onto it. The quality tokens themselves (FHD-1080,
// UHD-3840, HD-720, AVC-1) never become ids, and a filename of only
// quality tokens keeps its pinned fallback behavior.
func TestMatchFile_QualityPhrasePlainHyphenatedCatalogID(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ input, want string }{
		{"FHD 1080 HD ABC-1234.mkv", "ABC-1234H"},
		{"FHD 1080 HD ABC-12345.mkv", "ABC-12345H"},
		{"FHD 1080 HD ABC-1.mkv", "ABC-1H"},
		// Quality tokens at the widened digit counts stay suppressed: the
		// series word is display vocabulary, not a catalog id.
		{"FHD 1080 HD FHD-1080.mkv", "FHD-1080H"},
		{"FHD 1080 HD UHD-3840.mkv", "FHD-1080H"},
		{"FHD 1080 HD HD-720.mkv", "FHD-1080H"},
		{"FHD 1080 HD AVC-1.mkv", "FHD-1080H"},
		// A filename of only quality tokens pins the pre-existing fallback:
		// the marker-less form matches nothing; the marker form keeps the
		// synthetic phrase id.
		{"FHD 1080.mkv", ""},
		{"FHD 1080 HD.mkv", "FHD-1080H"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, m.MatchString(tc.input))
			if tc.want == "" {
				assert.Nil(t, matchOne(t, m, tc.input))
				return
			}
			got := matchOne(t, m, tc.input)
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.ID)
		})
	}

	// The quality phrase's remaster marker moves onto the trailing id.
	for _, tc := range []struct{ input, id, marker string }{
		{"FHD 1080 HD ABC-1234.mkv", "ABC-1234H", "HD"},
		{"FHD 1080 HD ABC-12345.mkv", "ABC-12345H", "HD"},
		{"FHD 1080 HD ABC-1.mkv", "ABC-1H", "HD"},
	} {
		t.Run(tc.input+" marker", func(t *testing.T) {
			got := matchOne(t, m, tc.input)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
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
