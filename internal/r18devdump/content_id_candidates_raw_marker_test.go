package r18devdump

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDottedRemasterCandidates(t *testing.T) {
	for _, id := range []string{"RCT-156.HD", "RCT.156.HD"} {
		assert.Equal(t, ContentIDCandidatesWithMarker("RCT-156-HD"), ContentIDCandidatesWithMarker(id))
	}
	assert.Equal(t, ContentIDCandidatesWithMarker("RCT-00156-HD"), ContentIDCandidatesWithMarker("RCT.00156.HD"))
	assert.Equal(t, ContentIDCandidatesWithMarker("DV-818-AI"), ContentIDCandidatesWithMarker("DV.818.AI"))
}

func TestRawUnderscoreRemasterCandidates(t *testing.T) {
	for _, cid := range []string{"h_003abc00123h", "h_003abc00123hd", "n_600abc00123ai"} {
		t.Run(cid, func(t *testing.T) { assert.Equal(t, []string{cid}, ContentIDCandidatesWithMarker(cid)) })
	}
	assert.Equal(t, []string{"h_003abc00123hd"}, ContentIDCandidatesWithMarker(" H_003ABC00123HD "))
	assert.Equal(t, ContentIDCandidatesWithMarker("RCT-156H"), ContentIDCandidatesWithMarker("RCT_156_HD"))
}
