package minnanoav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalCoverage_SearchUnexpectedStatus(t *testing.T) {
	cleanup := ssrf.AllowHostForTest("127.0.0.1")
	t.Cleanup(cleanup)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	settings := &models.ScraperSettings{Enabled: true, BaseURL: srv.URL}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, _, err := s.searchActress(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestFinalCoverage_SearchInvalidResponse(t *testing.T) {
	cleanup := ssrf.AllowHostForTest("127.0.0.1")
	t.Cleanup(cleanup)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>no results</body></html>"))
	}))
	defer srv.Close()

	settings := &models.ScraperSettings{Enabled: true, BaseURL: srv.URL}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, _, _ = s.searchActress(context.Background(), "test")
}

func TestFinalCoverage_BuildClientWithUserAgent(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, UserAgent: "test-ua"}
	client := buildClient(settings, nil)
	assert.NotNil(t, client)
}

func TestFinalCoverage_BuildClientWithRetryCondition(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, RetryCount: 3}
	client := buildClient(settings, nil)
	assert.NotNil(t, client)
}
