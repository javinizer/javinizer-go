package r18dev

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeIDDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"IPX-535", "ipx535"},
		{"ABP-420", "abp420"},
		{"abc123", "abc123"},
		{"IPX 535", "ipx535"}, // space removed
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, normalizeID(tt.input), "input=%q", tt.input)
	}
}

func TestNormalizeIDWithoutStrippingDeep2(t *testing.T) {
	// Should NOT strip DMM prefix
	assert.Equal(t, "61mdb087", normalizeIDWithoutStripping("61MDB-087"))
	assert.Equal(t, "ipx535", normalizeIDWithoutStripping("IPX-535"))
}

func TestStripDMMPrefixDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"4sone860", "sone860"},
		{"118abw001", "abw001"},
		{"sone-860", "sone-860"}, // no prefix, unchanged
		{"IPX-535", "IPX-535"},   // no prefix, unchanged
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, stripDMMPrefix(tt.input), "input=%q", tt.input)
	}
}

func TestContentIDToIDDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"118abw00001", "ABW-001"},
		{"ipx00535", "IPX-535"},
		{"4sone00860", "SONE-860"},
		{"invalid", "INVALID"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, contentIDToID(tt.input), "input=%q", tt.input)
	}
}

func TestGenerateAlternateContentIDsDeep2(t *testing.T) {
	ids := generateContentIDVariations("abw001")
	assert.NotEmpty(t, ids)
	// Should generate IDs using the prefix lookup table for "abw" which has prefix "118"
	assert.Contains(t, ids, "118abw00001")
	assert.Contains(t, ids, "118abw001")
}

func TestGetPreferredStringDeep2(t *testing.T) {
	assert.Equal(t, "preferred", getPreferredString("preferred", "fallback"))
	assert.Equal(t, "fallback", getPreferredString("", "fallback"))
	assert.Equal(t, "", getPreferredString("", ""))
}

func TestSelectLocalizedStringDeep2(t *testing.T) {
	assert.Equal(t, "English", selectLocalizedString("en", "English", "日本語"))
	assert.Equal(t, "日本語", selectLocalizedString("ja", "English", "日本語"))
	assert.Equal(t, "English", selectLocalizedString("ja", "English", "")) // fallback
	assert.Equal(t, "日本語", selectLocalizedString("en", "", "日本語"))         // fallback
}

func TestContentIDMatchesExpectedDeep2(t *testing.T) {
	assert.True(t, contentIDCoreMatch("abw001", "abw001"))
	assert.False(t, contentIDCoreMatch("", "abw001"))
	assert.False(t, contentIDCoreMatch("abw001", ""))
}

func TestValidateScraperSettingsDeep2(t *testing.T) {
	assert.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: "en"}))
	assert.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: "ja"}))
	assert.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: ""}))
	assert.Error(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: "fr"}))
}

func TestCanHandleURLDeep2(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	assert.True(t, s.CanHandleURL("https://r18.dev/videos/vod/movies/detail/-/combined=abc123"))
	assert.True(t, s.CanHandleURL("https://www.r18.com/videos/abc"))
	assert.False(t, s.CanHandleURL("https://example.com"))
	assert.False(t, s.CanHandleURL("not-a-url"))
}

func TestExtractIDFromURLDeep2(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	id, err := s.ExtractIDFromURL("https://r18.dev/videos/vod/movies/detail/-/combined=abc123")
	assert.NoError(t, err)
	assert.Equal(t, "abc123", id)

	id, err = s.ExtractIDFromURL("https://r18.dev/videos/vod/movies/detail/-/id=xyz789")
	assert.NoError(t, err)
	assert.Equal(t, "xyz789", id)

	_, err = s.ExtractIDFromURL("https://r18.dev/no-id-here")
	assert.Error(t, err)
}

func TestResolveDownloadProxyForHostDeep2(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	_, _, ok := s.ResolveDownloadProxyForHost("r18.dev")
	assert.True(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("cdn.r18.dev")
	assert.True(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
}
