package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Codex P2 (round 7): an explicit canonical rate_limit: 0 must beat the
// deprecated request_delay alias (documented "canonical takes precedence"),
// while a fully omitted rate_limit still inherits the alias value.
func TestAliasCanonicalPrecedence(t *testing.T) {
	t.Run("yaml canonical explicit zero beats alias", func(t *testing.T) {
		var sc ScrapersConfig
		require.NoError(t, yaml.Unmarshal([]byte("r18dev:\n  rate_limit: 0\n  request_delay: 1500\n"), &sc))
		assert.Equal(t, 0, sc.Overrides["r18dev"].RateLimit)
		assert.True(t, sc.Overrides["r18dev"].RateLimitIsExplicit())
	})
	t.Run("yaml alias fills only when canonical absent", func(t *testing.T) {
		var sc ScrapersConfig
		require.NoError(t, yaml.Unmarshal([]byte("r18dev:\n  request_delay: 1500\n"), &sc))
		assert.Equal(t, 1500, sc.Overrides["r18dev"].RateLimit)
	})
	t.Run("json canonical explicit zero beats alias", func(t *testing.T) {
		var sc ScrapersConfig
		require.NoError(t, sc.UnmarshalJSON([]byte(`{"r18dev":{"rate_limit":0,"request_delay":1500}}`)))
		assert.Equal(t, 0, sc.Overrides["r18dev"].RateLimit)
	})
	t.Run("json alias fills only when canonical absent", func(t *testing.T) {
		var sc ScrapersConfig
		require.NoError(t, sc.UnmarshalJSON([]byte(`{"r18dev":{"request_delay":1500}}`)))
		assert.Equal(t, 1500, sc.Overrides["r18dev"].RateLimit)
	})
}
