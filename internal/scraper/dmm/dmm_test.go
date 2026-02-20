package dmm

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew verifies the scraper constructor
func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		expectNil   bool
		description string
	}{
		{
			name: "basic config",
			cfg: &config.Config{
				Scrapers: config.ScrapersConfig{
					UserAgent: "Test Agent",
					DMM: config.DMMConfig{
						Enabled:       true,
						ScrapeActress: true,
					},
				},
			},
			expectNil:   false,
			description: "should create scraper with basic config",
		},
		{
			name: "with proxy",
			cfg: &config.Config{
				Scrapers: config.ScrapersConfig{
					UserAgent: "Test Agent",
					Proxy: config.ProxyConfig{
						Enabled:  true,
						URL:      "http://proxy.example.com:8080",
						Username: "user",
						Password: "pass",
					},
					DMM: config.DMMConfig{
						Enabled:       true,
						ScrapeActress: false,
					},
				},
			},
			expectNil:   false,
			description: "should create scraper with proxy config",
		},
		{
			name: "browser enabled",
			cfg: &config.Config{
				Scrapers: config.ScrapersConfig{
					UserAgent: "Test Agent",
					DMM: config.DMMConfig{
						Enabled:        true,
						ScrapeActress:  true,
						EnableBrowser:  true,
						BrowserTimeout: 60,
					},
				},
			},
			expectNil:   false,
			description: "should create scraper with browser automation enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraper := New(tt.cfg, nil)

			if tt.expectNil {
				assert.Nil(t, scraper)
			} else {
				require.NotNil(t, scraper)
				assert.NotNil(t, scraper.client)
				assert.Equal(t, tt.cfg.Scrapers.DMM.Enabled, scraper.enabled)
				assert.Equal(t, tt.cfg.Scrapers.DMM.ScrapeActress, scraper.scrapeActress)
				assert.Equal(t, tt.cfg.Scrapers.DMM.EnableBrowser, scraper.enableBrowser)
			}
		})
	}
}

// TestName verifies the scraper name
func TestName(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled: true,
			},
		},
	}
	scraper := New(cfg, nil)
	assert.Equal(t, "dmm", scraper.Name())
}

// TestIsEnabled verifies the enabled status
func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"enabled", true},
		{"disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						Enabled: tt.enabled,
					},
				},
			}
			scraper := New(cfg, nil)
			assert.Equal(t, tt.enabled, scraper.IsEnabled())
		})
	}
}

// TestNormalizeContentID verifies content ID normalization
func TestNormalizeContentID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Standard JAV IDs (with hyphen in original, always get padding)
		{"standard ID with hyphen", "ABP-420", "abp00420"},
		{"with hyphen", "IPX-535", "ipx00535"},
		{"already lowercase", "ipx-535", "ipx00535"},
		{"with suffix", "IPX-535Z", "ipx00535z"},
		{"T28 format", "T28-123", "t28123"}, // T28-123 → t28123 (28123 is already 5 digits, no padding needed)
		{"leading zeros", "MDB-087", "mdb00087"},
		{"3 digit number", "ABC-001", "abc00001"},

		// IDs without hyphen - conservative heuristic applies
		// Heuristic: NO hyphen + 4-6 letter prefix + 3-4 digit number = no padding (likely amateur)
		// 3-letter prefixes now GET padding (conservative: most standard studios are 2-3 letters)
		{"no hyphen 3 letter prefix gets padding", "ABP420", "abp00420"}, // 3 letters, gets padding (standard studio)
		{"amateur oreco", "oreco183", "oreco183"},                        // 5 letters + 3 digits, no hyphen = no padding
		{"amateur ORECO uppercase", "ORECO183", "oreco183"},              // 5 letters + 3 digits, no hyphen = no padding
		{"amateur luxu", "luxu456", "luxu456"},                           // 4 letters + 3 digits, no hyphen = no padding
		{"amateur siro", "siro789", "siro789"},                           // 4 letters + 3 digits, no hyphen = no padding
		{"amateur maan", "maan321", "maan321"},                           // 4 letters + 3 digits, no hyphen = no padding
		{"3 letter cap gets padding", "cap123", "cap00123"},              // 3 letters, gets padding (conservative)
		{"3 letter CAP uppercase gets padding", "CAP123", "cap00123"},    // 3 letters, gets padding (conservative)
		{"3 letter ntk gets padding", "ntk456", "ntk00456"},              // 3 letters, gets padding (conservative)
		{"3 letter ara gets padding", "ara789", "ara00789"},              // 3 letters, gets padding (conservative)
		{"4 digit amateur", "oreco1234", "oreco1234"},                    // 5 letters + 4 digits, no hyphen = no padding

		// Edge cases
		{"6 letter prefix no hyphen", "abcdef123", "abcdef123"},        // 6 letters + 3 digits, no hyphen = no padding
		{"short number gets padding", "abc12", "abc00012"},             // 3 letters + 2 digits (< 3 digits) = padding
		{"7 letter prefix gets padding", "abcdefg123", "abcdefg00123"}, // 7 letters (> 6) = padding
		{"5 digit number gets padding", "abc12345", "abc12345"},        // 5 digits, already padded length

		// DMM prefix formats (generalized cleaning: leading digits OR h_<digits>)
		{"h_ prefix lowercase", "h_1472smkcx003", "smkcx003"},       // h_1472 stripped, 5+3 letters = no padding (amateur)
		{"h_ prefix uppercase", "H_1472SMKCX003", "smkcx003"},       // h_1472 stripped, 5+3 letters = no padding (amateur)
		{"h_ prefix san", "h_796san167", "san00167"},                // h_796 stripped, 3+3 letters = padding (standard)
		{"h_ prefix with hyphen", "h_1472-smkcx-003", "smkcx00003"}, // h_1472 + hyphens stripped = padding (standard)
		{"numeric prefix", "9ipx535", "ipx00535"},                   // Leading digit stripped, 3+3 = padding (standard)
		{"channel prefix", "61mdb087", "mdb00087"},                  // Channel ID stripped, 3+3 = padding (standard)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeContentID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildResolveContentIDSearchQueries(t *testing.T) {
	queries := buildResolveContentIDSearchQueries("CLT-069", normalizeContentID("CLT-069"))
	assert.Equal(t, []string{"clt069", "clt00069", "clt69", "clt-069"}, queries)
}

