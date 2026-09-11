package r18devdump

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentIDCandidatesWithMarker(t *testing.T) {
	cands := ContentIDCandidatesWithMarker("RCT-156H")
	assert.NotEmpty(t, cands)
	for _, c := range cands {
		assert.Contains(t, c, "h", "every candidate preserves the marker: %s", c)
		assert.NotContains(t, c, "hd")
	}
	assert.Contains(t, cands, "rct156h")

	cands = ContentIDCandidatesWithMarker("DV-818AI")
	assert.NotEmpty(t, cands)
	for _, c := range cands {
		assert.Contains(t, c, "ai", "every candidate preserves the ai marker: %s", c)
	}

	// Channel-prefixed content-id input keeps identity leading with marker intact.
	cands = ContentIDCandidatesWithMarker("1RCT00156H")
	assert.Equal(t, "1rct00156h", cands[0])

	// HD marker folds to h.
	cands = ContentIDCandidatesWithMarker("RCT-156HD")
	assert.NotEmpty(t, cands)
	assert.Equal(t, ContentIDCandidatesWithMarker("RCT-156H"), cands)

	// Separator-bearing display spellings split and expand correctly.
	for _, in := range []string{"RCT-156-HD", "RCT-156 HD", "DV-818-AI"} {
		cands = ContentIDCandidatesWithMarker(in)
		assert.NotEmpty(t, cands, in)
		for _, c := range cands {
			assert.True(t, strings.HasSuffix(c, "h") || strings.HasSuffix(c, "ai"), "%s -> %s keeps marker", in, c)
		}
	}

	// Raw content ids preserve the exact marker; display spellings fold hd->h.
	cands = ContentIDCandidatesWithMarker("1rct00156hd")
	require.Contains(t, cands, "1rct00156hd")
	cands = ContentIDCandidatesWithMarker("rct00156hd")
	require.Contains(t, cands, "rct00156hd")

	// Separated padded display spellings fold hd->h despite looking padded.
	for _, c := range ContentIDCandidatesWithMarker("RCT-00156-HD") {
		assert.False(t, strings.HasSuffix(c, "hd"), "%q must probe h, not hd", c)
	}

	// Marker-free input delegates to base behavior.
	assert.Equal(t, ContentIDCandidates("RCT-156"), ContentIDCandidatesWithMarker("RCT-156"))
}
