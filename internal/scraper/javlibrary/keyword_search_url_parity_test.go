package javlibrary

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestKeywordSearchKeepsURLBytesAndResult(t *testing.T) {
	const fixture = `<title>Fixture entry - JAVLibrary</title><div id="video_info"></div>
<div id="video_id"><table><tr><td class="text">FIX-123</td></tr></table></div>`
	for _, tc := range []struct {
		name, base, lang, input, wantURL, html string
	}{
		{
			name:    "Unicode and reserved query characters",
			base:    "https://www.javlibrary.com:8443/catalog%2Farchive",
			lang:    "ja",
			input:   "日本 + 100% & title",
			wantURL: "https://www.javlibrary.com:8443/catalog%2Farchive/ja/vl_searchbyid.php?keyword=%E6%97%A5%E6%9C%AC+%2B+100%25+%26+title",
			html:    fixture,
		},
		{
			name:    "normal keyword with no matches",
			base:    "https://www.javlibrary.org",
			lang:    "en",
			input:   "FIX-000",
			wantURL: "https://www.javlibrary.org/en/vl_searchbyid.php?keyword=FIX-000",
			html:    `<title>No results</title>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := &scraper{
				client: resty.New().SetTransport(offlineEdgesTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					require.Equal(t, http.MethodGet, req.Method)
					require.Equal(t, tc.wantURL, req.URL.String())
					require.Equal(t, tc.input, req.URL.Query().Get("keyword"))
					return &http.Response{
						Request:    req,
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"text/html"}},
						Body:       io.NopCloser(strings.NewReader(tc.html)),
					}, nil
				})),
				enabled:     true,
				baseURL:     tc.base,
				language:    tc.lang,
				rateLimiter: ratelimit.NewLimiter(0),
			}
			u, err := s.GetURL(t.Context(), tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.wantURL, u)
			require.Zero(t, calls, "formatting must not dispatch")
			result, err := s.Search(t.Context(), tc.input)
			require.Equal(t, 1, calls)
			if tc.html != fixture {
				var notFound *models.ScraperError
				require.ErrorAs(t, err, &notFound)
				require.EqualError(t, err, models.NewScraperNotFoundError("JavLibrary", "movie FIX-000 not found on JavLibrary").Error())
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.wantURL, result.SourceURL)
			require.Equal(t, "FIX-123", result.ID)
			require.Equal(t, "Fixture entry", result.Title)
			require.Equal(t, tc.lang, result.Language)
		})
	}
}
