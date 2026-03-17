package javdb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearch_RetriesSparseDetailWithDirectSuccess(t *testing.T) {
	var detailRequests int

	searchHTML := `
<html><body>
	<div class="movie-list">
		<div class="item">
			<a href="/v/retry123">
				<div class="video-title"><strong>IPX-123</strong> Retry Title</div>
			</a>
		</div>
	</div>
</body></html>`
	sparseDetailHTML := `<html><body><h2 class="title is-4"><strong>IPX-123</strong> IPX-123</h2></body></html>`
	richDetailHTML := `
<html><body>
	<h2 class="title is-4"><strong>IPX-123</strong> Retry Title</h2>
	<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/retry-cover.jpg" /></div>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>ID:</strong><span class="value">IPX-123</span></div>
		<div class="panel-block"><strong>Released Date:</strong><span class="value">2024-03-04</span></div>
		<div class="panel-block"><strong>Duration:</strong><span class="value">135 minute(s)</span></div>
		<div class="panel-block"><strong>Maker:</strong><span class="value"><a>Retry Maker</a></span></div>
	</div>
</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(searchHTML))
		case r.URL.Path == "/v/retry123":
			detailRequests++
			if detailRequests == 1 {
				_, _ = w.Write([]byte(sparseDetailHTML))
				return
			}
			_, _ = w.Write([]byte(richDetailHTML))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	scraper := &Scraper{
		client:       resty.New(),
		cfg:          &config.JavDBConfig{Enabled: true},
		enabled:      true,
		baseURL:      server.URL,
		requestDelay: 0,
	}
	scraper.lastRequestTime.Store(time.Time{})

	result, err := scraper.Search("IPX-123")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 2, detailRequests)
	assert.Equal(t, "Retry Title", result.Title)
	assert.Equal(t, "Retry Maker", result.Maker)
	assert.Equal(t, "https://img.example.com/retry-cover.jpg", result.CoverURL)
	require.NotNil(t, result.ReleaseDate)
	assert.Equal(t, "2024-03-04", result.ReleaseDate.Format("2006-01-02"))
}

func TestSearch_FailsWhenDirectRetryStillSparse(t *testing.T) {
	searchHTML := `
<html><body>
	<div class="movie-list">
		<div class="item">
			<a href="/v/retry123">
				<div class="video-title"><strong>IPX-123</strong> Retry Title</div>
			</a>
		</div>
	</div>
</body></html>`
	sparseDetailHTML := `<html><body><h2 class="title is-4"><strong>IPX-123</strong> IPX-123</h2></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(searchHTML))
		case r.URL.Path == "/v/retry123":
			_, _ = w.Write([]byte(sparseDetailHTML))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	scraper := &Scraper{
		client:       resty.New(),
		cfg:          &config.JavDBConfig{Enabled: true},
		enabled:      true,
		baseURL:      server.URL,
		requestDelay: 0,
	}
	scraper.lastRequestTime.Store(time.Time{})

	result, err := scraper.Search("IPX-123")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "non-detail content")
}
