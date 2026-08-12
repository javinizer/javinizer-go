package minnanoav

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestRedirectPolicy_RejectsNonAllowlistedHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example.com", http.StatusFound)
	}))
	defer server.Close()

	settings := &models.ScraperSettings{Enabled: true, BaseURL: server.URL, RetryCount: 0}
	client := buildClient(settings, nil)
	_, err := client.R().Get(server.URL)
	assert.Error(t, err)
}

func TestRedirectPolicy_AllowsAllowlistedHost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	settings := &models.ScraperSettings{Enabled: true, BaseURL: target.URL, RetryCount: 0}
	client := buildClient(settings, nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()

	_, err := client.R().Get(server.URL)
	assert.Error(t, err)
}
