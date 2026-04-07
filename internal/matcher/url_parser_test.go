package matcher

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockURLHandlerScraper is a test scraper that implements URLHandler
type mockURLHandlerScraper struct {
	name       string
	enabled    bool
	canHandle  bool
	extractID  string
	extractErr error
}

func (m *mockURLHandlerScraper) Name() string { return m.name }
func (m *mockURLHandlerScraper) Search(_ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *mockURLHandlerScraper) GetURL(_ string) (string, error) { return "", nil }
func (m *mockURLHandlerScraper) IsEnabled() bool                 { return m.enabled }
func (m *mockURLHandlerScraper) Config() *config.ScraperSettings { return nil }
func (m *mockURLHandlerScraper) Close() error                    { return nil }

// URLHandler implementation
func (m *mockURLHandlerScraper) CanHandleURL(_ string) bool {
	return m.canHandle
}

func (m *mockURLHandlerScraper) ExtractIDFromURL(_ string) (string, error) {
	if m.extractErr != nil {
		return "", m.extractErr
	}
	return m.extractID, nil
}

func TestParseInput(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedID    string
		expectedHint  string
		expectedIsURL bool
		expectError   bool
	}{
		{
			name:          "Plain JAV ID",
			input:         "IPX-535",
			expectedID:    "IPX-535",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:          "JAV ID with spaces",
			input:         "  IPX-535  ",
			expectedID:    "IPX-535",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:          "Lowercase JAV ID",
			input:         "ipx-535",
			expectedID:    "ipx-535",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:          "JAV ID without hyphen",
			input:         "IPX535",
			expectedID:    "IPX535",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:        "Empty input",
			input:       "",
			expectError: true,
		},
		{
			name:        "Whitespace only",
			input:       "   ",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parser is now agnostic - with nil registry, all inputs treated as plain IDs
			result, err := ParseInput(tt.input, nil)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedID, result.ID)
			assert.Equal(t, tt.expectedHint, result.ScraperHint)
			assert.Equal(t, tt.expectedIsURL, result.IsURL)
		})
	}
}

func TestParseInputWithRegistry(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		setupRegistry func() *models.ScraperRegistry
		expectedID    string
		expectedHint  string
		expectedIsURL bool
		expectError   bool
	}{
		{
			name:  "DMM scraper handles URL",
			input: "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			setupRegistry: func() *models.ScraperRegistry {
				reg := models.NewScraperRegistry()
				reg.Register(&mockURLHandlerScraper{
					name:      "dmm",
					enabled:   true,
					canHandle: true,
					extractID: "ipx00535",
				})
				return reg
			},
			expectedID:    "ipx00535",
			expectedHint:  "dmm",
			expectedIsURL: true,
		},
		{
			name:  "R18dev scraper handles URL",
			input: "https://r18.dev/videos/vod/movies/detail/-/id=ipx00535/",
			setupRegistry: func() *models.ScraperRegistry {
				reg := models.NewScraperRegistry()
				reg.Register(&mockURLHandlerScraper{
					name:      "r18dev",
					enabled:   true,
					canHandle: true,
					extractID: "ipx00535",
				})
				return reg
			},
			expectedID:    "ipx00535",
			expectedHint:  "r18dev",
			expectedIsURL: true,
		},
		{
			name:  "LibreDMM scraper handles URL",
			input: "https://www.libredmm.com/movies/IPX-535",
			setupRegistry: func() *models.ScraperRegistry {
				reg := models.NewScraperRegistry()
				reg.Register(&mockURLHandlerScraper{
					name:      "libredmm",
					enabled:   true,
					canHandle: true,
					extractID: "IPX-535",
				})
				return reg
			},
			expectedID:    "IPX-535",
			expectedHint:  "libredmm",
			expectedIsURL: true,
		},
		{
			name:  "Registry scraper not enabled - falls through to plain ID",
			input: "https://javbus.com/IPX-535",
			setupRegistry: func() *models.ScraperRegistry {
				reg := models.NewScraperRegistry()
				reg.Register(&mockURLHandlerScraper{
					name:      "javbus",
					enabled:   false,
					canHandle: true,
					extractID: "IPX-535",
				})
				return reg
			},
			expectedID:    "https://javbus.com/IPX-535", // Not a handled URL, treated as raw ID
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:  "Registry scraper cannot handle URL - treats as plain ID",
			input: "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			setupRegistry: func() *models.ScraperRegistry {
				reg := models.NewScraperRegistry()
				reg.Register(&mockURLHandlerScraper{
					name:      "javbus",
					enabled:   true,
					canHandle: false,
				})
				return reg
			},
			expectedID:    "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:  "Multiple scrapers - first match wins",
			input: "https://javbus.com/IPX-535",
			setupRegistry: func() *models.ScraperRegistry {
				reg := models.NewScraperRegistry()
				reg.Register(&mockURLHandlerScraper{
					name:      "javbus",
					enabled:   true,
					canHandle: true,
					extractID: "IPX-535",
				})
				reg.Register(&mockURLHandlerScraper{
					name:      "javdb",
					enabled:   true,
					canHandle: true,
					extractID: "IPX-535-DB",
				})
				return reg
			},
			expectedID:    "IPX-535", // javbus registered first alphabetically
			expectedHint:  "javbus",
			expectedIsURL: true,
		},
		{
			name:  "Empty registry treats all input as plain ID",
			input: "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			setupRegistry: func() *models.ScraperRegistry {
				return models.NewScraperRegistry()
			},
			expectedID:    "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			expectedHint:  "",
			expectedIsURL: false,
		},
		{
			name:  "Nil registry treats all input as plain ID",
			input: "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			setupRegistry: func() *models.ScraperRegistry {
				return nil
			},
			expectedID:    "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			expectedHint:  "",
			expectedIsURL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := tt.setupRegistry()
			result, err := ParseInput(tt.input, registry)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedID, result.ID)
			assert.Equal(t, tt.expectedHint, result.ScraperHint)
			assert.Equal(t, tt.expectedIsURL, result.IsURL)
		})
	}
}

