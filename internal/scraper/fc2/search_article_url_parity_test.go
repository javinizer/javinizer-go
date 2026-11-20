package fc2

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestSearchKeepsArticleURLAndIDContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		base    string
		input   string
		wantURL string
	}{
		{
			name:    "canonical ID with surrounding space",
			base:    " https://mirror.invalid/catalog/ ",
			input:   "  FC2-PPV-1234567  ",
			wantURL: "https://mirror.invalid/catalog/article/1234567/",
		},
		{
			name:    "article URL is an ID source",
			base:    "https://mirror.invalid:8443/catalog%2Farchive",
			input:   "https://contents.fc2.com/article/1234567/?label=sample%20title",
			wantURL: "https://mirror.invalid:8443/catalog%2Farchive/article/1234567/",
		},
		{
			name:    "alternate catalog link with explicit token",
			base:    "http://[::1]:8080/demo%20catalog",
			input:   "https://catalog.invalid:9443/items/FC2-PPV-1234567?label=sample%20title",
			wantURL: "http://[::1]:8080/demo%20catalog/article/1234567/",
		},
		{
			name:  "encoded article digits are not canonical input",
			base:  "https://mirror.invalid/catalog/",
			input: "https://contents.fc2.com/article/%31%32%33%34%35%36%37/",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true, BaseURL: tc.base}, nil, models.FlareSolverrConfig{})
			s.client.SetRetryCount(0)
			calls := 0
			s.client.SetTransport(metadataContractTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, tc.wantURL, req.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/html"}},
					Body: io.NopCloser(strings.NewReader(
						`<title>Fixture title</title><p>商品ID: FC2 PPV 1234567</p>`)),
				}, nil
			}))

			legacyURL, legacyErr := s.GetURL(t.Context(), tc.input)
			result, err := s.Search(t.Context(), tc.input)
			if tc.wantURL == "" {
				require.Error(t, legacyErr)
				require.EqualError(t, err, legacyErr.Error())
				require.Nil(t, result)
				require.Zero(t, calls)
				return
			}
			require.NoError(t, legacyErr)
			require.Equal(t, tc.wantURL, legacyURL)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, calls)
			require.Equal(t, tc.wantURL, result.SourceURL)
			require.Equal(t, "FC2-PPV-1234567", result.ID)
			require.Equal(t, "Fixture title", result.Title)
		})
	}
}
