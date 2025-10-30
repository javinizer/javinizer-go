package dmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseHTML_EdgeCases tests edge cases in HTML parsing
func TestParseHTML_EdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tests := []struct {
		name      string
		sourceURL string
		html      string
	}{
		{
			name:      "old site with minimal data",
			sourceURL: "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test123/",
			html:      `<html><head></head><body><h1 id="title" class="item">Minimal Title</h1></body></html>`,
		},
		{
			name:      "new site with minimal data",
			sourceURL: "https://video.dmm.co.jp/av/content/?id=test123",
			html:      `<html><head><meta property="og:title" content="Minimal New Title"></head><body><h1>Minimal New Title</h1></body></html>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						Enabled:       true,
						ScrapeActress: true,
					},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(tt.html)
			require.NoError(t, err)

			result, err := scraper.parseHTML(doc, tt.sourceURL)
			require.NoError(t, err)
			assert.NotNil(t, result)
			assert.NotEmpty(t, result.Title)
			assert.Equal(t, "dmm", result.Source)
		})
	}
}

// TestExtractDescription_BothSites tests description extraction for both site formats
func TestExtractDescription_BothSites(t *testing.T) {
	tests := []struct {
		name       string
		html       string
		isNewSite  bool
		shouldFind bool
	}{
		{
			name:       "old site description",
			html:       `<html><body><div class="mg-b20 lh4"><p class="mg-b20">Old site description here.</p></div></body></html>`,
			isNewSite:  false,
			shouldFind: true,
		},
		{
			name:       "old site fallback",
			html:       `<html><body><div class="mg-b20 lh4">Fallback description without p tag.</div></body></html>`,
			isNewSite:  false,
			shouldFind: true,
		},
		{
			name:       "new site og:description",
			html:       `<html><head><meta property="og:description" content="New site OG description."></head><body></body></html>`,
			isNewSite:  true,
			shouldFind: true,
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

			result := scraper.extractDescription(doc, tt.isNewSite)

			if tt.shouldFind {
				assert.NotEmpty(t, result)
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

// TestExtractMaker_BothSites tests maker extraction for both site formats
func TestExtractMaker_BothSites(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		isNewSite bool
		expected  string
	}{
		{
			name:      "old site with ?maker=",
			html:      `<html><body><a href="?maker=123">Test Studio</a></body></html>`,
			isNewSite: false,
			expected:  "Test Studio",
		},
		{
			name:      "old site with /article=maker/id=",
			html:      `<html><body><a href="/article=maker/id=123">Test Studio 2</a></body></html>`,
			isNewSite: false,
			expected:  "Test Studio 2",
		},
		{
			name:      "new site",
			html:      `<html><body><table><tr><th>メーカー</th><td><a href="/maker/1">New Studio</a></td></tr></table></body></html>`,
			isNewSite: true,
			expected:  "New Studio",
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

			result := scraper.extractMaker(doc, tt.isNewSite)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractSeries_BothSites tests series extraction for both site formats
// Note: Old site series extraction uses complex regex that requires exact HTML structure
func TestExtractSeries_BothSites(t *testing.T) {
	t.Run("new site series", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				DMM: config.DMMConfig{Enabled: true},
			},
		}
		scraper := New(cfg, nil)

		html := `<html><body><table><tr><th>シリーズ</th><td><a href="/series/1">New Series</a></td></tr></table></body></html>`
		doc, err := parseHTMLString(html)
		require.NoError(t, err)

		result := scraper.extractSeries(doc, true)
		assert.Equal(t, "New Series", result)
	})

	t.Run("no series", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				DMM: config.DMMConfig{Enabled: true},
			},
		}
		scraper := New(cfg, nil)

		html := `<html><body></body></html>`
		doc, err := parseHTMLString(html)
		require.NoError(t, err)

		result := scraper.extractSeries(doc, false)
		assert.Equal(t, "", result)
	})
}

// TestExtractCoverURL_BothSites tests cover URL extraction for both site formats
func TestExtractCoverURL_BothSites(t *testing.T) {
	tests := []struct {
		name       string
		html       string
		isNewSite  bool
		shouldFind bool
	}{
		{
			name:       "old site mono/movie/adult",
			html:       `<html><body><img src="https://pics.dmm.co.jp/mono/movie/adult/test/testps.jpg" /></body></html>`,
			isNewSite:  false,
			shouldFind: true,
		},
		{
			name:       "old site digital/video",
			html:       `<html><body><img src="https://pics.dmm.co.jp/digital/video/test123/test123ps.jpg" /></body></html>`,
			isNewSite:  false,
			shouldFind: true,
		},
		{
			name:       "new site og:image",
			html:       `<html><head><meta property="og:image" content="https://awsimgsrc.dmm.co.jp/pics_dig/video/test/testps.jpg"></head><body></body></html>`,
			isNewSite:  true,
			shouldFind: true,
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

			result := scraper.extractCoverURL(doc, tt.isNewSite)

			if tt.shouldFind {
				assert.NotEmpty(t, result)
				assert.Contains(t, result, "pl.jpg") // Should be converted to large size
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

// TestExtractScreenshots_BothSites tests screenshot extraction for both site formats
func TestExtractScreenshots_BothSites(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		isNewSite     bool
		expectedCount int
	}{
		{
			name: "old site with data-lazy",
			html: `<html><body>
				<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/test/test-1.jpg" /></a>
				<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/test/test-2.jpg" /></a>
			</body></html>`,
			isNewSite:     false,
			expectedCount: 2,
		},
		{
			name: "new site with awsimgsrc",
			html: `<html><body>
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/test/test-1.jpg" />
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/test/test-2.jpg" />
			</body></html>`,
			isNewSite:     true,
			expectedCount: 2,
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

			result := scraper.extractScreenshots(doc, tt.isNewSite)
			assert.Len(t, result, tt.expectedCount)
		})
	}
}

// TestExtractRating_BothSites tests rating extraction for both site formats
func TestExtractRating_BothSites(t *testing.T) {
	t.Run("new site with JSON-LD", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				DMM: config.DMMConfig{Enabled: true},
			},
		}
		scraper := New(cfg, nil)

		html := `<html><head><script type="application/ld+json">{"aggregateRating":{"ratingValue":4.0,"ratingCount":150}}</script></head><body></body></html>`
		doc, err := parseHTMLString(html)
		require.NoError(t, err)

		rating := scraper.extractRating(doc, true)
		require.NotNil(t, rating)
		assert.Equal(t, 8.0, rating.Score) // 4.0 * 2 = 8.0
		assert.Equal(t, 150, rating.Votes)
	})

	t.Run("no rating", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				DMM: config.DMMConfig{Enabled: true},
			},
		}
		scraper := New(cfg, nil)

		html := `<html><body></body></html>`
		doc, err := parseHTMLString(html)
		require.NoError(t, err)

		rating := scraper.extractRating(doc, false)
		assert.Nil(t, rating)
	})
}
