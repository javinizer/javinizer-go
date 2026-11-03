package nfo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// These are private helper contracts. The public remapping caller checks for a
// matching prefix before calling stripCodePrefix, so these preservation cases
// are not claims about new public remapping paths.
func TestStripCodePrefixPreservesUnprefixedTitles(t *testing.T) {
	for _, tc := range []struct {
		name, title, movieID, want string
	}{
		{"empty ID", "  手書きの題名！  ", "", "手書きの題名！"},
		{"different ID", "  [OTHER-002] Imported title!  ", "ABC-001", "[OTHER-002] Imported title!"},
		{"plain title", "  A manually edited title.  ", "ABC-001", "A manually edited title."},
		{"bracketed prefix control", "  [abc-001] 題名  ", "ABC-001", "題名"},
		{"bare prefix control", "  ABC-001 - Imported title  ", "abc-001", "Imported title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, stripCodePrefix(tc.title, tc.movieID))
		})
	}
}
