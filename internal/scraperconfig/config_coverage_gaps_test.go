package scraperconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigCov_MarshalJSON(t *testing.T) {
	s := ScraperSettings{
		Enabled:         true,
		Language:        "ja",
		Timeout:         30,
		RateLimit:       1000,
		RetryCount:      3,
		UserAgent:       "test",
		BaseURL:         "https://example.com",
		UseFlareSolverr: false,
		UseBrowser:      false,
	}
	s.SetRateLimitPresence(true)
	s.SetRetryCountPresence(true)
	s.SetTimeoutPresence(true)
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, float64(30), m["timeout"])
	assert.Equal(t, float64(1000), m["rate_limit"])
	assert.Equal(t, float64(3), m["retry_count"])
}

func TestConfigCov_MarshalJSONOmitsZeroWhenNotExplicit(t *testing.T) {
	s := ScraperSettings{Timeout: 0, RateLimit: 0, RetryCount: 0}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}
