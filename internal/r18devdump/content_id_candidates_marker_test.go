package r18devdump

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

	// Marker-free input delegates to base behavior.
	assert.Equal(t, ContentIDCandidates("RCT-156"), ContentIDCandidatesWithMarker("RCT-156"))
}
