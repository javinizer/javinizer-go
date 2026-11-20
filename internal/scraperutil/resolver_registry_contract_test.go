package scraperutil_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

type registryResolverScraper struct{ mockScraper }

func (s *registryResolverScraper) Name() string {
	if s == nil {
		return "nullable"
	}
	return s.mockScraper.Name()
}

func (*registryResolverScraper) ResolveDownloadProxyForHost(string) (*models.ProxyConfig, *models.ProxyConfig, bool) {
	return nil, nil, false
}

func TestCollectRegistryResolversPreservesPriorityAndNilPolicy(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	priority := &registryResolverScraper{mockScraper{name: "z-priority", enabled: true}}
	first := &registryResolverScraper{mockScraper{name: "a-first", enabled: true}}
	last := &registryResolverScraper{mockScraper{name: "b-last", enabled: false}}
	var typedNil *registryResolverScraper

	reg.RegisterInstance(nil)
	reg.RegisterInstance(last)
	reg.RegisterInstance(priority)
	reg.RegisterInstance(first)
	reg.RegisterInstance(&mockScraper{name: "plain", enabled: true})
	// A typed-nil custom implementation can expose a nil-safe Name. It is
	// distinct from a nil interface and follows normal registration semantics.
	reg.RegisterInstance(typedNil)

	require.Equal(t, []models.DownloadProxyResolver{priority, first, last, typedNil},
		reg.CollectDownloadProxyResolvers([]string{"unknown", "z-priority", "plain", "z-priority"}))
	require.Equal(t, []models.DownloadProxyResolver{first, last, typedNil, priority},
		reg.CollectDownloadProxyResolvers(nil))
}