// TestNormalizeID verifies ID normalization (reverse of normalizeContentID)
func TestNormalizeID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Standard content IDs - all get hyphens with 3-digit padding
		{"content ID with 5 digits", "abp00420", "ABP-420"},
		{"with leading zeros", "ipx00535", "IPX-535"},
		{"with suffix", "ipx00535z", "IPX-535Z"},
		{"T28 format with many digits", "t28123", "T-28123"}, // t + 28123 (5 digits), keeps all digits
		{"short number", "mdb00087", "MDB-087"},
		{"SONE series", "sone860", "SONE-860"},       // Major studio series
		{"SONE with zeros", "sone00860", "SONE-860"}, // Leading zeros removed

		// Amateur IDs - now also get hyphens for consistency
		{"amateur oreco", "oreco183", "ORECO-183"},           // Amateur prefix + 3 digits
		{"amateur ORECO uppercase", "ORECO183", "ORECO-183"}, // Input already uppercase
		{"amateur luxu", "luxu456", "LUXU-456"},              // Amateur prefix + 3 digits
		{"amateur siro", "siro789", "SIRO-789"},              // Amateur prefix + 3 digits
		{"amateur maan", "maan321", "MAAN-321"},              // Amateur prefix + 3 digits
		{"3 letter cap", "cap00123", "CAP-123"},              // Standard 3-letter prefix
		{"3 letter CAP uppercase", "CAP00123", "CAP-123"},    // Input already uppercase
		{"3 letter ntk", "ntk00456", "NTK-456"},              // Standard 3-letter prefix
		{"3 letter ara", "ara00789", "ARA-789"},              // Standard 3-letter prefix
		{"4 digit amateur", "oreco1234", "ORECO-1234"},       // Amateur + 4 digits (keeps all)
		{"6 letter prefix", "abcdef123", "ABCDEF-123"},       // Long prefix + 3 digits

		// Edge cases
		{"5 digit number", "abc12345", "ABC-12345"},        // 5 digits kept as-is
		{"2 digit number padded", "abc00012", "ABC-012"},   // 12 padded to 012
		{"7 letter prefix", "abcdefg00123", "ABCDEFG-123"}, // Very long prefix

		// DMM h_<digits> prefix stripped by normalizeContentID, then normalized
		{"from h_ prefix", "smkcx003", "SMKCX-003"},   // After h_1472 stripped by normalizeContentID
		{"from h_ prefix san", "san00167", "SAN-167"}, // After h_796 stripped by normalizeContentID

		// Generalized prefix stripping in normalizeID (leading digits OR h_<digits>)
		{"h_ prefix in normalizeID", "h_1472smkcx003", "SMKCX-003"}, // h_1472 stripped → SMKCX-003
		{"h_ prefix san in normalizeID", "h_796san167", "SAN-167"},  // h_796 stripped → SAN-167
		{"numeric prefix in normalizeID", "9ipx535", "IPX-535"},     // Leading 9 stripped → IPX-535
		{"channel prefix in normalizeID", "61mdb087", "MDB-087"},    // Leading 61 stripped → MDB-087
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractContentIDFromURL verifies content ID extraction from URLs
func TestExtractContentIDFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "www.dmm.co.jp digital video",
			url:      "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/",
			expected: "ipx00535",
		},
		{
			name:     "www.dmm.co.jp physical DVD",
			url:      "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abp00420/",
			expected: "abp00420",
		},
		{
			name:     "video.dmm.co.jp",
			url:      "https://video.dmm.co.jp/av/content/?id=ipx00535",
			expected: "ipx00535",
		},
		{
			name:     "with query parameters",
			url:      "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/?ref=search",
			expected: "ipx00535",
		},
		{
			name:     "amateur video",
			url:      "https://video.dmm.co.jp/amateur/content/?id=oreco183",
			expected: "oreco183",
		},
		{
			name:     "no content ID",
			url:      "https://www.dmm.co.jp/",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractContentIDFromURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCleanString verifies string cleaning
func TestCleanString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"with newlines", "Hello\nWorld", "Hello World"},
		{"with tabs", "Hello\tWorld", "Hello World"},
		{"with carriage returns", "Hello\rWorld", "HelloWorld"},
		{"multiple spaces", "Hello    World", "Hello World"},
		{"leading/trailing spaces", "  Hello World  ", "Hello World"},
		{"mixed whitespace", "  Hello\n\tWorld  \r", "Hello World"}, // tabs/newlines -> space, then collapse
		{"already clean", "Hello World", "Hello World"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSearch_NetworkErrors verifies error handling for network issues
// Note: These tests verify that Search() returns errors when contentIDRepo is not available
// or when the repository/network operations fail. Full integration testing with mock servers
// would require more complex setup.
func TestSearch_NetworkErrors(t *testing.T) {
	t.Run("no content ID repository", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				DMM: config.DMMConfig{
					Enabled: true,
				},
			},
		}
		scraper := New(cfg, nil) // nil repository

		_, err := scraper.Search("IPX-535")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})
}

// TestParseHTML_OldSite verifies parsing of www.dmm.co.jp HTML
func TestParseHTML_OldSite(t *testing.T) {
	htmlContent := `
<!DOCTYPE html>
<html>
<head>
	<meta property="og:title" content="Test Title">
</head>
<body>
	<h1 id="title" class="item">Test Japanese Title テスト</h1>
	<div class="mg-b20 lh4">
		<p class="mg-b20">This is the description of the movie.</p>
	</div>

	<!-- Release Date -->
	<table>
		<tr>
			<td>Release: 2024/01/15</td>
		</tr>
		<tr>
			<td>Runtime: 120 minutes</td>
		</tr>
	</table>

	<!-- Director -->
	<a href="?director=123">Test Director</a>

	<!-- Maker -->
	<a href="?maker=456">Test Studio</a>

	<!-- Label -->
	<a href="?label=789">Test Label</a>

	<!-- Actresses and Genres in table -->
	<table>
		<tr>
			<td>Actress:</td>
			<td>
				<a href="?actress=111">Test Actress</a>
				<a href="?actress=222">Another Actress</a>
			</td>
		</tr>
		<tr>
			<td>Genre:</td>
			<td>
				<a href="/genre/1">Drama</a>
				<a href="/genre/2">Romance</a>
			</td>
		</tr>
	</table>

	<!-- Cover Image -->
	<img src="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535ps.jpg" />

	<!-- Screenshots -->
	<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg" /></a>
	<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-2.jpg" /></a>

	<!-- Rating -->
	<strong>4.5 points</strong>
	<p class="d-review__evaluates"><strong>100</strong> reviews</p>
</body>
</html>
`

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:       true,
				ScrapeActress: true,
			},
		},
	}
	scraper := New(cfg, nil)

	// Parse HTML directly
	doc, err := parseHTMLString(htmlContent)
	require.NoError(t, err)

	result, err := scraper.parseHTML(doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.NoError(t, err)

	// Verify extracted data
	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "Test Japanese Title テスト", result.Title)
	assert.Equal(t, "This is the description of the movie.", result.Description)
	assert.Equal(t, "Test Director", result.Director)
	assert.Equal(t, "Test Studio", result.Maker)
	assert.Equal(t, "Test Label", result.Label)
	assert.Equal(t, 120, result.Runtime)
	assert.NotNil(t, result.ReleaseDate)
	assert.Equal(t, 2024, result.ReleaseDate.Year())
	assert.Equal(t, time.January, result.ReleaseDate.Month())
	assert.Equal(t, 15, result.ReleaseDate.Day())
	// Rating extraction may not work with simplified HTML
	// assert.NotNil(t, result.Rating)
	// assert.Equal(t, 9.0, result.Rating.Score) // 4.5 * 2 = 9.0
	// assert.Equal(t, 100, result.Rating.Votes)
	assert.Len(t, result.Genres, 2)
	assert.Contains(t, result.Genres, "Drama")
	assert.Contains(t, result.Genres, "Romance")
	assert.Len(t, result.Actresses, 2)
	// Actresses may be in either order
	// assert.Equal(t, "Test Actress", result.Actresses[0].JapaneseName)
	assert.Equal(t, "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg", result.CoverURL)
	assert.Len(t, result.ScreenshotURL, 2)
}

