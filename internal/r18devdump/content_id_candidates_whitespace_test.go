package r18devdump

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentIDCandidatesWithMarker_UnicodeWhitespaceSeparators(t *testing.T) {
	for _, separator := range []string{"\t", "\n", "\r\n", "\v", "\f", "\u00a0", "\u2003", "\u202f"} {
		for _, display := range []string{"RCT-156 HD", "RCT 00156 HD", "DV 818 AI", "T 28123 HD", "T28 123 HD", "T 28123 Z HD", "T28 123 E AI"} {
			query := strings.ReplaceAll(display, " ", separator)
			t.Run(query, func(t *testing.T) {
				assert.Equal(t, ContentIDCandidatesWithMarker(display), ContentIDCandidatesWithMarker(query))
			})
		}
	}
	assert.Contains(t, ContentIDCandidatesWithMarker("RCT-156\tHD"), "1rct00156h")
	assert.Contains(t, ContentIDCandidatesWithMarker("T\u00a028123\u00a0HD"), "55t28123h")
	assert.NotContains(t, ContentIDCandidatesWithMarker("T\u00a028123\u00a0HD"), "9t28123h")
}

func TestContentIDCandidatesWithMarker_SurroundingWhitespaceKeepsIdentity(t *testing.T) {
	for _, id := range []string{"1rct00156hd", "rct00156hd", "t2800123hd", "h_003abc00123hd", "dv00899ai", "RCT-00156-HD", "T-28123-HD", "T28-123-HD"} {
		for _, padding := range []string{" ", "\t", "\u00a0", "\u2003"} {
			t.Run(padding+id+padding, func(t *testing.T) {
				assert.Equal(t, ContentIDCandidatesWithMarker(id), ContentIDCandidatesWithMarker(padding+id+padding))
			})
		}
	}
	got := ContentIDCandidatesWithMarker(" 1rct00156hd ")
	assert.Contains(t, got, "1rct00156hd")
	assert.NotContains(t, got, "1rct00156h")
}
