package dmm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

type nestingLimitTransport func(*http.Request) (*http.Response, error)

func (f nestingLimitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOfflineNestingLimitDMMDetailSources(t *testing.T) {
	for _, browser := range []bool{false, true} {
		for _, search := range []bool{false, true} {
			name := map[bool]string{false: "http", true: "browser"}[browser] + map[bool]string{false: " direct", true: " search"}[search]
			t.Run(name, func(t *testing.T) {
				repo := newDMMTestRepo(t)
				require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{SearchID: "SSIS-200", ContentID: "ssis00200", Source: "dmm"}))
				target := "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ssis00200/"
				if browser {
					target = "https://video.dmm.co.jp/av/content/?id=ssis00200"
				}
				deep := strings.Repeat("<dl>", 511)
				s := newScraper(&models.ScraperSettings{Enabled: true, UseBrowser: browser}, nil, models.FlareSolverrConfig{}, dmmOptions{Browser: models.BrowserConfig{Enabled: browser}, ContentIDRepo: repo})
				s.client = resty.New().SetTransport(nestingLimitTransport(func(req *http.Request) (*http.Response, error) {
					body := deep
					if strings.Contains(req.URL.Path, "search") {
						body = `<a href="` + target + `">SSIS-200</a>`
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
				}))
				if browser {
					stubDMMBrowser(t, s, target, deep, nil)
				}
				var result *models.ScraperResult
				var err error
				if search {
					result, err = s.Search(context.Background(), "SSIS-200")
				} else {
					result, err = s.ScrapeURL(context.Background(), target)
				}
				require.Nil(t, result)
				require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
				cached, err := repo.FindBySearchID(context.Background(), "SSIS-200")
				require.NoError(t, err)
				require.Equal(t, "ssis00200", cached.ContentID)
			})
		}
	}
}

func TestOfflineNestingLimitDMMResolverAndActress(t *testing.T) {
	repo := newDMMTestRepo(t)
	s := &scraper{contentIDRepo: repo, rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(nestingLimitTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(strings.Repeat("<dl>", 511)))}, nil
	}))}
	cid, err := s.ResolveContentIDCtx(context.Background(), "SSIS-200")
	require.Empty(t, cid)
	require.ErrorContains(t, err, "open stack of elements exceeds 512 nodes")
	require.Empty(t, s.extractRomajiVariantsFromActressPageCtx(context.Background(), 12345))
	_, err = repo.FindBySearchID(context.Background(), "SSIS-200")
	require.True(t, database.IsNotFound(err), "rejected search page must not create a mapping")
}
