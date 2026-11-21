package fc2

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestFC2DetailIdentityPrecedenceAcrossCallers(t *testing.T) {
	const inputURL = "https://adult.contents.fc2.com/article/1234567/?fixture=space%20kept"
	const htmlBody = `<title>Fixture title</title><p>商品ID: FC2 PPV 7654321</p>`
	settings := &models.ScraperSettings{Enabled: true, BaseURL: "https://mirror.invalid/catalog"}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	s.client.SetRetryCount(0)
	var requests []string
	s.client.SetTransport(metadataContractTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, req.Method)
		requests = append(requests, req.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(htmlBody)),
		}, nil
	}))

	direct, err := s.ScrapeURL(t.Context(), inputURL)
	require.NoError(t, err)
	require.NotNil(t, direct)
	require.Equal(t, "FC2-PPV-7654321", direct.ID, "HTML ID takes precedence over the direct page URL")
	require.Equal(t, direct.ID, direct.ContentID)
	require.Equal(t, inputURL, direct.SourceURL)
	require.Equal(t, "Fixture title", direct.Title)

	search, err := s.Search(t.Context(), "FC2-PPV-1234567")
	require.EqualError(t, err, models.NewScraperNotFoundError("FC2", "movie FC2-PPV-1234567 not found on FC2").Error())
	require.Nil(t, search, "keyword Search retains its mismatch rejection")
	require.Equal(t, []string{inputURL, "https://mirror.invalid/catalog/article/1234567/"}, requests)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlBody))
	require.NoError(t, err)
	parsed := ParseHTML(doc, htmlBody, inputURL)
	require.Equal(t, direct, parsed, "plain HTML parser uses the same metadata and precedence")
}

func TestFC2ParseHTMLKeepsNoIdentityOptional(t *testing.T) {
	const body = `<title>Fixture without a product ID</title>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	require.NoError(t, err)
	require.Nil(t, ParseHTML(doc, body, "https://mirror.invalid/catalog"))
	const sourceURL = "https://contents.fc2.com/article/0012345/"
	parsed := ParseHTML(doc, body, sourceURL)
	require.NotNil(t, parsed)
	require.Equal(t, "FC2-PPV-0012345", parsed.ID)
	require.Equal(t, sourceURL, parsed.SourceURL)
}
