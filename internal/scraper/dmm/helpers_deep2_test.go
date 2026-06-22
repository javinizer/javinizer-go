package dmm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripRentalSuffixDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ipx535r", "ipx535"},
		{"abw001r", "abw001"},
		{"ipx535", "ipx535"},     // no suffix
		{"ipx535R", "ipx535"},    // uppercase R
		{"ipx535ar", "ipx535ar"}, // not a digit before 'r'
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, stripRentalSuffix(tt.input), "input=%q", tt.input)
	}
}

func TestUniqueNonEmptyStringsDeep2(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{[]string{"a", "b", "a", "c"}, []string{"a", "b", "c"}},
		{[]string{"", "a", "  ", "b"}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		result := uniqueNonEmptyStrings(tt.input)
		assert.Equal(t, tt.expected, result)
	}
	// Empty/nil input
	assert.Empty(t, uniqueNonEmptyStrings([]string{}))
	assert.Empty(t, uniqueNonEmptyStrings(nil))
}

func TestNormalizeContentIDDeep2_StandardID(t *testing.T) {
	result := normalizeContentID("IPX-535")
	assert.Equal(t, "ipx00535", result)
}

func TestNormalizeContentIDDeep2_NoHyphen(t *testing.T) {
	result := normalizeContentID("ipx535")
	assert.Equal(t, "ipx00535", result)
}

func TestNormalizeContentIDDeep2_Amateur(t *testing.T) {
	// Amateur IDs with 4+ letter prefix don't get zero-padding
	result := normalizeContentID("oreco183")
	assert.Equal(t, "oreco183", result)
}

func TestNormalizeContentIDDeep2_DMMPrefix(t *testing.T) {
	result := normalizeContentID("4sone860")
	// DMM prefix stripped then padded: "4sone860" -> "sone860" -> "sone00860"
	assert.Contains(t, result, "sone")
	assert.Contains(t, result, "860")
}

func TestNormalizeIDDeep2_Standard(t *testing.T) {
	assert.Equal(t, "IPX-535", normalizeID("ipx00535"))
	assert.Equal(t, "SONE-860", normalizeID("sone860"))
	assert.Equal(t, "MDB-087", normalizeID("61mdb087"))
}

func TestNormalizeIDDeep2_DMMPrefix(t *testing.T) {
	assert.Equal(t, "SMKCX-003", normalizeID("h_1472smkcx003"))
}

func TestNormalizeIDDeep2_ShortNumber(t *testing.T) {
	// Numbers less than 3 digits should be padded
	assert.Equal(t, "ABC-001", normalizeID("abc1"))
}

func TestNormalizeIDDeep2_AllZeros(t *testing.T) {
	assert.Equal(t, "ABC-000", normalizeID("abc000"))
}

func TestExtractContentIDFromURLDeep2_CIDFormat(t *testing.T) {
	url := "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/"
	assert.Equal(t, "ipx00535", extractContentIDFromURL(url))
}

func TestExtractContentIDFromURLDeep2_IDFormat(t *testing.T) {
	url := "https://video.dmm.co.jp/av/content/?id=abc123"
	assert.Equal(t, "abc123", extractContentIDFromURL(url))
}

func TestExtractContentIDFromURLDeep2_NoMatch(t *testing.T) {
	assert.Equal(t, "", extractContentIDFromURL("https://example.com/no-id"))
}

func TestMatchesWithVariantSuffixDeep2(t *testing.T) {
	assert.True(t, matchesWithVariantSuffix("akdl229a", "akdl229"))
	assert.True(t, matchesWithVariantSuffix("ipx535b", "ipx535"))
	assert.True(t, matchesWithVariantSuffix("ipx535", "ipx535")) // exact match
	assert.False(t, matchesWithVariantSuffix("ipx535", "abc123"))
	assert.False(t, matchesWithVariantSuffix("akdl229ab", "akdl229")) // 2-char suffix
}

func TestHiraganaToRomajiDeep2(t *testing.T) {
	// Test basic hiragana conversion
	assert.Equal(t, "a", hiraganaToRomaji("あ"))
	assert.Equal(t, "ka", hiraganaToRomaji("か"))
	assert.Equal(t, "sa", hiraganaToRomaji("さ"))
}

func TestHiraganaToRomajiDeep2_NonHiragana(t *testing.T) {
	// Non-hiragana characters should pass through
	assert.Equal(t, "abc", hiraganaToRomaji("abc"))
	assert.Equal(t, "", hiraganaToRomaji(""))
}

func TestBuildResolveContentIDSearchQueriesDeep2(t *testing.T) {
	queries := buildResolveContentIDSearchQueries("IPX-535", "ipx00535")
	assert.NotEmpty(t, queries)
	// Should contain deduplicated search terms
	assert.Contains(t, queries, "ipx535")
}

func TestNormalizedContentIDWithoutPaddingDeep2(t *testing.T) {
	assert.Equal(t, "ipx535", normalizedContentIDWithoutPadding("ipx00535"))
	assert.Equal(t, "sone860", normalizedContentIDWithoutPadding("sone00860"))
	assert.Equal(t, "", normalizedContentIDWithoutPadding(""))
}