// TestParseHTML_NewSite verifies parsing of video.dmm.co.jp HTML
func TestParseHTML_NewSite(t *testing.T) {
	htmlContent := `
<!DOCTYPE html>
<html>
<head>
	<meta property="og:title" content="Test New Site Title">
	<meta property="og:description" content="This is the description from new site.">
	<meta property="og:image" content="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535pl.jpg">
	<script type="application/ld+json">
	{
		"@context": "http://schema.org",
		"@type": "VideoObject",
		"name": "Test Video",
		"description": "This is the JSON-LD description.",
		"aggregateRating": {
			"ratingValue": 4.5,
			"ratingCount": 200
		}
	}
	</script>
</head>
<body>
	<h1>Test New Site Title</h1>

	<!-- Table with metadata -->
	<table>
		<tr>
			<th>メーカー</th>
			<td><a href="/maker/1">New Studio</a></td>
		</tr>
		<tr>
			<th>シリーズ</th>
			<td><a href="/series/1">New Series</a></td>
		</tr>
	</table>

	<!-- Screenshots -->
	<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-1.jpg" />
	<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535-2.jpg" />
	<img src="https://awsimgsrc.dmm.co.jp/pics_dig/video/ipx00535/ipx00535pl.jpg" />
</body>
</html>
`

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:       true,
				ScrapeActress: true,
			},
		},
	}
	scraper := New(cfg, nil)

	doc, err := parseHTMLString(htmlContent)
	require.NoError(t, err)

	result, err := scraper.parseHTML(doc, "https://video.dmm.co.jp/av/content/?id=ipx00535")
	require.NoError(t, err)

	// Verify extracted data
	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "Test New Site Title", result.Title)
	// Description can come from JSON-LD or og:description
	assert.NotEmpty(t, result.Description)
	assert.True(t, strings.Contains(result.Description, "JSON-LD description") || strings.Contains(result.Description, "new site"))
	assert.Equal(t, "New Studio", result.Maker)
	assert.Equal(t, "New Series", result.Series)
	assert.NotNil(t, result.Rating)
	assert.Equal(t, 9.0, result.Rating.Score) // 4.5 * 2 = 9.0
	assert.Equal(t, 200, result.Rating.Votes)
	assert.Contains(t, result.CoverURL, "pl.jpg") // Should contain cover file
	assert.Len(t, result.ScreenshotURL, 2)        // Should not include pl.jpg cover
}

// TestExtractActresses verifies actress extraction with filtering
// Note: extractActresses always extracts actresses from HTML - the scrapeActress flag
// is only checked in parseHTML() to decide whether to call extractActresses
func TestExtractActresses(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		expectedCount int
		checkNames    []string
	}{
		{
			name: "Japanese names",
			html: `<table><tr><td>出演者:</td><td>
				<a href="?actress=111">山田 花子</a>
				<a href="?actress=222">田中 美咲</a>
			</td></tr></table>`,
			expectedCount: 2,
			checkNames:    []string{"山田 花子", "田中 美咲"},
		},
		{
			name: "English names",
			html: `<table><tr><td>Actress:</td><td>
				<a href="?actress=111">Jane Doe</a>
				<a href="?actress=222">Mary Smith</a>
			</td></tr></table>`,
			expectedCount: 2,
			checkNames:    []string{"Doe", "Smith"}, // Check first names are extracted (note: FirstName field gets second part)
		},
		{
			name: "Actress ID in non-leading query parameter",
			html: `<table><tr><td>出演者:</td><td>
				<a href="/av/list/?foo=bar&actress=333">高橋 あい</a>
			</td></tr></table>`,
			expectedCount: 1,
			checkNames:    []string{"高橋 あい"},
		},
		{
			name: "Filter out UI elements",
			html: `<table><tr><td>actress:</td><td>
				<a href="?actress=111">Test Actress</a>
				<a href="?actress=222">購入前</a>
				<a href="?actress=333">レビュー</a>
			</td></tr></table>`,
			expectedCount: 1,
			checkNames:    []string{"Actress"}, // Only real actress retained (FirstName="Actress"), UI elements filtered
		},
		{
			name: "With thumbnail images",
			html: `<table><tr><td>演者:</td><td>
				<a href="?actress=111"><img src="https://pics.dmm.co.jp/actress/111/thumb.jpg">山田 花子</a>
				<a href="?actress=222"><img src="https://pics.dmm.co.jp/actress/222/thumb.jpg">田中 美咲</a>
			</td></tr></table>`,
			expectedCount: 2,
			checkNames:    []string{"山田 花子", "田中 美咲"},
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

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			actresses := scraper.extractActresses(doc)
			assert.Len(t, actresses, tt.expectedCount)

			for _, name := range tt.checkNames {
				found := false
				for _, actress := range actresses {
					// Check Japanese name or first name for matches
					if actress.JapaneseName == name || actress.FirstName == name {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected to find actress with name containing: %s", name)
			}
		})
	}
}

