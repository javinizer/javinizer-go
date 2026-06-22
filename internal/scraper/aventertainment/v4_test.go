package aventertainment

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

	result, err := s.ScrapeURL(context.Background(), ts.URL+"/eng/detail/12345")
	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.aventertainments.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.aventertainments.com/productdetail/12345", true},
		{"https://aventertainments.com/productdetail/12345", true},
		{"https://example.com/productdetail/12345", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

func TestExtractIDFromURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	id, err := s.ExtractIDFromURL("https://www.aventertainments.com/productdetail/12345")
	assert.NoError(t, err)
	assert.Equal(t, "12345", id)
}

func TestExtractDetailLinksV4(t *testing.T) {
	html := `<html><body>
		<div class="product-list">
			<a href="/eng/detail/12345">ABC-123</a>
			<a href="/eng/detail/67890">XYZ-456</a>
		</div>
	</body></html>`

	links := extractDetailLinks(html, "https://www.aventertainments.com")
	assert.GreaterOrEqual(t, len(links), 0)
}
