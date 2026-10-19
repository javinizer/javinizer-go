package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchUnprovenT28PrefixKeepsPageBoundary(t *testing.T) {
	for _, cid := range []string{"55t28123h", "n_600t28123h", "55t28123ai", "n_600t28123ai"} {
		tID, t28ID := "T-28123H", "T28-123H"
		if strings.HasSuffix(cid, "ai") {
			tID, t28ID = "T-28999AI", "T28-456AI"
		}
		for _, page := range []struct{ display, wantID string }{
			{tID, tID}, {t28ID, t28ID}, {"", ""}, {"unknown", ""},
		} {
			t.Run(cid+"/"+page.display, func(t *testing.T) {
				result, err := searchT28Fixture(t, cid, cid, page.display)
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, cid, result.ContentID)
				assert.Equal(t, page.wantID, result.ID)
			})
		}
	}
}

func TestSearchExplicitT28BoundaryRejectsOppositePage(t *testing.T) {
	for _, tc := range []struct{ cid, tID, t28ID string }{
		{"55t28123h", "T-28123-HD", "T28-123-HD"},
		{"n_600t28123ai", "T-28999-AI", "T28-456-AI"},
	} {
		for _, query := range []string{tc.tID, tc.t28ID} {
			for _, display := range []string{tc.tID, tc.t28ID} {
				t.Run(query+"/"+display, func(t *testing.T) {
					result, err := searchT28Fixture(t, query, tc.cid, display)
					if query != display {
						require.Error(t, err)
						assert.Nil(t, result)
						scraperErr, ok := models.AsScraperError(err)
						require.True(t, ok)
						assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
						return
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, tc.cid, result.ContentID)
					assert.Equal(t, canonicalRemasterDisplayID(display), result.ID)
				})
			}
		}
	}
}

func searchT28Fixture(t *testing.T, query, cid, display string) (*models.ScraperResult, error) {
	t.Helper()
	s, repo := newRemasterTestScraper(t)
	require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{
		SearchID: query, ContentID: cid, Source: s.Name(),
	}))
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<a href="/mono/dvd/-/detail/=/cid=` + cid + `/">candidate</a>`
		}
		if strings.Contains(u, "cid="+cid+"/") {
			return 200, `<html><body><table><tr><td>品番：</td><td>` + display + `</td></tr></table></body></html>`
		}
		return 404, ""
	}})
	return s.Search(context.Background(), query)
}