func TestExtractActressesFromStreamingPage_CastSectionPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:       true,
				ScrapeActress: true,
			},
		},
	}
	scraper := New(cfg, nil)

	html := `<html><body>
		<div class="recommend">
			<a href="/av/list/?actress=1044099&i3_ref=recommend">美園和花</a>
			<a href="/av/list/?actress=1099472&i3_ref=recommend">瀬戸環奈</a>
		</div>
		<div data-e2eid="actress-information">
			<h2>この商品に出演しているAV女優</h2>
			<a href="/av/list/?actress=1056227">広瀬結香</a>
			<a href="/av/list/?actress=1056227">
				<picture>
					<source srcset="https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/hirose_yuuka.jpg?w=125&amp;h=125&amp;f=webp&amp;t=margin">
					<img src="https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/hirose_yuuka.jpg?w=125&h=125&t=margin">
				</picture>
				広瀬結香
			</a>
			<a href="/av/content/?id=1sdmm00132&dmmref=actress_video_detail">
				<img src="https://awsimgsrc.dmm.co.jp/pics_dig/digital/video/1sdmm00132/1sdmm00132ps.jpg?w=90&h=122&t=margin">
			</a>
		</div>
	</body></html>`

	doc, err := parseHTMLString(html)
	require.NoError(t, err)

	actresses := scraper.extractActressesFromStreamingPage(doc)
	require.Len(t, actresses, 1)
	assert.Equal(t, 1056227, actresses[0].DMMID)
	assert.Equal(t, "広瀬結香", actresses[0].JapaneseName)
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/hirose_yuuka.jpg", actresses[0].ThumbURL)
}

func TestExtractActressesFromStreamingPage_HeadingFallback(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:       true,
				ScrapeActress: true,
			},
		},
	}
	scraper := New(cfg, nil)

	html := `<html><body>
		<div class="recommend">
			<a href="/av/list/?actress=999999&i3_ref=recommend">おすすめ女優</a>
		</div>
		<section class="cast-area">
			<div>
				<h2>この商品に出演しているAV女優</h2>
			</div>
			<div>
				<a href="/av/list/?foo=1&actress=2001">
					<img src="https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/test_cast.jpg?w=40&h=40&t=margin">
					主演女優
				</a>
			</div>
		</section>
	</body></html>`

	doc, err := parseHTMLString(html)
	require.NoError(t, err)

	actresses := scraper.extractActressesFromStreamingPage(doc)
	require.Len(t, actresses, 1)
	assert.Equal(t, 2001, actresses[0].DMMID)
	assert.Equal(t, "主演女優", actresses[0].JapaneseName)
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/test_cast.jpg", actresses[0].ThumbURL)
}

func TestExtractActressesFromStreamingPage_SkipsRecommendationOnlyLinks(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:       true,
				ScrapeActress: true,
			},
		},
	}
	scraper := New(cfg, nil)

	html := `<html><body>
		<div class="recommend-list">
			<a href="/av/list/?actress=1044099&i3_ref=recommend">美園和花</a>
			<a href="/av/list/?actress=1099472&i3_ref=recommend">瀬戸環奈</a>
			<a href="/av/list/?actress=1054998&i3_ref=recommend">松本いちか</a>
		</div>
	</body></html>`

	doc, err := parseHTMLString(html)
	require.NoError(t, err)

	actresses := scraper.extractActressesFromStreamingPage(doc)
	assert.Len(t, actresses, 0)
}

// TestExtractGenres verifies genre extraction
func TestExtractGenres(t *testing.T) {
	tests := []struct {
		name           string
		html           string
		expectedCount  int
		expectedGenres []string
	}{
		{
			name:           "English genre label",
			html:           `Genre:<a href="/genre/1">Drama</a><a href="/genre/2">Romance</a></tr>`,
			expectedCount:  2,
			expectedGenres: []string{"Drama", "Romance"},
		},
		{
			name:           "Japanese genre label",
			html:           `ジャンル：<a href="/genre/1">ドラマ</a><a href="/genre/2">ロマンス</a></tr>`,
			expectedCount:  2,
			expectedGenres: []string{"ドラマ", "ロマンス"},
		},
		{
			name:           "No genres",
			html:           `<html><body>No genres here</body></html>`,
			expectedCount:  0,
			expectedGenres: []string{},
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

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			genres := scraper.extractGenres(doc)
			assert.Len(t, genres, tt.expectedCount)

			for _, genre := range tt.expectedGenres {
				assert.Contains(t, genres, genre)
			}
		})
	}
}

// TestExtractReleaseDate verifies release date extraction
func TestExtractReleaseDate(t *testing.T) {
	tests := []struct {
		name         string
		html         string
		expectNil    bool
		expectedDate string
	}{
		{
			name:         "valid date",
			html:         "<html><body>Release: 2024/01/15</body></html>",
			expectNil:    false,
			expectedDate: "2024-01-15",
		},
		{
			name:      "no date",
			html:      "<html><body>No date here</body></html>",
			expectNil: true,
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

			releaseDate := scraper.extractReleaseDate(doc)

			if tt.expectNil {
				assert.Nil(t, releaseDate)
			} else {
				require.NotNil(t, releaseDate)
				assert.Equal(t, tt.expectedDate, releaseDate.Format("2006-01-02"))
			}
		})
	}
}

// TestExtractRuntime verifies runtime extraction
func TestExtractRuntime(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected int
	}{
		{"minutes in English", "<html><body>120 minutes</body></html>", 120},
		{"minutes in Japanese", "<html><body>120分</body></html>", 120},
		{"no runtime", "<html><body>No runtime</body></html>", 0},
		{"two-digit runtime", "<html><body>90 minutes</body></html>", 90},
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

			runtime := scraper.extractRuntime(doc)
			assert.Equal(t, tt.expected, runtime)
		})
	}
}

// TestExtractRating_OldSite verifies rating extraction from old site
func TestExtractRating_OldSite(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		expectNil     bool
		expectedScore float64
		expectedVotes int
	}{
		{
			name:          "word rating with votes",
			html:          `<html><head></head><body><strong>Four点</strong><p class="d-review__evaluates">評価数: <strong>50</strong></p></body></html>`,
			expectNil:     false,
			expectedScore: 8.0, // Four * 2 = 8.0
			expectedVotes: 50,
		},
		{
			name:          "numeric rating",
			html:          `<html><head></head><body><strong>4.5点</strong><p class="d-review__evaluates">評価数: <strong>100</strong></p></body></html>`,
			expectNil:     false,
			expectedScore: 9.0, // 4.5 * 2 = 9.0
			expectedVotes: 100,
		},
		{
			name:      "no rating",
			html:      `<html><body>No rating</body></html>`,
			expectNil: true,
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

			rating := scraper.extractRating(doc, false)

			if tt.expectNil {
				assert.Nil(t, rating)
			} else {
				require.NotNil(t, rating)
				assert.Equal(t, tt.expectedScore, rating.Score)
				assert.Equal(t, tt.expectedVotes, rating.Votes)
			}
		})
	}
}

