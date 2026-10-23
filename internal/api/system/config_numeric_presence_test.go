package system

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateConfigNumericPresenceRoundTrip(t *testing.T) {
	for _, fields := range []string{`"rate_limit":0,"retry_count":0,"timeout":0`, `"request_delay":0,"max_retries":0,"timeout":0`} {
		t.Run(fields, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			path := t.TempDir() + "/config.yaml"
			require.NoError(t, config.Save(cfg, path))
			deps := createTestDeps(t, cfg, path)
			defaults := models.ScraperSettings{Enabled: true, Language: "en", RateLimit: 500, RetryCount: 3, Timeout: 30}
			deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{Name: "r18dev", Defaults: defaults})
			router := gin.New()
			router.PUT("/config", updateConfig(testkit.GetTestRuntime(deps)))
			router.GET("/config", getConfig(deps))
			data, err := json.Marshal(cfg)
			require.NoError(t, err)
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &body))
			var scrapers map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body["scrapers"], &scrapers))
			scrapers["priority"] = json.RawMessage(`["r18dev"]`)
			scrapers["r18dev"] = json.RawMessage(`{` + fields + `}`)
			body["scrapers"], err = json.Marshal(scrapers)
			require.NoError(t, err)
			data, err = json.Marshal(body)
			require.NoError(t, err)
			put := func(data []byte) {
				req := httptest.NewRequest("PUT", "/config", bytes.NewReader(data))
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				require.Equal(t, 200, response.Code, response.Body.String())
			}
			put(data)
			// Exercise actual GET -> PUT editing, not just JSON codecs.
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/config", nil))
			require.Equal(t, 200, response.Code)
			put(response.Body.Bytes())
			saved, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(saved), "rate_limit: 0")
			assert.Contains(t, string(saved), "retry_count: 0")
			assert.Contains(t, string(saved), "timeout: 0")
			loaded, err := config.Load(path)
			require.NoError(t, err)
			settings := loaded.Scrapers.Overrides["r18dev"]
			require.NotNil(t, settings)
			settings.MergeDefaultsFrom(defaults)
			settings.MergeEnabledDefault(defaults)
			assert.Zero(t, settings.RateLimit)
			assert.Zero(t, settings.RetryCount)
			assert.Zero(t, settings.Timeout)
			assert.True(t, settings.Enabled)
		})
	}
}
