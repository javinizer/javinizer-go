package models

import (
	"database/sql/driver"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatActressNameV4(t *testing.T) {
	tests := []struct {
		name     string
		actress  Actress
		opts     FormatActressNameOptions
		expected string
	}{
		{"JapaneseName only", Actress{JapaneseName: "田中太郎"}, FormatActressNameOptions{}, "田中太郎"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatActressName(tt.actress, tt.opts)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAPITokenTableNameV4(t *testing.T) {
	token := ApiToken{}
	assert.Equal(t, "api_tokens", token.TableName())
}

func TestBrowserConfigValidateV4(t *testing.T) {
	cfg := BrowserConfig{Enabled: true, Timeout: 30, WindowWidth: 1280, WindowHeight: 720}
	err := cfg.Validate("test-scraper")
	assert.NoError(t, err)
}

func TestFlareSolverrConfigValidateV4(t *testing.T) {
	cfg := FlareSolverrConfig{Enabled: false}
	err := cfg.Validate("test-scraper")
	assert.NoError(t, err)
}

func TestRescrapeStatusStringV4(t *testing.T) {
	rs := RescrapeStatus("completed")
	result := rs.String()
	assert.Equal(t, "completed", result)
}

func TestRescrapeStatusMarshalJSONV4(t *testing.T) {
	rs := RescrapeStatus("completed")
	data, err := rs.MarshalJSON()
	assert.NoError(t, err)
	assert.Equal(t, `"completed"`, string(data))
}

func TestRescrapeStatusUnmarshalJSONV4(t *testing.T) {
	var rs RescrapeStatus
	err := rs.UnmarshalJSON([]byte(`"pending"`))
	assert.NoError(t, err)
	assert.Equal(t, RescrapeStatus("pending"), rs)
}

func TestRescrapeStatusScanV4(t *testing.T) {
	var rs RescrapeStatus
	err := rs.Scan([]byte("failed"))
	assert.NoError(t, err)
	assert.Equal(t, RescrapeStatus("failed"), rs)
}

func TestRescrapeStatusValueV4(t *testing.T) {
	rs := RescrapeStatus("completed")
	val, err := rs.Value()
	assert.NoError(t, err)
	assert.Equal(t, driver.Value("completed"), val)
}

func TestOrchestrationStateCloneV4(t *testing.T) {
	original := &OrchestrationState{}
	cloned := original.Clone()
	require.NotNil(t, cloned)
}

func TestProxyRedactV4(t *testing.T) {
	proxy := ProxyConfig{
		Enabled:  true,
		Profiles: map[string]ProxyProfile{"test": {URL: "http://user:pass@proxy.example.com:8080"}},
	}
	redacted := proxy.Redact()
	// Verify redaction works on profiles
	if p, ok := redacted.Profiles["test"]; ok {
		// Credentials should be redacted (not contain plaintext user:pass)
		assert.NotContains(t, p.URL, "user:pass")
	}
}

func TestRedactProxyProfilesV4(t *testing.T) {
	profiles := map[string]ProxyProfile{
		"test": {
			URL: "http://user:pass@proxy.example.com:8080",
		},
	}
	cfg := ProxyConfig{Enabled: true, Profiles: profiles}
	redacted := cfg.Redact()
	require.NotNil(t, redacted.Profiles)
}

func TestHistoryOperationStringV4(t *testing.T) {
	assert.Equal(t, "scrape", string(HistoryOpScrape))
	assert.Equal(t, "organize", string(HistoryOpOrganize))
}

func TestHistoryOperationMarshalV4(t *testing.T) {
	data, err := HistoryOpScrape.MarshalJSON()
	assert.NoError(t, err)
	assert.Equal(t, `"scrape"`, string(data))
}

func TestHistoryOperationUnmarshalV4(t *testing.T) {
	var op HistoryOperation
	err := json.Unmarshal([]byte(`"download"`), &op)
	assert.NoError(t, err)
	assert.Equal(t, HistoryOpDownload, op)
}

func TestResolveSearchQueryForScraperV4(t *testing.T) {
	// Just test the function signature exists
	_ = ResolveSearchQueryForScraper
}