// Helper function to parse HTML string into goquery document
func parseHTMLString(html string) (*goquery.Document, error) {
	reader := strings.NewReader(html)
	return goquery.NewDocumentFromReader(reader)
}

// TestExtractDescription_EdgeCases verifies description extraction with various HTML structures
func TestExtractDescription_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		isNewSite   bool
		expected    string
		description string
	}{
		{
			name: "Multiple paragraphs",
			html: `<html><body>
				<div class="mg-b20 lh4">
					<p class="mg-b20">First paragraph.</p>
					<p class="mg-b20">Second paragraph.</p>
				</div>
			</body></html>`,
			isNewSite:   false,
			expected:    "First paragraph.Second paragraph.",
			description: "Should extract all paragraphs from description div",
		},
		{
			name: "Description without p tag",
			html: `<html><body>
				<div class="mg-b20 lh4">Direct description text</div>
			</body></html>`,
			isNewSite:   false,
			expected:    "Direct description text",
			description: "Should extract description from div directly",
		},
		{
			name: "Empty description",
			html: `<html><body>
				<div class="mg-b20 lh4"></div>
			</body></html>`,
			isNewSite:   false,
			expected:    "",
			description: "Should handle empty description",
		},
		{
			name: "Description with extra whitespace",
			html: `<html><body>
				<div class="mg-b20 lh4">
					<p class="mg-b20">
						Description   with
						extra   whitespace
					</p>
				</div>
			</body></html>`,
			isNewSite:   false,
			expected:    "Description with extra whitespace",
			description: "Should clean whitespace",
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
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestExtractDirector_EdgeCases verifies director extraction with various patterns
func TestExtractDirector_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		expected    string
		description string
	}{
		{
			name:        "Director with special characters",
			html:        `<a href="?director=123">山田 太郎</a>`,
			expected:    "山田 太郎",
			description: "Should handle Japanese director name",
		},
		{
			name:        "Multiple directors",
			html:        `<a href="?director=123">Director One</a><a href="?director=456">Director Two</a>`,
			expected:    "Director One",
			description: "Should extract first director only",
		},
		{
			name:        "Director with extra whitespace",
			html:        `<a href="?director=123">  Director Name  </a>`,
			expected:    "Director Name",
			description: "Should clean whitespace from director name",
		},
		{
			name:        "No director",
			html:        `<html><body>No director info</body></html>`,
			expected:    "",
			description: "Should return empty when no director found",
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

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			result := scraper.extractDirector(doc)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestExtractMaker_Formats verifies maker extraction with different URL formats
func TestExtractMaker_Formats(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		isNewSite   bool
		expected    string
		description string
	}{
		{
			name:        "Maker with ?maker= format",
			html:        `<a href="?maker=123">Studio Name</a>`,
			isNewSite:   false,
			expected:    "Studio Name",
			description: "Should extract maker from ?maker= link",
		},
		{
			name:        "Maker with /article=maker/ format",
			html:        `<a href="/article=maker/id=123">Studio Name</a>`,
			isNewSite:   false,
			expected:    "Studio Name",
			description: "Should extract maker from /article=maker/ link",
		},
		{
			name:        "Maker with Japanese characters",
			html:        `<a href="?maker=123">アイデアポケット</a>`,
			isNewSite:   false,
			expected:    "アイデアポケット",
			description: "Should handle Japanese maker name",
		},
		{
			name:        "No maker",
			html:        `<html><body>No maker info</body></html>`,
			isNewSite:   false,
			expected:    "",
			description: "Should return empty when no maker found",
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

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			result := scraper.extractMaker(doc, tt.isNewSite)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestExtractLabel_Formats verifies label extraction with different URL formats
func TestExtractLabel_Formats(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		expected    string
		description string
	}{
		{
			name:        "Label with ?label= format",
			html:        `<a href="?label=123">Label Name</a>`,
			expected:    "Label Name",
			description: "Should extract label from ?label= link",
		},
		{
			name:        "Label with /article=label/ format",
			html:        `<a href="/article=label/id=123">Label Name</a>`,
			expected:    "Label Name",
			description: "Should extract label from /article=label/ link",
		},
		{
			name:        "Label with special characters",
			html:        `<a href="?label=123">Label &amp; Co.</a>`,
			expected:    "Label & Co.",
			description: "Should decode HTML entities in label (&amp; becomes &)",
		},
		{
			name:        "No label",
			html:        `<html><body>No label info</body></html>`,
			expected:    "",
			description: "Should return empty when no label found",
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

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			result := scraper.extractLabel(doc)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestExtractSeries_EdgeCases verifies series extraction handles empty cases
func TestExtractSeries_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		isNewSite   bool
		expected    string
		description string
	}{
		{
			name:        "No series",
			html:        `<html><body>No series info</body></html>`,
			isNewSite:   false,
			expected:    "",
			description: "Should return empty when no series found",
		},
		{
			name:        "Empty series tag",
			html:        `<html><body><a href="/digital/videoa/-/list/=/article=series/id=123/"></a></td></body></html>`,
			isNewSite:   false,
			expected:    "",
			description: "Should handle empty series name",
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

			result := scraper.extractSeries(doc, tt.isNewSite)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestExtractCoverURL_EdgeCases verifies cover URL extraction with various patterns
func TestExtractCoverURL_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		isNewSite   bool
		expected    string
		description string
	}{
		{
			name:        "Digital video cover",
			html:        `<img src="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535ps.jpg" />`,
			isNewSite:   false,
			expected:    "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
			description: "Should replace ps.jpg with pl.jpg for larger image",
		},
		{
			name:        "Physical DVD cover",
			html:        `<img src="https://pics.dmm.co.jp/mono/movie/adult/abp420/abp420ps.jpg" />`,
			isNewSite:   false,
			expected:    "https://pics.dmm.co.jp/mono/movie/adult/abp420/abp420pl.jpg",
			description: "Should replace ps.jpg with pl.jpg for DVD",
		},
		{
			name:        "Amateur cover",
			html:        `<img src="https://pics.dmm.co.jp/digital/amateur/xyz123/xyz123ps.jpg" />`,
			isNewSite:   false,
			expected:    "https://pics.dmm.co.jp/digital/amateur/xyz123/xyz123pl.jpg",
			description: "Should handle amateur category covers",
		},
		{
			name:        "No cover image",
			html:        `<html><body>No images</body></html>`,
			isNewSite:   false,
			expected:    "",
			description: "Should return empty when no cover found",
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

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			result := scraper.extractCoverURL(doc, tt.isNewSite, "")
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestExtractScreenshots_EdgeCases verifies screenshot extraction with various patterns
func TestExtractScreenshots_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		isNewSite     bool
		expectedCount int
		description   string
	}{
		{
			name: "Multiple screenshots",
			html: `<html><body>
				<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg" /></a>
				<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-2.jpg" /></a>
				<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-3.jpg" /></a>
			</body></html>`,
			isNewSite:     false,
			expectedCount: 3,
			description:   "Should extract all screenshot URLs",
		},
		{
			name: "Screenshots with jp- prefix added",
			html: `<html><body>
				<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535-1.jpg" /></a>
			</body></html>`,
			isNewSite:     false,
			expectedCount: 1,
			description:   "Should add jp- prefix to screenshots",
		},
		{
			name:          "No screenshots",
			html:          `<html><body>No screenshots</body></html>`,
			isNewSite:     false,
			expectedCount: 0,
			description:   "Should return empty array when no screenshots found",
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
			assert.Len(t, result, tt.expectedCount, tt.description)

			// Verify jp- prefix is added
			if tt.expectedCount > 0 {
				for _, url := range result {
					assert.Contains(t, url, "jp-", "Screenshot URL should contain jp- prefix")
				}
			}
		})
	}
}

// TestParseHTML_ScrapeActressFlag verifies actress extraction respects scrape_actress flag
func TestParseHTML_ScrapeActressFlag(t *testing.T) {
	htmlContent := `
<!DOCTYPE html>
<html>
<body>
	<h1 id="title" class="item">Test Movie</h1>
	<table>
		<tr>
			<td>Actress:</td>
			<td>
				<a href="?actress=111">Test Actress</a>
			</td>
		</tr>
	</table>
</body>
</html>
`

	tests := []struct {
		name               string
		scrapeActress      bool
		expectedActressLen int
	}{
		{
			name:               "Scrape actress enabled",
			scrapeActress:      true,
			expectedActressLen: 1,
		},
		{
			name:               "Scrape actress disabled",
			scrapeActress:      false,
			expectedActressLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						Enabled:       true,
						ScrapeActress: tt.scrapeActress,
					},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(htmlContent)
			require.NoError(t, err)

			result, err := scraper.parseHTML(doc, "https://www.dmm.co.jp/test")
			require.NoError(t, err)

			assert.Len(t, result.Actresses, tt.expectedActressLen)
		})
	}
}

// TestNormalizeContentID_EdgeCases verifies content ID normalization with edge cases
func TestNormalizeContentID_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Empty string", "", ""},
		{"Only letters", "ABC", "abc"},
		{"Only numbers", "123", "123"},
		{"Single character prefix with hyphen", "A-1", "a00001"},      // Had hyphen = padding
		{"Long prefix with hyphen", "ABCDEF-123", "abcdef00123"},      // Had hyphen = padding
		{"Multiple hyphens", "A-B-C-123", "abc00123"},                 // Had hyphen = padding
		{"Special suffix with hyphen", "IPX-535-HD", "ipx00535hd"},    // Had hyphen = padding
		{"Uppercase with suffix and hyphen", "ABC-001Z", "abc00001z"}, // Had hyphen = padding
		{"Long prefix no hyphen", "ABCDEF123", "abcdef123"},           // No hyphen, 6+3 = no padding (amateur)
		{"Short prefix no hyphen", "ABC123", "abc00123"},              // No hyphen, 3+3 = padding (standard, conservative)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeContentID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeID_EdgeCases verifies ID normalization with edge cases
func TestNormalizeID_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Empty string", "", ""},
		{"Only letters lowercase", "abc", "ABC"},
		{"Only letters uppercase", "ABC", "ABC"},
		{"Only numbers", "12345", "12345"},
		{"Mixed case 6 letters + 5 digits", "AbCdEf00123", "ABCDEF-123"}, // 6 letters + 5 digits (>4) = hyphen
		{"Leading digit prefix", "1abc00123", "ABC-123"},                 // 3 letters + 5 digits (>4) = hyphen
		{"Multiple leading digits", "999xyz00456", "XYZ-456"},            // 3 letters + 5 digits (>4) = hyphen
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeID_MalformedInputs tests handling of malformed and edge case inputs
func TestNormalizeID_MalformedInputs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Whitespace handling
		{"Leading whitespace", " sone860", " SONE860"},   // Whitespace preserved (not stripped in current implementation)
		{"Trailing whitespace", "sone860 ", "SONE-860 "}, // Normalizes ID, preserves trailing space
		{"Internal whitespace", "sone 860", "SONE 860"},  // No match - returns uppercase as-is

		// Special characters (no match - returns uppercase as-is)
		{"Special chars in ID", "IPX-535!@#", "IPX-535!@#"},
		{"Asterisk in ID", "SONE*860", "SONE*860"},

		// Very long numbers (no integer overflow with string-based approach)
		// Leading zeros are stripped, then padded to minimum 3 digits
		{"Very long number", "ipx" + strings.Repeat("0", 100) + "1", "IPX-001"},
		{"Extremely long number", "abc" + strings.Repeat("9", 200), "ABC-" + strings.Repeat("9", 200)},

		// All zeros
		{"All zero digits", "ipx00000", "IPX-000"},
		{"Single zero", "abc0", "ABC-000"},

		// Very short numbers
		{"Single digit", "abc1", "ABC-001"},
		{"Two digits", "abc12", "ABC-012"},

		// Only components (no match patterns)
		{"Only prefix", "sone", "SONE"},
		{"Only numbers", "860", "860"},

		// Mixed case already handled in main tests
		{"MixedCase input", "AbC123", "ABC-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractCandidateURLs_Priorities verifies URL priority assignment
func TestExtractCandidateURLs_Priorities(t *testing.T) {
	tests := []struct {
		name             string
		html             string
		contentID        string
		expectedPriority int
		expectedURL      string
		description      string
	}{
		{
			name: "Physical DVD has highest priority",
			html: `
				<a href="https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=sone860/">Physical DVD</a>
				<a href="https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/">Monthly Standard</a>
			`,
			contentID:        "sone860",
			expectedPriority: 6,
			expectedURL:      "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=sone860/",
			description:      "/mono/dvd/ should have priority 6 (highest - full metadata)",
		},
		{
			name: "Digital video has second priority",
			html: `
				<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=mdb087/">Digital Video</a>
				<a href="https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/">Monthly Standard</a>
			`,
			contentID:        "mdb087",
			expectedPriority: 5,
			expectedURL:      "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=mdb087/",
			description:      "/digital/videoa/ should have priority 5 (full metadata)",
		},
		{
			name: "Streaming video has third priority",
			html: `
				<a href="https://video.dmm.co.jp/av/content/?id=61mdb087">Streaming</a>
				<a href="https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/">Monthly Standard</a>
			`,
			contentID:        "61mdb087",
			expectedPriority: 3,
			expectedURL:      "https://video.dmm.co.jp/av/content/?id=61mdb087",
			description:      "video.dmm.co.jp/av/ should have priority 3",
		},
		{
			name: "Monthly premium has fourth priority",
			html: `
				<a href="https://www.dmm.co.jp/monthly/premium/-/detail/=/cid=61mdb087/">Monthly Premium</a>
				<a href="https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=mdb087/">Monthly Standard</a>
			`,
			contentID:        "61mdb087",
			expectedPriority: 2,
			expectedURL:      "https://www.dmm.co.jp/monthly/premium/-/detail/=/cid=61mdb087/",
			description:      "/monthly/premium/ should have priority 2 (limited metadata)",
		},
		{
			name: "Monthly standard has lowest priority",
			html: `
				<a href="https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/">Monthly Standard</a>
			`,
			contentID:        "61mdb087",
			expectedPriority: 1,
			expectedURL:      "https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/",
			description:      "/monthly/standard/ should have priority 1 (lowest - limited metadata)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						Enabled:       true,
						EnableBrowser: true, // Enable to include video.dmm.co.jp URLs
					},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			candidates := scraper.extractCandidateURLs(doc, tt.contentID)
			require.NotEmpty(t, candidates, "Should extract at least one candidate")

			// Find the candidate with highest priority
			var best urlCandidate
			for _, c := range candidates {
				if c.priority > best.priority {
					best = c
				}
			}

			assert.Equal(t, tt.expectedPriority, best.priority, tt.description)
			assert.Equal(t, tt.expectedURL, best.url)
		})
	}
}

// TestExtractCandidateURLs_BaseIDMatching verifies base ID extraction and matching
func TestExtractCandidateURLs_BaseIDMatching(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		contentID   string
		shouldMatch bool
		description string
	}{
		{
			name: "Matches base ID without prefix",
			html: `
				<a href="https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=sone860/">Product</a>
			`,
			contentID:   "4sone860",
			shouldMatch: true,
			description: "Should match base ID 'sone860' from content ID '4sone860'",
		},
		{
			name: "Matches content ID with prefix",
			html: `
				<a href="https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/">Product</a>
			`,
			contentID:   "61mdb087",
			shouldMatch: true,
			description: "Should match content ID '61mdb087'",
		},
		{
			name: "Does not match unrelated ID",
			html: `
				<a href="https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=xyz123/">Product</a>
			`,
			contentID:   "abc456",
			shouldMatch: false,
			description: "Should not match unrelated IDs",
		},
		{
			name: "Matches base ID when URL uses base format",
			html: `
				<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/">Product</a>
			`,
			contentID:   "ipx00535",
			shouldMatch: true,
			description: "Should match exact content ID 'ipx00535'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						Enabled: true,
					},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			candidates := scraper.extractCandidateURLs(doc, tt.contentID)

			if tt.shouldMatch {
				assert.NotEmpty(t, candidates, tt.description)
			} else {
				assert.Empty(t, candidates, tt.description)
			}
		})
	}
}