func TestReorderWithPriority(t *testing.T) {
	tests := []struct {
		name     string
		scrapers []string
		priority string
		expected []string
	}{
		{
			name:     "priority at start",
			scrapers: []string{"r18dev", "dmm", "javlibrary"},
			priority: "r18dev",
			expected: []string{"r18dev", "dmm", "javlibrary"},
		},
		{
			name:     "priority in middle",
			scrapers: []string{"r18dev", "dmm", "javlibrary"},
			priority: "dmm",
			expected: []string{"dmm", "r18dev", "javlibrary"},
		},
		{
			name:     "priority at end",
			scrapers: []string{"r18dev", "dmm", "javlibrary"},
			priority: "javlibrary",
			expected: []string{"javlibrary", "r18dev", "dmm"},
		},
		{
			name:     "priority not in list",
			scrapers: []string{"r18dev", "dmm"},
			priority: "javlibrary",
			expected: []string{"javlibrary", "r18dev", "dmm"},
		},
		{
			name:     "empty scrapers list",
			scrapers: []string{},
			priority: "r18dev",
			expected: []string{"r18dev"},
		},
		{
			name:     "single scraper - same as priority",
			scrapers: []string{"r18dev"},
			priority: "r18dev",
			expected: []string{"r18dev"},
		},
		{
			name:     "empty priority",
			scrapers: []string{"r18dev", "dmm"},
			priority: "",
			expected: []string{"r18dev", "dmm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReorderWithPriority(tt.scrapers, tt.priority)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateOptimalScrapers(t *testing.T) {
	tests := []struct {
		name            string
		requestScrapers []string
		configPriority  []string
		parsed          *ParsedInput
		expected        []string
	}{
		{
			name:            "no URL - uses config priority",
			requestScrapers: []string{},
			configPriority:  []string{"dmm", "r18dev", "javlibrary"},
			parsed: &ParsedInput{
				ID:                 "IPX-123",
				IsURL:              false,
				CompatibleScrapers: nil,
				ScraperHint:        "",
			},
			expected: []string{"dmm", "r18dev", "javlibrary"},
		},
		{
			name:            "no URL - user override",
			requestScrapers: []string{"javdb", "javbus"},
			configPriority:  []string{"dmm", "r18dev"},
			parsed: &ParsedInput{
				ID:                 "IPX-123",
				IsURL:              false,
				CompatibleScrapers: nil,
				ScraperHint:        "",
			},
			expected: []string{"javdb", "javbus"},
		},
		{
			name:            "URL - filters to compatible and prioritizes hint",
			requestScrapers: []string{},
			configPriority:  []string{"dmm", "r18dev", "javlibrary", "javdb"},
			parsed: &ParsedInput{
				ID:                 "kitaike429",
				IsURL:              true,
				CompatibleScrapers: []string{"dmm", "javdb"},
				ScraperHint:        "dmm",
			},
			expected: []string{"dmm", "javdb"},
		},
		{
			name:            "URL - user selection filtered to compatible",
			requestScrapers: []string{"dmm", "r18dev", "javlibrary"},
			configPriority:  []string{"dmm", "javdb"},
			parsed: &ParsedInput{
				ID:                 "kitaike429",
				IsURL:              true,
				CompatibleScrapers: []string{"dmm"},
				ScraperHint:        "dmm",
			},
			expected: []string{"dmm"},
		},
		{
			name:            "URL - no compatible scrapers falls back to original",
			requestScrapers: []string{"javlibrary"},
			configPriority:  []string{"dmm", "r18dev"},
			parsed: &ParsedInput{
				ID:                 "unknown123",
				IsURL:              true,
				CompatibleScrapers: []string{}, // Empty - no compatible scrapers
				ScraperHint:        "",
			},
			expected: []string{"javlibrary"},
		},
		{
			name:            "URL - compatible scrapers empty and no user selection uses config",
			requestScrapers: []string{},
			configPriority:  []string{"dmm", "r18dev", "javlibrary"},
			parsed: &ParsedInput{
				ID:                 "unknown123",
				IsURL:              true,
				CompatibleScrapers: []string{}, // Empty - no compatible scrapers
				ScraperHint:        "",
			},
			expected: []string{"dmm", "r18dev", "javlibrary"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateOptimalScrapers(tt.requestScrapers, tt.configPriority, tt.parsed)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateOptimalScrapersWithNilParsed(t *testing.T) {
	// Test when parsed is nil (no manual input parsing)
	result := CalculateOptimalScrapers(
		[]string{"javdb", "javbus"},
		[]string{"dmm", "r18dev"},
		nil,
	)
	assert.Equal(t, []string{"javdb", "javbus"}, result)

	// Test when parsed is nil and no user selection
	result = CalculateOptimalScrapers(
		[]string{},
		[]string{"dmm", "r18dev"},
		nil,
	)
	assert.Equal(t, []string{"dmm", "r18dev"}, result)
}
