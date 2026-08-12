package scraperconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRetryCountExplicitZeroSurvivesDefaults(t *testing.T) {
	s := ScraperSettings{RetryCount: 0}
	s.SetRetryCountPresence(true)
	defaults := ScraperSettings{RetryCount: 3}
	s.MergeDefaultsFrom(defaults)
	assert.Equal(t, 0, s.RetryCount, "explicit 0 must survive MergeDefaultsFrom")
	assert.True(t, s.RetryCountIsExplicit())
}

func TestRetryCountOmittedInherits(t *testing.T) {
	s := ScraperSettings{}
	defaults := ScraperSettings{RetryCount: 3}
	s.MergeDefaultsFrom(defaults)
	assert.Equal(t, 3, s.RetryCount, "omitted must inherit default")
	assert.False(t, s.RetryCountIsExplicit())
}

func TestTimeoutExplicitZeroSurvivesDefaults(t *testing.T) {
	s := ScraperSettings{Timeout: 0}
	s.SetTimeoutPresence(true)
	defaults := ScraperSettings{Timeout: 30}
	s.MergeDefaultsFrom(defaults)
	assert.Equal(t, 0, s.Timeout, "explicit 0 means no timeout and must survive")
	assert.True(t, s.TimeoutIsExplicit())
}

func TestTimeoutOmittedInherits(t *testing.T) {
	s := ScraperSettings{}
	defaults := ScraperSettings{Timeout: 30}
	s.MergeDefaultsFrom(defaults)
	assert.Equal(t, 30, s.Timeout, "omitted must inherit default")
	assert.False(t, s.TimeoutIsExplicit())
}

func TestRetryCountMarshalYAMLOmitsWhenNotSet(t *testing.T) {
	s := ScraperSettings{RetryCount: 0}
	raw, err := s.MarshalYAML()
	assert.NoError(t, err)
	m := raw.(map[string]any)
	_, hasRetry := m["retry_count"]
	assert.False(t, hasRetry, "omitted retry_count should not be marshaled")
}

func TestTimeoutMarshalYAMLOmitsWhenNotSet(t *testing.T) {
	s := ScraperSettings{Timeout: 0}
	raw, err := s.MarshalYAML()
	assert.NoError(t, err)
	m := raw.(map[string]any)
	_, hasTimeout := m["timeout"]
	assert.False(t, hasTimeout, "omitted timeout should not be marshaled")
}
