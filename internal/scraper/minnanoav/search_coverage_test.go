package minnanoav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestSearchCov_ServerError500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	settings := &models.ScraperSettings{Enabled: true, BaseURL: server.URL, RetryCount: 3}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, _, err := s.searchActress(context.Background(), "test")
	assert.Error(t, err)
}

func TestSearchCov_SearchNetworkError(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, BaseURL: "http://localhost:1", RetryCount: 3}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, _, err := s.searchActress(context.Background(), "test")
	assert.Error(t, err)
}

func TestSplitRomajiCov_EmptyString(t *testing.T) {
	first, last, ok := splitRomajiName("")
	assert.False(t, ok)
	assert.Equal(t, "", first)
	assert.Equal(t, "", last)
}

func TestSplitRomajiCov_SingleToken(t *testing.T) {
	first, last, ok := splitRomajiName("AIKA")
	assert.True(t, ok)
	assert.Equal(t, "AIKA", first)
	assert.Equal(t, "", last)
}
