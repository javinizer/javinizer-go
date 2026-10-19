package matcher

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Candidate generation is not authority to reinterpret an ambiguous filename
// as a literal raw CID. Keep existing display tiers and explicit CID evidence.
func TestNumericWidthDisplayAndExplicitCIDContract(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, tc := range []struct{ input, id string }{
		{"rct1234hd", "RCT-1234H"}, {"rct12345hd", "RCT-12345H"},
		{"rct100000hd", "RCT-100000H"}, {"rct000123hd", "RCT-000123H"},
		{"rct100000h", "RCT-100000H"}, {"rct100000ai", "RCT-100000AI"},
		{"1rct100000hd", "1RCT100000HD"}, {"118rct100000hd", "118RCT100000HD"},
		{"h_003rct100000hd", "H_003RCT100000HD"},
		{"t281234hd", "T-281234H"}, {"T-281234-HD", "T-281234H"},
		{"T28-1234-HD", "T28-1234H"}, {"RCT-100000-HD", "RCT-100000H"},
		{"RCT-1000000-HD", "RCT-1000000H"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got := m.MatchFile(models.FileMatchInfo{Name: tc.input + ".mkv", Extension: ".mkv"})
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.input))
		})
	}
	// Unsupported compact widths do not gain raw identity from a wide helper.
	for _, input := range []string{"rct1000000hd", "rct0000123hd"} {
		assert.Empty(t, m.MatchString(input))
		assert.Nil(t, m.MatchFile(models.FileMatchInfo{Name: input + ".mkv", Extension: ".mkv"}))
	}
}

func TestNumericWidthCustomRegexKeepsOriginalCapture(t *testing.T) {
	m, err := NewMatcher(&Config{RegexEnabled: true, RegexPattern: `^(rct\d+(?:hd|ai|h))$`})
	require.NoError(t, err)
	for _, input := range []string{"rct12345hd", "rct100000hd", "rct1000000hd", "rct000123hd", "rct100000ai"} {
		got := m.MatchFile(models.FileMatchInfo{Name: input + ".mkv", Extension: ".mkv"})
		require.NotNil(t, got)
		assert.Equal(t, "regex", got.MatchedBy)
		assert.Equal(t, strings.ToUpper(input), got.ID)
		assert.Equal(t, strings.ToUpper(input), m.MatchString(input))
	}
}