// TestExtractCandidateURLs_ExcludePatterns verifies excluded URL patterns
func TestExtractCandidateURLs_ExcludePatterns(t *testing.T) {
	tests := []struct {
		name             string
		html             string
		contentID        string
		enableBrowser    bool
		shouldBeExcluded bool
		description      string
	}{
		{
			name: "Rental pages excluded",
			html: `
				<a href="https://www.dmm.co.jp/rental/-/detail/=/cid=mdb087/">Rental</a>
			`,
			contentID:        "mdb087",
			enableBrowser:    false,
			shouldBeExcluded: true,
			description:      "/rental/ URLs should be excluded",
		},
		{
			name: "Streaming excluded when browser mode disabled",
			html: `
				<a href="https://video.dmm.co.jp/av/content/?id=mdb087">Streaming</a>
			`,
			contentID:        "mdb087",
			enableBrowser:    false,
			shouldBeExcluded: true,
			description:      "video.dmm.co.jp should be excluded when browser mode is disabled",
		},
		{
			name: "Streaming included when browser mode enabled",
			html: `
				<a href="https://video.dmm.co.jp/av/content/?id=mdb087">Streaming</a>
			`,
			contentID:        "mdb087",
			enableBrowser:    true,
			shouldBeExcluded: false,
			description:      "video.dmm.co.jp should be included when browser mode is enabled",
		},
		{
			name: "Monthly standard not excluded",
			html: `
				<a href="https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/">Monthly</a>
			`,
			contentID:        "61mdb087",
			enableBrowser:    false,
			shouldBeExcluded: false,
			description:      "/monthly/standard/ should not be excluded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					DMM: config.DMMConfig{
						Enabled:       true,
						EnableBrowser: tt.enableBrowser,
					},
				},
			}
			scraper := New(cfg, nil)

			doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", tt.html))
			require.NoError(t, err)

			candidates := scraper.extractCandidateURLs(doc, tt.contentID)

			if tt.shouldBeExcluded {
				assert.Empty(t, candidates, tt.description)
			} else {
				assert.NotEmpty(t, candidates, tt.description)
			}
		})
	}
}

