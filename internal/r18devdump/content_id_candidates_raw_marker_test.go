package r18devdump

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRawUnderscoreRemasterCandidates(t *testing.T) {
	for _, cid := range []string{"h_003abc00123h", "h_003abc00123hd", "n_600abc00123ai"} {
		t.Run(cid, func(t *testing.T) { assert.Equal(t, []string{cid}, ContentIDCandidatesWithMarker(cid)) })
	}
	assert.Equal(t, []string{"h_003abc00123hd"}, ContentIDCandidatesWithMarker(" H_003ABC00123HD "))
	assert.Equal(t, ContentIDCandidatesWithMarker("RCT-156H"), ContentIDCandidatesWithMarker("RCT_156_HD"))
}
