package system

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigUpdateService_PreserveDefaultsVersionAcrossAPIRoundTrip(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	oldCfg.DefaultsVersion = config.CurrentDefaultsVersion
	oldCfg.Scrapers.RequestTimeoutSeconds = 60

	data, err := json.Marshal(oldCfg)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "defaults_version", "defaults_version must be hidden from API JSON")

	var newCfg config.Config
	require.NoError(t, json.Unmarshal(data, &newCfg))
	assert.Equal(t, 0, newCfg.DefaultsVersion, "decoded config must start at 0 (json:\"-\" round-trip)")

	tempConfigFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	require.NoError(t, svc.ValidateAndApply(oldCfg, &newCfg, nil))

	saved, err := os.ReadFile(tempConfigFile)
	require.NoError(t, err)
	assert.Contains(t, string(saved), "defaults_version: 1")

	reloaded, err := config.LoadOrCreate(tempConfigFile)
	require.NoError(t, err)
	assert.Equal(t, 60, reloaded.Scrapers.RequestTimeoutSeconds, "patch must not rerun after API round-trip")
	assert.Equal(t, config.CurrentDefaultsVersion, reloaded.DefaultsVersion)
}
