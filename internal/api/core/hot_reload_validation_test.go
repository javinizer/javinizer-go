package core

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAPIRuntime_ReloadConfig_InvalidEffectiveScraperFails(t *testing.T) {
	src := strings.Join([]string{
		"scrapers:",
		"    r18dev:",
		"        rate_limit: -1",
	}, "\n")
	cfg := config.DefaultConfig(nil, nil)
	require.NoError(t, yaml.Unmarshal([]byte(src), cfg))

	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())

	err := rt.ReloadConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit")
}

func TestAPIRuntime_ReloadConfig_ExplicitFalseInvalidScraperPasses(t *testing.T) {
	src := strings.Join([]string{
		"scrapers:",
		"    r18dev:",
		"        enabled: false",
		"        rate_limit: -1",
	}, "\n")
	cfg := config.DefaultConfig(nil, nil)
	require.NoError(t, yaml.Unmarshal([]byte(src), cfg))

	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())

	err := rt.ReloadConfig(cfg)
	require.NoError(t, err, "explicit enabled:false must skip validation and reload successfully")
}
