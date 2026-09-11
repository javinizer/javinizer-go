package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentIDDotMultipart(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{"1rct00156h.pt2.mkv", "1rct00156h.part2.mkv", "1rct00156h.cd2.mkv"} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "1RCT00156H", got.ID)
			assert.Equal(t, 2, got.PartNumber)
			assert.True(t, got.IsMultiPart)
			assert.Equal(t, PatternExplicit, got.MultipartPattern)
			assert.Equal(t, "1RCT00156H", m.MatchString(name))
		})
	}
	id, remainder := contentIDPrefixMatch("1rct00156h.pt2")
	assert.Equal(t, "1rct00156h", id)
	assert.Equal(t, ".pt2", remainder)
	single := matchOne(t, m, "1rct00156h.mkv")
	require.NotNil(t, single)
	assert.False(t, single.IsMultiPart)
}
