package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A resolution token before a raw content id must not block the scan: the
// leftmost shape hit is never the candidate, and a strong raw id later in
// the name still wins (the already-covered standalone-token case).
func TestContentIDCandidateResolutionPrefix(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, id, marker string
	}{
		{"1080p60 1rct00156h.mkv", "1RCT00156H", ""},
		{"1920x1080 dv00899ai.mkv", "DV00899AI", ""},
		{"1080p t28-123-hd.mkv", "T28-123H", "HD"},
		{"1080p60.mkv", "", ""},
		{"1080p60 birthday2024.mkv", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
}
