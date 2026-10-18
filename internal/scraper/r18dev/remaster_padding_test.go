package r18dev

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestPaddedRemasterDisplayComparison(t *testing.T) {
	assert.Equal(t, foldDisplay("RCT-156-HD"), foldDisplay("RCT-00156-HD"))
	assert.Equal(t, foldDisplay("DV-818-AI"), foldDisplay("DV-00818AI"))
	assert.NotEqual(t, foldDisplay("RCT-00157-HD"), foldDisplay("RCT-156-HD"))
	assert.True(t, markerVariationAccept([]byte(`{"content_id":"1rct00156h","dvd_id":"RCT-156-HD"}`), "RCT-00156-HD", "h", "rct"))
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"1rct00156h","dvd_id":"RCT-157-HD"}`), "RCT-00156-HD", "h", "rct"))
	assert.False(t, markerVariationAccept([]byte(`{"content_id":"2rct00156h","dvd_id":"RCT-156-HD"}`), "1rct00156h", "h", "rct"))
}
