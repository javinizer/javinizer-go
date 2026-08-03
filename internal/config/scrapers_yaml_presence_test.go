package config

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
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

// A legacy alias must not clobber an explicitly configured canonical zero.
func TestYAMLAliasDoesNotOverrideCanonicalKeys(t *testing.T) {
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{}}

	canonical := scraperYAMLNode(t, "minnanoav:\n  rate_limit: 0\n  request_delay: 500\n")
	var ss models.ScraperSettings
	require.NoError(t, canonical.Decode(&ss))
	sc.applyYAMLAliases(canonical, &ss)
	require.Equal(t, 0, ss.RateLimit)

	aliasOnly := scraperYAMLNode(t, "minnanoav:\n  request_delay: 500\n")
	var ss2 models.ScraperSettings
	require.NoError(t, aliasOnly.Decode(&ss2))
	sc.applyYAMLAliases(aliasOnly, &ss2)
	require.Equal(t, 500, ss2.RateLimit)
}

// Same canonical-wins precedence in the JSON path.
func TestJSONAliasDoesNotOverrideCanonicalKeys(t *testing.T) {
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{}}

	var ss models.ScraperSettings
	sc.applyJSONAliases(map[string]json.RawMessage{
		"rate_limit":    json.RawMessage("0"),
		"request_delay": json.RawMessage("500"),
	}, &ss)
	require.Equal(t, 0, ss.RateLimit)

	var ss2 models.ScraperSettings
	sc.applyJSONAliases(map[string]json.RawMessage{
		"request_delay": json.RawMessage("500"),
	}, &ss2)
	require.Equal(t, 500, ss2.RateLimit)
}