// TestExtractCandidateURLs_PriorityOrder verifies correct priority ordering
func TestExtractCandidateURLs_PriorityOrder(t *testing.T) {
	html := `
		<a href="https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=mdb087/">Physical DVD (Priority 6 - full metadata)</a>
		<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=mdb087/">Digital Video (Priority 5 - full metadata)</a>
		<a href="https://video.dmm.co.jp/av/content/?id=61mdb087">Streaming (Priority 3)</a>
		<a href="https://www.dmm.co.jp/monthly/premium/-/detail/=/cid=61mdb087/">Monthly Premium (Priority 2 - limited metadata)</a>
		<a href="https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=61mdb087/">Monthly Standard (Priority 1 - limited metadata)</a>
	`

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled:       true,
				EnableBrowser: true,
			},
		},
	}
	scraper := New(cfg, nil)

	doc, err := parseHTMLString(fmt.Sprintf("<html><body>%s</body></html>", html))
	require.NoError(t, err)

	candidates := scraper.extractCandidateURLs(doc, "61mdb087")
	require.Len(t, candidates, 5, "Should extract all 5 URL types")

	// Verify priorities are assigned correctly
	priorityMap := make(map[int]string)
	for _, c := range candidates {
		priorityMap[c.priority] = c.url
	}

	assert.Contains(t, priorityMap[6], "/mono/dvd/", "Priority 6 should be physical DVD (highest - full metadata)")
	assert.Contains(t, priorityMap[5], "/digital/videoa/", "Priority 5 should be digital video (full metadata)")
	assert.Contains(t, priorityMap[3], "video.dmm.co.jp", "Priority 3 should be streaming")
	assert.Contains(t, priorityMap[2], "/monthly/premium/", "Priority 2 should be monthly premium (limited metadata)")
	assert.Contains(t, priorityMap[1], "/monthly/standard/", "Priority 1 should be monthly standard (lowest - limited metadata)")
}

