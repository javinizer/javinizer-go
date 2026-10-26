package tokyohot

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metadataContractTransport func(*http.Request) (*http.Response, error)

func (f metadataContractTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOfflineMetadataScrapeOpaquePathUsesHTMLProductID(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true, Language: "en"}, nil, models.FlareSolverrConfig{})
	s.client.SetRetryCount(0)
	calls := 0
	s.client.SetTransport(metadataContractTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, "https://www.tokyo-hot.com/pages/detail?lang=en", req.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body: io.NopCloser(strings.NewReader(`<html><title>Fixture title | TokyoHot</title>
				<dl class="info"><dt>Product ID</dt><dd>n4567</dd></dl></html>`)),
			Request: req,
		}, nil
	}))
	result, err := s.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/pages/detail")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "N4567", result.ID)
	assert.Equal(t, "N4567", result.ContentID)
	assert.Equal(t, "Fixture title", result.Title)
	assert.Equal(t, "tokyohot", result.Source)
	assert.Equal(t, "en", result.Language)
	assert.Equal(t, "https://www.tokyo-hot.com/pages/detail?lang=en", result.SourceURL)
	assert.Equal(t, 1, calls)
}

func TestOfflineMetadataSearchLinksNeverBecomeDetailCandidates(t *testing.T) {
	for _, tt := range []struct {
		name string
		html string
		want string
	}{
		{
			name: "only query link is not a detail result",
			html: `<a href="/product/?q=N1234">N1234</a>`,
		},
		{
			name: "query link cannot shadow exact product match",
			html: `<a href="/product/?q=N1234">N1234</a><a href="/product/n1234/">N1234</a>`,
			want: "https://www.tokyo-hot.com/product/n1234/?lang=en",
		},
		{
			name: "query link does not count against single product fallback",
			html: `<a href="/product/?q=N1234">N1234</a><a href="/product/98765/">Fixture title</a>`,
			want: "https://www.tokyo-hot.com/product/98765/?lang=en",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true, Language: "en"}, nil, models.FlareSolverrConfig{})
			s.client.SetRetryCount(0)
			calls := 0
			s.client.SetTransport(metadataContractTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, "https://www.tokyo-hot.com/product/?q=N1234", req.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/html"}},
					Body:       io.NopCloser(strings.NewReader(tt.html)),
					Request:    req,
				}, nil
			}))
			got, err := s.GetURL(context.Background(), "N1234")
			if tt.want == "" {
				require.Error(t, err)
				classified, ok := models.AsScraperError(err)
				require.True(t, ok)
				assert.Equal(t, models.ScraperErrorKindNotFound, classified.Kind)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
			assert.Equal(t, 1, calls)
		})
	}
}
