package minnanoav

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestMinnanoCoverage_Init(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, RetryCount: -5}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, s)
}

func TestMinnanoCoverage_InitNoProxy(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, s)
}

func TestMinnanoCoverage_InitExplicitZeroRetry(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, RetryCount: 0}
	settings.SetRetryCountPresence(true)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, s)
}

func TestMinnanoCoverage_RedirectPolicy(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.com"}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, s.client)
}
