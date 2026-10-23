package dmm

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplicitZeroTimeoutUsesGlobalAndRetriesStayDisabled(t *testing.T) {
	var settings models.ScraperSettings
	require.NoError(t, json.Unmarshal([]byte(`{"timeout":0,"retry_count":0}`), &settings))
	settings.MergeDefaultsFrom(models.ScraperSettings{Timeout: 30, RetryCount: 3})
	s := newScraper(&settings, nil, models.FlareSolverrConfig{}, dmmOptions{TimeoutSeconds: 47})
	assert.Equal(t, 47*time.Second, s.client.GetClient().Timeout)
	assert.Zero(t, s.client.RetryCount)
}
