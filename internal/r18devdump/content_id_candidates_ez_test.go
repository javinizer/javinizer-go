package r18devdump

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentIDCandidatesWithMarker_CatalogSuffix(t *testing.T) {
	cands := ContentIDCandidatesWithMarker("IPX-535Z-HD")
	require.NotEmpty(t, cands)
	for _, c := range cands {
		assert.True(t, strings.HasSuffix(c, "zh") || strings.HasSuffix(c, "zai"), "%s keeps the catalog suffix and marker", c)
	}
	assert.Contains(t, cands, "ipx535zh")
	assert.Contains(t, cands, "ipx00535zh")

	// Raw suffixed content ids expand to the canonical prefix-cleaned shapes.
	cands = ContentIDCandidatesWithMarker("1ipx00535zh")
	require.NotEmpty(t, cands)
	assert.Contains(t, cands, "ipx00535zh")

	// E variants behave identically.
	cands = ContentIDCandidatesWithMarker("IPX-535E-HD")
	require.NotEmpty(t, cands)
	assert.Contains(t, cands, "ipx00535eh")
}
