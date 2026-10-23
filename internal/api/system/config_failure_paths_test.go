package system

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestValidateProxySaveConfigFlareSolverrTokens(t *testing.T) {
	old := config.DefaultConfig(nil, nil)
	deps := newTestDeps(old)
	deps.TokenStore = core.NewTokenStore()
	t.Cleanup(func() { core.CloseTokenStore(deps.TokenStore) })
	next := config.DefaultConfig(nil, nil)
	next.Scrapers.FlareSolverr.URL = "http://127.0.0.1:8191/new"
	hash, err := core.HashProxyConfig(next.Scrapers.FlareSolverr)
	require.NoError(t, err)
	token, _, err := deps.TokenStore.Create("flaresolverr", hash)
	require.NoError(t, err)
	wrongScope, _, err := deps.TokenStore.Create("global", hash)
	require.NoError(t, err)
	for _, tc := range []struct{ name, token, want string }{
		{"missing", "", "no test verification token"},
		{"unknown", "unknown", "invalid or expired"},
		{"wrong scope", wrongScope, "invalid or expired"},
		{"valid", token, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProxySaveConfig(deps, next, map[string]string{"flaresolverr": tc.token})
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
	next.Scrapers.FlareSolverr.Timeout++
	require.ErrorContains(t, validateProxySaveConfig(deps, next, map[string]string{"flaresolverr": token}), "invalid or expired")
}

func TestValidateProxySaveConfigLegacyProfileNormalization(t *testing.T) {
	old := config.DefaultConfig(nil, nil)
	old.Scrapers.Proxy = models.ProxyConfig{Profile: "primary"}
	deps := newTestDeps(old)
	deps.TokenStore = core.NewTokenStore()
	t.Cleanup(func() { core.CloseTokenStore(deps.TokenStore) })
	next := config.DefaultConfig(nil, nil)
	next.Scrapers.Proxy = models.ProxyConfig{DefaultProfile: "primary"}
	require.NoError(t, validateProxySaveConfig(deps, next, nil), "equivalent legacy profile should not require another token")
	next.Scrapers.Proxy = models.ProxyConfig{Profile: "replacement"}
	require.ErrorContains(t, validateProxySaveConfig(deps, next, nil), "no test verification token")
	require.Equal(t, "primary", old.Scrapers.Proxy.Profile, "normalization must not mutate the live configuration")
	require.Equal(t, "replacement", next.Scrapers.Proxy.Profile, "normalization must not mutate the submitted configuration")
}

func TestReloadComponentsFallbackAndFailures(t *testing.T) {
	t.Run("missing runtime is initialized", func(t *testing.T) {
		deps := newTestDeps(config.DefaultConfig(nil, nil))
		next := config.DefaultConfig(nil, nil)
		require.NoError(t, reloadComponents(nil, deps, next))
		require.Same(t, next, deps.CoreDeps.GetConfig())
	})
	t.Run("invalid configuration is not published", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		deps := newTestDeps(old)
		rt := core.NewAPIRuntime(deps)
		require.ErrorContains(t, reloadComponents(rt, deps, nil), "config is nil")
		require.Same(t, old, deps.CoreDeps.GetConfig())
	})
	t.Run("logger error does not undo reload", func(t *testing.T) {
		deps := newTestDeps(config.DefaultConfig(nil, nil))
		rt := core.NewAPIRuntime(deps)
		next := config.DefaultConfig(nil, nil)
		next.Logging.Level = "not-a-log-level"
		require.NoError(t, reloadComponents(rt, deps, next))
		require.Same(t, next, deps.CoreDeps.GetConfig())
	})
}
