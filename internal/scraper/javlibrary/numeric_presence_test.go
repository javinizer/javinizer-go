package javlibrary

import (
	"encoding/json"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNumericPresencePreservesHTTPOptIns(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		var settings models.ScraperSettings
		require.NoError(t, json.Unmarshal([]byte(`{"retry_count":0,"cookies":{"session":"must-not-auto-forward"}}`), &settings))
		settings.UseFlareSolverr = enabled
		s := newScraper(&settings, nil, models.FlareSolverrConfig{Enabled: true, URL: "http://127.0.0.1:8191"})
		t.Cleanup(func() { require.NoError(t, s.Close()) })
		require.Zero(t, s.client.RetryCount)
		require.Empty(t, s.client.Header.Get("Cookie"), "cookies remain request-scoped, not automatic client defaults")
		require.Equal(t, enabled, s.flaresolverr != nil)
		require.Equal(t, "must-not-auto-forward", settings.Cookies["session"], "constructor must not mutate caller")
	}
}
