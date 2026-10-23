package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestConfigUpdateServiceActressNameOverrides(t *testing.T) {
	old := config.DefaultConfig(nil, nil)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, config.Save(old, path))
	deps := createTestDeps(t, old, path)
	deps.CoreDeps.ScraperRegistry.Register(scraperutil.ScraperRegistration{Name: "javlibrary", Defaults: models.ScraperSettings{Enabled: false, Language: "en"}})
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), path)
	var runtimeRules scraperconfig.ActressNameOverrides
	svc.reload = func(_ *core.APIRuntime, _ *core.APIDeps, cfg *config.Config) error {
		runtimeRules = cfg.Scrapers.ResolvedSettings("javlibrary").ActressNameOverrides
		return nil
	}
	decode := func(body string) *config.Config {
		t.Helper()
		next := config.DefaultConfig(nil, nil)
		require.NoError(t, json.Unmarshal([]byte(body), next))
		return next
	}
	next := decode(`{"scrapers":{"priority":["javlibrary"],"javlibrary":{"enabled":true,"actress_name_overrides":{"aa6sc":{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto"}}}}}`)
	require.NoError(t, svc.ValidateAndApply(old, next, nil))
	require.Equal(t, "Shiori", runtimeRules["aa6sc"].FirstName)
	persisted, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(persisted), "aa6sc:")
	invalid := decode(`{"scrapers":{"priority":["javlibrary"],"javlibrary":{"enabled":true,"actress_name_overrides":{"aa6sc":{"source_name":"Yorimoto Shiori","first_name":"Other","last_name":"Person"}}}}}`)
	require.ErrorContains(t, svc.ValidateAndApply(next, invalid, nil), "actress_name_overrides")
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, persisted, unchanged, "invalid rules must not be persisted")
	require.Equal(t, "Shiori", runtimeRules["aa6sc"].FirstName, "invalid rules must not reach runtime")
	// A normal full-settings JSON round trip must retain the YAML-only option.
	data, err := json.Marshal(next)
	require.NoError(t, err)
	roundTrip := decode(string(data))
	roundTrip.Logging.Level = "debug"
	require.NoError(t, svc.ValidateAndApply(next, roundTrip, nil))
	require.Equal(t, "Shiori", runtimeRules["aa6sc"].FirstName)
	cleared := decode(`{"scrapers":{"priority":["javlibrary"],"javlibrary":{"enabled":true,"actress_name_overrides":{}}}}`)
	require.NoError(t, svc.ValidateAndApply(roundTrip, cleared, nil))
	require.Empty(t, runtimeRules)
	persisted, err = os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(persisted), "actress_name_overrides")
}
