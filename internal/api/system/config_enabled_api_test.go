package system

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetConfig_OmittedEnabledExposesEffectiveDefaultTrue(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {RateLimit: 500},
	}
	cfg.Scrapers.Overrides["r18dev"].SetEnabledPresence(false)

	registry := scraperutil.NewScraperRegistry()
	registry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})
	require.NoError(t, cfg.Scrapers.Finalize(registry))

	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/config", getConfig(deps))

	req := httptest.NewRequest("GET", "/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	var scrapers map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["scrapers"], &scrapers))
	r18, ok := scrapers["r18dev"]
	require.True(t, ok, "r18dev override must be present in /api/v1/config output")
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(r18, &fields))
	assert.JSONEq(t, "true", string(fields["enabled"]), "API must expose effective default true for omitted enabled")
	assert.JSONEq(t, "500", string(fields["rate_limit"]))
}

func TestGetConfig_ExplicitFalsePreserved(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: false, RateLimit: 500},
	}
	cfg.Scrapers.Overrides["r18dev"].SetEnabledPresence(true)

	registry := scraperutil.NewScraperRegistry()
	registry.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})
	require.NoError(t, cfg.Scrapers.Finalize(registry))

	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/config", getConfig(deps))

	req := httptest.NewRequest("GET", "/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	var scrapers map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["scrapers"], &scrapers))
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(scrapers["r18dev"], &fields))
	assert.JSONEq(t, "false", string(fields["enabled"]), "API must preserve explicit false")
}

func TestGetConfig_OmittedEnabledExposesEffectiveDefaultFalse(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"dmm": {RateLimit: 250},
	}
	cfg.Scrapers.Overrides["dmm"].SetEnabledPresence(false)

	registry := scraperutil.NewScraperRegistry()
	registry.Register(scraperutil.ScraperRegistration{
		Name:     "dmm",
		Defaults: models.ScraperSettings{Enabled: false},
	})
	require.NoError(t, cfg.Scrapers.Finalize(registry))

	deps := newTestDeps(cfg)

	router := gin.New()
	router.GET("/config", getConfig(deps))

	req := httptest.NewRequest("GET", "/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	var scrapers map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["scrapers"], &scrapers))
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(scrapers["dmm"], &fields))
	assert.JSONEq(t, "false", string(fields["enabled"]), "API must expose effective default false for omitted enabled")
}