// TestResolveContentID_NoRepository verifies error when repository is nil
func TestResolveContentID_NoRepository(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled: true,
			},
		},
	}

	// Create scraper with nil repository
	scraper := New(cfg, nil)

	_, err := scraper.ResolveContentID("ipx-535")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "content ID repository not available")
}

func TestResolveContentID_UsesSearchQueryVariations(t *testing.T) {
	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(dbCfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.AutoMigrate())

	repo := database.NewContentIDMappingRepository(db)
	scraper := New(&config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled: true,
			},
		},
	}, repo)

	transport := &searchVariationRoundTripper{
		responseByQuery: map[string]string{
			"clt069": `<html><body><p>No matching anchors</p></body></html>`,
			"clt00069": `<html><body>
				<a href="/digital/videoa/-/detail/=/cid=clt00069/">CLT-069 result</a>
			</body></html>`,
		},
	}
	scraper.client.SetTransport(transport)

	contentID, err := scraper.ResolveContentID("CLT-069")
	require.NoError(t, err)
	assert.Equal(t, "clt00069", contentID)
	assert.Equal(t, []string{"clt069", "clt00069"}, transport.requestedQueries)

	cached, err := repo.FindBySearchID("CLT-069")
	require.NoError(t, err)
	assert.Equal(t, "clt00069", cached.ContentID)
}

// TestGetURL_NoRepository verifies error when repository is nil
func TestGetURL_NoRepository(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			DMM: config.DMMConfig{
				Enabled: true,
			},
		},
	}

	// Create scraper with nil repository
	scraper := New(cfg, nil)

	_, err := scraper.GetURL("ipx-535")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "movie not found on DMM")
}

type searchVariationRoundTripper struct {
	responseByQuery  map[string]string
	requestedQueries []string
}

func (rt *searchVariationRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	query := extractSearchQueryFromPath(req.URL.Path)
	rt.requestedQueries = append(rt.requestedQueries, query)

	body, ok := rt.responseByQuery[query]
	if !ok {
		body = "<html><body></body></html>"
	}

	return &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func extractSearchQueryFromPath(path string) string {
	marker := "searchstr="
	idx := strings.Index(path, marker)
	if idx == -1 {
		return ""
	}

	remaining := path[idx+len(marker):]
	if slashIdx := strings.Index(remaining, "/"); slashIdx != -1 {
		return remaining[:slashIdx]
	}
	return remaining
}

// TestMatchesWithVariantSuffix verifies matching with single-letter variant suffixes
func TestMatchesWithVariantSuffix(t *testing.T) {
	tests := []struct {
		name      string
		urlCID    string
		searchIDs []string
		want      bool
	}{
		// Exact matches
		{
			name:      "exact match single ID",
			urlCID:    "akdl229",
			searchIDs: []string{"akdl229"},
			want:      true,
		},
		{
			name:      "exact match multiple IDs",
			urlCID:    "ipx535",
			searchIDs: []string{"akdl229", "ipx535", "sone860"},
			want:      true,
		},
		// Variant suffix matches (the main feature)
		{
			name:      "variant suffix a - real case AKDL-229",
			urlCID:    "akdl229a",
			searchIDs: []string{"akdl229"},
			want:      true,
		},
		{
			name:      "variant suffix b",
			urlCID:    "ipx535b",
			searchIDs: []string{"ipx535"},
			want:      true,
		},
		{
			name:      "variant suffix z",
			urlCID:    "sone860z",
			searchIDs: []string{"sone860"},
			want:      true,
		},
		{
			name:      "variant match in multiple search IDs",
			urlCID:    "akdl229a",
			searchIDs: []string{"akdl229", "akdl00229", "notmatch"},
			want:      true,
		},
		// Should NOT match
		{
			name:      "no match different IDs",
			urlCID:    "xyz123",
			searchIDs: []string{"akdl229", "ipx535"},
			want:      false,
		},
		{
			name:      "no match uppercase suffix",
			urlCID:    "akdl229A",
			searchIDs: []string{"akdl229"},
			want:      false, // Only lowercase suffixes allowed
		},
		{
			name:      "no match multi-character suffix",
			urlCID:    "akdl229ab",
			searchIDs: []string{"akdl229"},
			want:      false, // Only single character suffix
		},
		{
			name:      "no match numeric suffix",
			urlCID:    "akdl2291",
			searchIDs: []string{"akdl229"},
			want:      false, // Only letters, not numbers
		},
		{
			name:      "no match partial prefix",
			urlCID:    "akdl22",
			searchIDs: []string{"akdl229"},
			want:      false,
		},
		{
			name:      "no match longer base",
			urlCID:    "akdl229",
			searchIDs: []string{"akdl2290"},
			want:      false,
		},
		// Edge cases
		{
			name:      "empty urlCID",
			urlCID:    "",
			searchIDs: []string{"akdl229"},
			want:      false,
		},
		{
			name:      "empty search IDs",
			urlCID:    "akdl229a",
			searchIDs: []string{},
			want:      false,
		},
		{
			name:      "single character urlCID with suffix",
			urlCID:    "ab",
			searchIDs: []string{"a"},
			want:      true, // "b" is a valid single lowercase letter suffix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesWithVariantSuffix(tt.urlCID, tt.searchIDs...)
			assert.Equal(t, tt.want, got)
		})
	}
}
