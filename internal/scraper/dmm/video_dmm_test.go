package dmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractDescriptionNewSite verifies description extraction from video.dmm.co.jp
func TestExtractDescriptionNewSite(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		expected    string
		shouldEmpty bool
	}{
		{
			name:     "from JSON-LD",
			html:     `<html><head><script type="application/ld+json">{"description":"This is the JSON-LD description text."}</script></head><body></body></html>`,
			expected: "This is the JSON-LD description text.",
		},
		{
			name:     "from og:description",
			html:     `<html><head><meta property="og:description" content="This is the OG description."></head><body></body></html>`,
			expected: "This is the OG description.",
		},
		{
			name:     "from meta description",
			html:     `<html><head><meta name="description" content="This is the meta description."></head><body></body></html>`,
			expected: "This is the meta description.",
		},
		{
			name:        "no description",
			html:        `<html><head></head><body>No description here</body></html>`,
			shouldEmpty: true,
		},
		{
			name:     "JSON-LD with escaped characters",
			html:     `<html><head><script type="application/ld+json">{"description":"Description with \"quotes\" and newlines."}</script></head><body></body></html>`,
			expected: "Description with \"quotes\" and newlines.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{Enabled: true},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(tt.html)
			require.NoError(t, err)

			result := scraper.extractDescriptionNewSite(doc)

			if tt.shouldEmpty {
				assert.Empty(t, result)
			} else {
				assert.Contains(t, result, tt.expected)
			}
		})
	}
}

// TestExtractCoverURLNewSite verifies cover URL extraction from video.dmm.co.jp
func TestExtractCoverURLNewSite(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "from og:image",
			html:     `<html><head><meta property="og:image" content="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535ps.jpg"></head><body></body></html>`,
			expected: "https://pics.dmm.co.jp/video/ipx00535/ipx00535pl.jpg",
		},
		{
			name:     "from og:image with query params",
			html:     `<html><head><meta property="og:image" content="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535ps.jpg?size=large"></head><body></body></html>`,
			expected: "https://pics.dmm.co.jp/video/ipx00535/ipx00535pl.jpg",
		},
		{
			name:     "from img tag",
			html:     `<html><head></head><body><img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535pl.jpg?v=1" /></body></html>`,
			expected: "https://pics.dmm.co.jp/video/ipx00535/ipx00535pl.jpg",
		},
		{
			name:     "no cover found",
			html:     `<html><head></head><body></body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{Enabled: true},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(tt.html)
			require.NoError(t, err)

			result := scraper.extractCoverURLNewSite(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractScreenshotsNewSite verifies screenshot extraction from video.dmm.co.jp
func TestExtractScreenshotsNewSite(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		expectedCount int
	}{
		{
			name: "multiple screenshots",
			html: `<html><body>
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-1.jpg" />
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-2.jpg?v=1" />
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-3.jpg" />
			</body></html>`,
			expectedCount: 3,
		},
		{
			name: "with cover image (should skip pl.jpg)",
			html: `<html><body>
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535pl.jpg" />
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-1.jpg" />
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-2.jpg" />
			</body></html>`,
			expectedCount: 2,
		},
		{
			name: "deduplicate screenshots",
			html: `<html><body>
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-1.jpg" />
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-1.jpg?v=1" />
			</body></html>`,
			expectedCount: 1,
		},
		{
			name:          "no screenshots",
			html:          `<html><body></body></html>`,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{Enabled: true},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(tt.html)
			require.NoError(t, err)

			result := scraper.extractScreenshotsNewSite(doc)
			assert.Len(t, result, tt.expectedCount)

			// Verify all screenshots are converted to pics.dmm.co.jp
			for _, url := range result {
				assert.Contains(t, url, "pics.dmm.co.jp")
				assert.NotContains(t, url, "?") // Query params removed
			}
		})
	}
}

// TestExtractSeriesNewSite verifies series extraction from video.dmm.co.jp
func TestExtractSeriesNewSite(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "with series",
			html:     `<html><body><table><tr><th>シリーズ</th><td><a href="/series/1">Test Series</a></td></tr></table></body></html>`,
			expected: "Test Series",
		},
		{
			name:     "no series",
			html:     `<html><body><table><tr><th>メーカー</th><td><a href="/maker/1">Test Studio</a></td></tr></table></body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{Enabled: true},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(tt.html)
			require.NoError(t, err)

			result := scraper.extractSeriesNewSite(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractMakerNewSite verifies maker extraction from video.dmm.co.jp
func TestExtractMakerNewSite(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "with maker",
			html:     `<html><body><table><tr><th>メーカー</th><td><a href="/maker/1">Test Studio</a></td></tr></table></body></html>`,
			expected: "Test Studio",
		},
		{
			name:     "no maker",
			html:     `<html><body><table><tr><th>シリーズ</th><td><a href="/series/1">Test Series</a></td></tr></table></body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{Enabled: true},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(tt.html)
			require.NoError(t, err)

			result := scraper.extractMakerNewSite(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractRatingNewSite verifies rating extraction from video.dmm.co.jp
func TestExtractRatingNewSite(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		expectedScore float64
		expectedVotes int
	}{
		{
			name:          "with rating and votes",
			html:          `<html><head><script type="application/ld+json">{"aggregateRating":{"ratingValue":4.5,"ratingCount":200}}</script></head><body></body></html>`,
			expectedScore: 9.0, // 4.5 * 2 = 9.0
			expectedVotes: 200,
		},
		{
			name:          "with rating only",
			html:          `<html><head><script type="application/ld+json">{"aggregateRating":{"ratingValue":3.5}}</script></head><body></body></html>`,
			expectedScore: 7.0, // 3.5 * 2 = 7.0
			expectedVotes: 0,
		},
		{
			name:          "no rating",
			html:          `<html><head></head><body></body></html>`,
			expectedScore: 0.0,
			expectedVotes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{Enabled: true},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(tt.html)
			require.NoError(t, err)

			rating, votes := scraper.extractRatingNewSite(doc)
			assert.Equal(t, tt.expectedScore, rating)
			assert.Equal(t, tt.expectedVotes, votes)
		})
	}
}
