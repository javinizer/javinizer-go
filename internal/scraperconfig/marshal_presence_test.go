package scraperconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Marshaling must omit rate_limit for inherited values so a config GET→save
// round trip cannot bake a synthesized zero (or default) into an explicit
// override; only explicitly configured values persist.
func TestMarshalYAMLRateLimitGating(t *testing.T) {
	omitted := &ScraperSettings{}
	out, err := omitted.MarshalYAML()
	require.NoError(t, err)
	m, ok := out.(map[string]any)
	require.True(t, ok)
	_, present := m["rate_limit"]
	require.False(t, present, "unset rate_limit must not serialize")

	inherited := &ScraperSettings{RateLimit: 1000}
	out, err = inherited.MarshalYAML()
	require.NoError(t, err)
	m, _ = out.(map[string]any)
	require.Equal(t, 1000, m["rate_limit"])

	explicitZero := &ScraperSettings{}
	explicitZero.SetRateLimitPresence(true)
	out, err = explicitZero.MarshalYAML()
	require.NoError(t, err)
	m, _ = out.(map[string]any)
	require.Equal(t, 0, m["rate_limit"], "explicitly configured zero persists")
}
