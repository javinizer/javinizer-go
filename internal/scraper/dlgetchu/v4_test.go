package dlgetchu

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
		baseURL:     "https://dl.getchu.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "RJ123456")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	// CanHandleURL checks for dl.getchu.com or getchu.com
	assert.True(t, s.CanHandleURL("https://dl.getchu.com/work/=/product_id/RJ123456.html"))
	assert.True(t, s.CanHandleURL("https://www.dl.getchu.com/work/=/product_id/RJ123456.html"))
	assert.False(t, s.CanHandleURL("https://example.com/work/RJ123456"))
	assert.False(t, s.CanHandleURL(""))
}

func TestParseDetailPageV4(t *testing.T) {
	detailHTML := `<html>
<body>
<div id="work_name"><a>Test Doujin Work</a></div>
<div class="work_maker"><a>Circle A</a></div>
<table>
	<tr><th>販売日</th><td>2024/01/15</td></tr>
</table>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(detailHTML))
	require.NoError(t, err)

	result := parseDetailPage(doc, detailHTML, "https://dl.getchu.com/work/=/product_id/RJ123456", "RJ123456")
	require.NotNil(t, result)
	assert.Equal(t, "RJ123456", result.ID)
}
