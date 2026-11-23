package system

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func TestValidateProxySaveConfigKeepsChangedScopesIndependent(t *testing.T) {
	old := config.DefaultConfig(nil, nil)
	next := old.Clone()
	next.Scrapers.Proxy.Enabled = !old.Scrapers.Proxy.Enabled
	next.Scrapers.FlareSolverr.Timeout++

	deps := newTestDeps(old)
	deps.TokenStore = core.NewTokenStore()
	t.Cleanup(func() { core.CloseTokenStore(deps.TokenStore) })

	globalHash, err := core.HashProxyConfig(next.Scrapers.Proxy)
	require.NoError(t, err)
	globalToken, _, err := deps.TokenStore.Create("global", globalHash)
	require.NoError(t, err)

	flareHash, err := core.HashProxyConfig(next.Scrapers.FlareSolverr)
	require.NoError(t, err)
	flareToken, _, err := deps.TokenStore.Create("flaresolverr", flareHash)
	require.NoError(t, err)

	for _, tt := range []struct {
		name   string
		tokens map[string]string
		want   string
	}{
		{"global is first", nil, "proxy settings changed but no test verification token provided - please test proxy before saving"},
		{"flare token cannot authorize global", map[string]string{"global": flareToken}, "proxy verification token is invalid or expired - please test proxy again"},
		{"missing flare is diagnosed separately", map[string]string{"global": globalToken}, "flaresolverr settings changed but no test verification token provided - please test flaresolverr before saving"},
		{"global token cannot authorize flare", map[string]string{"global": globalToken, "flaresolverr": globalToken}, "flaresolverr verification token is invalid or expired - please test flaresolverr again"},
		{"both verified", map[string]string{"global": globalToken, "flaresolverr": flareToken}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProxySaveConfig(deps, next, tt.tokens)
			if tt.want == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.want)
			}
		})
	}
}
