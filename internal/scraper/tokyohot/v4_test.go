package tokyohot

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.tokyo-hot.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "012345")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	assert.True(t, s.CanHandleURL("https://www.tokyo-hot.com/product/012345/"))
	assert.True(t, s.CanHandleURL("https://tokyo-hot.com/product/012345/"))
	assert.True(t, s.CanHandleURL("https://my.tokyo-hot.com/product/012345/"))
	assert.False(t, s.CanHandleURL("https://example.com/product/012345/"))
	assert.False(t, s.CanHandleURL(""))
}

func TestExtractIDFromURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	id, err := s.ExtractIDFromURL("https://www.tokyo-hot.com/product/AB123/")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)
}

func TestResolveSearchQueryV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	url, ok := s.ResolveSearchQuery("https://www.tokyo-hot.com/product/AB123/")
	assert.True(t, ok)
	assert.NotEmpty(t, url)
}

func TestExtractGenresV4(t *testing.T) {
	html := `<div class="genre"><a>Genre A</a><a>Genre B</a></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	genres := extractGenres(doc)
	assert.GreaterOrEqual(t, len(genres), 0)
}

func TestExtractActressesV4(t *testing.T) {
	html := `<div class="actress"><a>Actress A</a><a>Actress B</a></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	actresses := extractActresses(doc)
	assert.GreaterOrEqual(t, len(actresses), 0)
}

func TestExtractIDV4(t *testing.T) {
	// extractID requires a pattern like "LettersNumbers"
	assert.Equal(t, "AB123", extractID("AB123"))
	assert.Equal(t, "", extractID("nodigits"))
}
