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

func TestNumericPresenceInitPreservesOptInExclusions(t *testing.T) {
	var settings models.ScraperSettings
	require.NoError(t, json.Unmarshal([]byte(`{"retry_count":0,"cookies":{"session":"not-automatic"},"use_flaresolverr":true}`), &settings))
	result := InitScraperClient(&settings, nil, models.FlareSolverrConfig{Enabled: true, URL: "http://127.0.0.1:8191"})
	require.Zero(t, result.Client.RetryCount)
	require.Empty(t, result.Client.Header.Get("Cookie"))
	require.Nil(t, result.FlareSolverr)
	opted := InitScraperClient(&settings, nil, models.FlareSolverrConfig{}, WithScraperCookies(map[string]string{"session": "explicit"}))
	require.Equal(t, "session=explicit", opted.Client.Header.Get("Cookie"))
	require.Equal(t, "not-automatic", settings.Cookies["session"])
	require.True(t, settings.UseFlareSolverr)
}
