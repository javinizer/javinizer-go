package minnanoav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestSearchCov_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	settings := &models.ScraperSettings{Enabled: true, BaseURL: server.URL}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, _, err := s.searchActress(context.Background(), "test")
	assert.Error(t, err)
}

func TestSearchCov_SearchError(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, BaseURL: "http://localhost:1"}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, _, err := s.searchActress(context.Background(), "test")
	assert.Error(t, err)
}

func TestSplitRomajiCov_Empty(t *testing.T) {
	first, last, ok := splitRomajiName("")
	assert.False(t, ok)
	assert.Equal(t, "", first)
	assert.Equal(t, "", last)
}
