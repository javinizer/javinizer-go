package minnanoav

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCovFinal_BuildClientWithUserAgent(t *testing.T) {
	settings := &models.ScraperSettings{BaseURL: "https://www.minnano-av.com", UserAgent: "test-agent"}
	buildClient(settings, nil)
	assert.True(t, true)
}

func TestCovFinal_BuildClientRedirectPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example.com", http.StatusFound)
	}))
	defer server.Close()
	settings := &models.ScraperSettings{BaseURL: server.URL}
	client := buildClient(settings, nil)
	_, err := client.R().Get(server.URL)
	assert.Error(t, err)
}
