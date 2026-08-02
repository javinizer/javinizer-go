package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func scraperYAMLNode(t *testing.T, doc string) *yaml.Node {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &root))
	require.NotEmpty(t, root.Content)
	overrides := root.Content[0]
	require.Len(t, overrides.Content, 2)
	return overrides.Content[1]
}

// YAML decoding must record rate_limit presence (including the deprecated
// request_delay alias) so explicit rate_limit: 0 survives the defaults merge.
func TestScraperYAMLRateLimitPresence(t *testing.T) {
	node := scraperYAMLNode(t, "minnanoav:\n  rate_limit: 0\n")
	require.True(t, scraperYAMLHasKey(node, "rate_limit"))
	require.False(t, scraperYAMLHasKey(node, "enabled"))

	alias := scraperYAMLNode(t, "minnanoav:\n  request_delay: 0\n")
	require.True(t, scraperYAMLHasKey(alias, "request_delay"))

	missing := scraperYAMLNode(t, "minnanoav:\n  enabled: true\n")
	require.False(t, scraperYAMLHasKey(missing, "rate_limit"))
	require.False(t, scraperYAMLHasKey(nil, "rate_limit"))
}
