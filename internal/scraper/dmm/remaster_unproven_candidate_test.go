package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnprovenT28CandidateNeedsVerifiedTPage(t *testing.T) {
	for _, tc := range []struct {
		cid, query, page string
		accept           bool
	}{
		{"55t28123h", "T-28123-HD", "T-28123H", true},
		{"55t2800123hd", "T-2800123H", "T-2800123-HD", true},
		{"55t2800123hd", "T-28123H", "T-28123H", false},
		{"55t28123h", "T-28123H", "", false},
		{"55t28123h", "T-28123H", "unknown", false},
		{"55t28123h", "T-28123H", "T28-123H", false},
		{"55t28123h", "T-28123H", "T-28124H", false},
		{"55t28123h", "T-28123H", "T-28123", false},
		{"55t28123zhd", "T-28123-Z-HD", "T-28123ZH", true},
		{"55t28123zhd", "T-28123H", "T-28123H", false},
		{"n_600t28123ai", "T-28999AI", "T-28999AI", true},
		{"n_600t28123ai", "T-28999AI", "T28-999AI", false},
		{"9t28123h", "T-28123H", "T-28123H", false},
	} {
		t.Run(tc.cid+"/"+tc.query+"/"+tc.page, func(t *testing.T) {
			result, err := searchT28Fixture(t, tc.query, tc.cid, tc.page)
			if tc.accept {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tc.cid, result.ContentID)
				assert.Equal(t, canonicalRemasterDisplayID(tc.page), result.ID)
			} else {
				require.Error(t, err)
				assert.Nil(t, result)
			}
		})
	}
}

func TestUnprovenT28AdmissionDoesNotTrustCacheOrCIDAlone(t *testing.T) {
	require.False(t, cachedRemasterIdentityMatches("T-28123H", "55t28123h", "h", "t", "", false))
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="/mono/dvd/-/detail/=/cid=55t28123h/">candidate</a>`))
	require.NoError(t, err)
	require.Len(t, extractRemasterContentIDCandidates(doc, "t", "h", ""), 1)
	assert.Empty(t, extractRemasterContentIDCandidates(doc, "t", "ai", ""))
	assert.Empty(t, extractRemasterContentIDCandidates(doc, "t", "h", "z"))
	assert.Empty(t, extractRemasterContentIDCandidates(doc, "rct", "h", ""))
}

func TestUnprovenT28MultipleVerifiedCandidatesReject(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<a href="/mono/dvd/-/detail/=/cid=55t28123h/">one</a><a href="/mono/dvd/-/detail/=/cid=874t28123h/">two</a>`
		}
		if strings.Contains(u, "cid=55t28123h/") || strings.Contains(u, "cid=874t28123h/") {
			return 200, `<table><tr><td>品番：</td><td>T-28123H</td></tr></table>`
		}
		return 404, ""
	}})
	result, err := s.Search(context.Background(), "T-28123H")
	require.Error(t, err)
	assert.Nil(t, result)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
	assert.Contains(t, err.Error(), "2 verified of 2 distinct")
}
