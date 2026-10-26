package fc2

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metadataContractTransport func(*http.Request) (*http.Response, error)

func (f metadataContractTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOfflineMetadataRegisteredBaseURLValidation(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	Register(registry)
	validate := registry.GetValidateFn("fc2")
	require.NotNil(t, validate)
	for _, tt := range []struct {
		name    string
		baseURL string
		wantErr string
	}{
		{name: "optional empty value"},
		{name: "https", baseURL: "https://adult.contents.fc2.com"},
		{name: "trimmed http", baseURL: " http://contents.fc2.com/ "},
		{name: "unsupported scheme", baseURL: "file:///fixture", wantErr: "fc2.base_url must be a valid HTTP or HTTPS URL"},
		{name: "malformed URL", baseURL: "https://[bad", wantErr: "fc2.base_url must be a valid URL"},
		{name: "missing host", baseURL: "https:///article/1234567", wantErr: "fc2.base_url must be a valid HTTP or HTTPS URL with a host"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			settings := &models.ScraperSettings{BaseURL: tt.baseURL}
			original := settings.Clone()
			err := validate(settings)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
			assert.Equal(t, original, *settings, "validation must not rewrite the caller's configuration")
		})
	}
}

func TestOfflineMetadataPublicFetchPropagatesTransportFailure(t *testing.T) {
	for _, method := range []string{"ScrapeURL", "Search"} {
		t.Run(method, func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
			s.client.SetRetryCount(0)
			wantErr := errors.New("fixture connection reset")
			calls := 0
			s.client.SetTransport(metadataContractTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, "https://adult.contents.fc2.com/article/1234567/", req.URL.String())
				return nil, wantErr
			}))
			var result *models.ScraperResult
			var err error
			if method == "ScrapeURL" {
				result, err = s.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/1234567/")
			} else {
				result, err = s.Search(context.Background(), "FC2-PPV-1234567")
			}
			require.ErrorIs(t, err, wantErr)
			assert.ErrorContains(t, err, "failed to fetch FC2 detail page")
			assert.Nil(t, result, "failed fetch must not fabricate metadata from the requested ID")
			assert.Equal(t, 1, calls)
		})
	}
}

func TestOfflineMetadataCanceledPublicFetchDoesNotDispatch(t *testing.T) {
	for _, method := range []string{"ScrapeURL", "Search"} {
		t.Run(method, func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true, RateLimit: 1000}, nil, models.FlareSolverrConfig{})
			s.client.SetRetryCount(0)
			calls := 0
			s.client.SetTransport(metadataContractTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("canceled request reached transport")
			}))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var result *models.ScraperResult
			var err error
			if method == "ScrapeURL" {
				result, err = s.ScrapeURL(ctx, "https://adult.contents.fc2.com/article/1234567/")
			} else {
				result, err = s.Search(ctx, "FC2-PPV-1234567")
			}
			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, result)
			assert.Zero(t, calls, "the positive-delay limiter must honor pre-cancellation before transport dispatch")
		})
	}
}
