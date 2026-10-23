package httpclient

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraperClientNumericPresence(t *testing.T) {
	for _, tc := range []struct {
		input string
		retry int
	}{
		{`{}`, DefaultRetryCount},
		{`{"retry_count":null}`, DefaultRetryCount},
		{`{"retry_count":0,"timeout":0}`, 0},
		{`{"max_retries":0}`, 0},
		{`{"retry_count":2}`, 2},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var settings models.ScraperSettings
			require.NoError(t, json.Unmarshal([]byte(tc.input), &settings))
			settings.MergeDefaultsFrom(models.ScraperSettings{RetryCount: 3, Timeout: 30})
			client, err := FromScraperSettings(&settings, nil, models.FlareSolverrConfig{}).build(false)
			require.NoError(t, err)
			assert.Equal(t, tc.retry, client.Client.RetryCount)
			assert.Equal(t, DefaultTimeout, client.Client.GetClient().Timeout, "zero timeout retains existing default inheritance, not unbounded requests")
			initialized := InitScraperClient(&settings, nil, models.FlareSolverrConfig{})
			assert.Equal(t, tc.retry, initialized.Client.RetryCount)
		})
	}
}
