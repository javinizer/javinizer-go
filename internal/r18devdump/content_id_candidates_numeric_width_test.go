package r18devdump

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentIDCandidatesWithMarker_RawNumericWidth(t *testing.T) {
	for _, digits := range []string{"1234", "0123", "12345", "00123", "100000", "000123", "123456789", "000000123", "123456789012345678901234567890", "000000000000000000000000000123"} {
		for _, prefix := range []string{"rct", "1rct", "118rct", "h_003rct", "t28", "9t28"} {
			for _, suffix := range []string{"", "e", "z"} {
				for _, marker := range []string{"h", "hd", "ai"} {
					raw := prefix + digits + suffix + marker
					t.Run(raw, func(t *testing.T) {
						got := ContentIDCandidatesWithMarker(raw)
						require.NotEmpty(t, got)
						// Prefixless four-digit ordinary series keeps legacy display folding.
						// Compact t28 also has the valid T-series reading (28+digits).
						literal := prefix != "rct" || len(digits) >= 5
						if literal {
							assert.Equal(t, raw, got[0], "literal identity must lead, including beyond integer expansion range")
							assert.Equal(t, 1, countCandidate(got, raw))
							for _, candidate := range got {
								assert.True(t, strings.HasSuffix(candidate, suffix+marker), candidate)
							}
						} else if marker == "hd" {
							for _, candidate := range got {
								assert.True(t, strings.HasSuffix(candidate, suffix+"h"), candidate)
							}
						}
					})
				}
			}
		}
	}
	// A leading digit alone is not a raw CID: a series and number are required.
	for _, invalid := range []string{"123h", "123hd", "123ai", "123zhd", "123eai", "0h", "1rcthd",
		"", "h", "hd", "ai", "zhd", "rcthd", "rctai", "1hd", "123rcthd",
		"h_003rcthd", "n_600rctai", "+1rct123hd"} {
		assert.Empty(t, ContentIDCandidatesWithMarker(invalid), invalid)
	}

	// Malformed channel/catalog forms may retain legacy display normalization,
	// but must never be seeded as literal raw identities.
	for _, malformed := range []string{"h_003hd", "h_003123hd", "h__003rct123hd", "h_003_rct123hd", "1_rct123hd", "h_003rct123..hd"} {
		assert.NotContains(t, ContentIDCandidatesWithMarker(malformed), malformed)
	}

}

func TestContentIDCandidatesWithMarker_DisplayNumericWidth(t *testing.T) {
	for _, digits := range []string{"1234", "0123", "12345", "00123", "100000", "000123", "123456789", "000000123"} {
		for _, series := range []string{"RCT", "T", "T28"} {
			for _, suffix := range []string{"", "E", "Z"} {
				display := series + "-" + digits + suffix
				t.Run(display, func(t *testing.T) {
					h := ContentIDCandidatesWithMarker(display + "-H")
					require.NotEmpty(t, h)
					assert.Equal(t, h, ContentIDCandidatesWithMarker(display+"-HD"))
					for _, candidate := range h {
						assert.True(t, strings.HasSuffix(candidate, strings.ToLower(suffix)+"h"))
					}
					ai := ContentIDCandidatesWithMarker(display + "-AI")
					require.NotEmpty(t, ai)
					for _, candidate := range ai {
						assert.True(t, strings.HasSuffix(candidate, strings.ToLower(suffix)+"ai"))
					}
				})
			}
		}
	}
	// An explicit display spelling does not gain raw fallback on overflow.
	for _, series := range []string{"RCT", "T", "T28"} {
		for _, marker := range []string{"H", "HD", "AI"} {
			assert.Empty(t, ContentIDCandidatesWithMarker(series+"-123456789012345678901234567890-"+marker))
		}
	}
	assert.Contains(t, ContentIDCandidatesWithMarker("T-281234-HD"), "55t281234h")
	assert.NotContains(t, ContentIDCandidatesWithMarker("T-281234-HD"), "9t281234h")
	assert.Contains(t, ContentIDCandidatesWithMarker("T28-1234-HD"), "9t2801234h")
}

func countCandidate(candidates []string, want string) int {
	n := 0
	for _, candidate := range candidates {
		if candidate == want {
			n++
		}
	}
	return n
}
