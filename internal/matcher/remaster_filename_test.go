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
		{"RCT-156-HD.pt2", "RCT-156"},
		{"1rct00156hd.pt2.mkv", "1RCT00156HD"},
		{"RCT-156.mkv", "RCT-156"},
	} {
		t.Run(tc.input, func(t *testing.T) { assert.Equal(t, tc.want, m.MatchString(tc.input)) })
	}
}
