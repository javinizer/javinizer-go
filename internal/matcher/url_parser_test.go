package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInput(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedID    string
		expectedHint  string
		expectedIsURL bool
		expectError   bool
	}{
		{
			name:          "Plain JAV ID",
			input:         "IPX-535",
			expectedID:    "IPX-535",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:          "DMM URL with cid",
			input:         "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			expectedID:    "ipx00535",
			expectedHint:  "dmm",
			expectedIsURL: true,
		},
		{
			name:          "DMM URL with id parameter",
			input:         "https://video.dmm.co.jp/detail?id=abc123",
			expectedID:    "abc123",
			expectedHint:  "dmm",
			expectedIsURL: true,
		},
		{
			name:          "R18.dev URL",
			input:         "https://r18.dev/videos/vod/movies/detail/-/id=ipx00535/",
			expectedID:    "ipx00535",
			expectedHint:  "r18dev",
			expectedIsURL: true,
		},
		{
			name:          "R18.com URL",
			input:         "https://www.r18.com/videos/vod/movies/detail/-/id=ipx00535/",
			expectedID:    "ipx00535",
			expectedHint:  "r18dev",
			expectedIsURL: true,
		},
		{
			name:        "Empty input",
			input:       "",
			expectError: true,
		},
		{
			name:        "Whitespace only",
			input:       "   ",
			expectError: true,
		},
		{
			name:          "JAV ID with spaces",
			input:         "  IPX-535  ",
			expectedID:    "IPX-535",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:          "Lowercase JAV ID",
			input:         "ipx-535",
			expectedID:    "ipx-535",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:          "JAV ID without hyphen",
			input:         "IPX535",
			expectedID:    "IPX535",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:        "DMM URL without content ID",
			input:       "https://www.dmm.co.jp/",
			expectError: true,
		},
		{
			name:        "R18.dev URL without ID",
			input:       "https://r18.dev/",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseInput(tt.input)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedID, result.ID)
			assert.Equal(t, tt.expectedHint, result.ScraperHint)
			assert.Equal(t, tt.expectedIsURL, result.IsURL)
		})
	}
}

func TestExtractDMMContentID(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "cid parameter",
			url:      "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			expected: "ipx00535",
		},
		{
			name:     "id parameter",
			url:      "https://video.dmm.co.jp/detail?id=abc123",
			expected: "abc123",
		},
		{
			name:     "id parameter with ampersand",
			url:      "https://video.dmm.co.jp/detail?foo=bar&id=abc123",
			expected: "abc123",
		},
		{
			name:     "cid with query parameters",
			url:      "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/?ref=list",
			expected: "ipx00535",
		},
		{
			name:     "no match",
			url:      "https://www.dmm.co.jp/",
			expected: "",
		},
		{
			name:     "malformed cid",
			url:      "https://www.dmm.co.jp/cid=/",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDMMContentID(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractR18DevID(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "standard format",
			url:      "https://r18.dev/videos/vod/movies/detail/-/id=ipx00535/",
			expected: "ipx00535",
		},
		{
			name:     "with query parameters",
			url:      "https://r18.dev/videos/vod/movies/detail/-/id=abc123/?ref=list",
			expected: "abc123",
		},
		{
			name:     "r18.com domain",
			url:      "https://www.r18.com/videos/vod/movies/detail/-/id=test123/",
			expected: "test123",
		},
		{
			name:     "no match",
			url:      "https://r18.dev/",
			expected: "",
		},
		{
			name:     "malformed id",
			url:      "https://r18.dev/videos/vod/movies/detail/-/id=/",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractR18DevID(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}
