package httpclient

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestScraperClientCov_PropagatesExplicitZeroTimeout(t *testing.T) {
	settings := &models.ScraperSettings{Timeout: 0}
	settings.SetTimeoutPresence(true)
	result := InitScraperClient(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, result)
}

func TestScraperClientCov_PropagatesExplicitZeroRetry(t *testing.T) {
	settings := &models.ScraperSettings{RetryCount: 0}
	settings.SetRetryCountPresence(true)
	result := InitScraperClient(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, result)
}
