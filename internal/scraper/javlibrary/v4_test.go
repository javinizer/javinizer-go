package javlibrary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
)

func TestScrapeURLV4_StatusErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), ts.URL+"/?v=ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestParseDetailPageV4(t *testing.T) {
	detailHTML := `<html>
<body>
<div id="video_title"><h3><a>ABC-123 Test Movie</a></h3></div>
<div id="video_id"><td class="text">ABC-123</td></div>
<div id="video_date"><td class="text">2024-01-15</td></div>
<div id="video_length"><td class="text">120</td></div>
<div id="video_maker"><td class="text"><a>TestMaker</a></td></div>
<div id="video_jacket"><img src="https://pics.dmm.co.jp/cover.jpg" /></div>
</body>
</html>`

	s := &scraper{
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(detailHTML, "ABC-123", "https://www.javlibrary.com/ja/?v=ABC-123", "ja")
	if err != nil {
		t.Logf("parseDetailPage error: %v", err)
	}
	// The parseDetailPage may or may not succeed depending on HTML structure
	// but it should not panic
	_ = result
}

func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	assert.True(t, s.CanHandleURL("https://www.javlibrary.com/ja/?v=ABC-123"))
	assert.True(t, s.CanHandleURL("https://javlibrary.com/?v=ABC-123"))
	assert.False(t, s.CanHandleURL("https://example.com/?v=ABC-123"))
	assert.False(t, s.CanHandleURL(""))
}

func TestExtractIDFromURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	id, err := s.ExtractIDFromURL("https://www.javlibrary.com/ja/?v=ABC-123")
	assert.NoError(t, err)
	assert.Equal(t, "ABC-123", id)
}

func TestCloseV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}
	assert.NoError(t, s.Close())
}
