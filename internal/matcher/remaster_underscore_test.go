package matcher

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnderscoreRawRemasterMarkers(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, id := range []string{"h_003abc00123hd", "h_003abc00123h", "h_003abc00123ai", "n_600abc00123hd", "h_003abc00123"} {
		for _, suffix := range []string{"", ".pt2"} {
			t.Run(id+suffix, func(t *testing.T) {
				name := id + suffix + ".mkv"
				got := matchOne(t, m, name)
				require.NotNil(t, got)
				assert.Equal(t, strings.ToUpper(id), got.ID)
				assert.Empty(t, got.RemasterMarker)
				assert.Equal(t, strings.ToUpper(id), m.MatchString(name))
				if suffix != "" {
					assert.True(t, got.IsMultiPart)
					assert.Equal(t, 2, got.PartNumber)
				} else {
					assert.False(t, got.IsMultiPart)
				}
			})
		}
	}
	assert.Equal(t, "RCT-156H", m.MatchString("RCT-156-HD.mkv"))
}
