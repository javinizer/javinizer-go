package aggregator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The helpers explicitly return a sentinel outside their supported range.
// Production censor matching guards its offsets first; these are helper
// contracts, not new end-to-end replacement paths. Interior offsets below
// are valid UTF-8 codepoint boundaries, never synthetic split-codepoint input.
func TestWordBoundaryRunesRespectUnicodeAndSentinels(t *testing.T) {
	const text = "日é*界"
	for _, tc := range []struct {
		name          string
		text          string
		offset        int
		before, after rune
	}{
		{"empty", "", 0, -1, -1},
		{"negative", text, -1, -1, -1},
		{"start", text, 0, -1, '日'},
		{"after Japanese", text, len("日"), '日', 'é'},
		{"after accented Latin", text, len("日é"), 'é', '*'},
		{"after censor marker", text, len("日é*"), '*', '界'},
		{"end", text, len(text), '界', -1},
		{"past end", text, len(text) + 1, -1, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.before, boundaryRuneBefore(tc.text, tc.offset))
			require.Equal(t, tc.after, boundaryRuneAfter(tc.text, tc.offset))
		})
	}
}
