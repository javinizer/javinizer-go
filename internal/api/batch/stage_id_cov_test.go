package batch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex cloud P2: same-tick poster-from-URL/crop requests used to collide on
// identical staged IDs (unixnano only); the atomic suffix makes every call
// unique — the first lock winner can never promote the other request's bytes.
func TestNextPosterStageID(t *testing.T) {
	a := nextPosterStageID("PI-1", "stage")
	b := nextPosterStageID("PI-1", "stage")
	c := nextPosterStageID("PI-1", "crop")
	require.NotEqual(t, a, b, "back-to-back same-kind IDs must differ")
	assert.Contains(t, a, ".stage-")
	assert.Contains(t, c, ".crop-")
	assert.NotEqual(t, a, c, "kind lanes never collide")
}
