package caribbeancom

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeMovieIDKeepsTokenOrderAndSuffixRules(t *testing.T) {
	for input, want := range map[string]string{
		"  https://example.invalid/MoviePages/012345_067/  ": "012345-067",
		" https://example.invalid/moviepages/012345-67/ ":    "012345-067",
		"987654-32 /moviepages/012345_067/":                  "987654-032",
		"prefix_012345-6789":                                 "prefix-012345-6789",
		"DISPLAY_ONLY":                                       "display-only",
	} {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, want, normalizeMovieID(input))
		})
	}
}
